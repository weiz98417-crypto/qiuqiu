package main

import (
	"context"
	"strings"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/speech"
	"qiuqiu/internal/tts"
)

type websocketResponseSink struct{ writer *wsWriter }

func (sink websocketResponseSink) DeliverReply(_ context.Context, delivery conversation.ReplyDelivery) error {
	return sink.writer.SendJSON(map[string]interface{}{
		"type":  "event",
		"event": "qiuqiu_reply",
		"data": qiuqiuReplyData(
			delivery.Text,
			delivery.TraceID,
			delivery.Source,
			delivery.EventID,
			delivery.DeliveryKey,
			delivery.Presentation,
		),
	})
}

func (sink websocketResponseSink) DeliverAudio(_ context.Context, delivery conversation.AudioDelivery) error {
	message := map[string]interface{}{
		"type":        "voice_audio",
		"mime":        delivery.MIME,
		"traceId":     delivery.TraceID,
		"byteLength":  len(delivery.Data),
		"eventId":     delivery.EventID,
		"deliveryKey": delivery.DeliveryKey,
		"source":      delivery.Source,
	}
	// 句粒度路径（voice-streaming-delivery 3.4）：句序号随帧；整段路径
	// 恒 0，wire 层省略（老客户端零感知，FIFO 配对按到达序不受影响）。
	// sentenceCount 随帧下发（外部声音 #9：流结束可感知，客户端可据
	// index==count-1 判终；0/缺省=整段路径）。
	if delivery.SentenceIndex > 0 {
		message["sentenceIndex"] = delivery.SentenceIndex
	}
	if delivery.SentenceCount > 0 {
		message["sentenceCount"] = delivery.SentenceCount
	}
	return sink.writer.SendAudio(message, delivery.Data)
}

func (sink websocketResponseSink) DeliverStatus(_ context.Context, status conversation.DeliveryStatus) error {
	message := map[string]interface{}{
		"type":  status.Kind + "_status",
		"state": status.State,
	}
	if status.Reason != "" {
		message["reason"] = status.Reason
	}
	if status.TraceID != "" {
		message["traceId"] = status.TraceID
	}
	return sink.writer.SendJSON(message)
}

// collectingResponseSink 是操作台 HTTP 语音路径的 ResponseSink：帧不外发，
// 原地收集供 HTTP 响应返回（voice-streaming-delivery 3.1 双装配线合一——
// WS 与 HTTP 两条装配线的差异从此只剩 sink 实现）。句粒度路径一句一帧，
// frames 保留全序；Audio 返回合并视图。
type collectingResponseSink struct {
	reply    conversation.ReplyDelivery
	frames   []conversation.AudioDelivery
	statuses []conversation.DeliveryStatus
}

func (sink *collectingResponseSink) DeliverReply(_ context.Context, delivery conversation.ReplyDelivery) error {
	sink.reply = delivery
	return nil
}

func (sink *collectingResponseSink) DeliverAudio(_ context.Context, delivery conversation.AudioDelivery) error {
	sink.frames = append(sink.frames, delivery)
	return nil
}

func (sink *collectingResponseSink) DeliverStatus(_ context.Context, status conversation.DeliveryStatus) error {
	sink.statuses = append(sink.statuses, status)
	return nil
}

// Audio 把收集的帧合并为 HTTP 响应的单段音频：同构 canonical WAV（本包
// WAVFromPCM16 的 44 字节头）剥头拼 PCM 重封装；混入完整产物分片（mock
// 回放/降级回退）时无法安全拼容器——如实回退首帧（既有单帧行为）。
func (sink *collectingResponseSink) Audio() conversation.AudioDelivery {
	if len(sink.frames) == 0 {
		return conversation.AudioDelivery{}
	}
	first := sink.frames[0]
	if len(sink.frames) == 1 {
		return first
	}
	pcm := make([]byte, 0, len(first.Data)*len(sink.frames))
	for _, frame := range sink.frames {
		if frame.MIME != first.MIME || !isCanonicalWAV(frame.Data) {
			return first
		}
		pcm = append(pcm, frame.Data[44:]...)
	}
	return conversation.AudioDelivery{
		Data: tts.WAVFromPCM16(pcm, tts.PCMStreamSampleRate), MIME: first.MIME,
		TraceID: first.TraceID, Source: first.Source, EventID: first.EventID, DeliveryKey: first.DeliveryKey,
	}
}

func isCanonicalWAV(data []byte) bool {
	return len(data) > 44 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

// mediaDeliveryRecorder 把投递服务的媒体记账事件落到互动账本（两条装配线
// 共用：事件 ID 格式 `media:<user>:<match>:<trace>:<state>` 由服务单一源）。
func mediaDeliveryRecorder(agent *companion.Agent) conversation.MediaDeliveryRecorder {
	return conversation.MediaDeliveryRecorderFunc(func(ctx context.Context, event conversation.MediaDeliveryEvent) error {
		return agent.RecordMediaDelivery(ctx, interaction.Event{
			ID: event.ID, Kind: interaction.KindMediaDelivery, UserID: event.UserID, MatchID: event.MatchID,
			TraceID: event.TraceID, DeliveryKey: event.DeliveryKey, MediaType: event.MediaType,
			DeliveryState: event.DeliveryState, DeliveryReason: event.Reason,
			Source: event.Source, CreatedAt: event.CreatedAt,
		})
	})
}

type responseSpeechSynthesizer struct{ synthesizer speechSynthesizer }

func (adapter responseSpeechSynthesizer) SynthesizeResponse(ctx context.Context, text string, presentation relationship.PresentationPlan, acts []relationship.CommunicationAct) (conversation.SynthesizedAudio, error) {
	result, err := synthesizeReply(ctx, adapter.synthesizer, text, presentation, acts)
	if err != nil {
		return conversation.SynthesizedAudio{}, err
	}
	return conversation.SynthesizedAudio{Data: result.AudioData, MIME: result.MimeType}, nil
}

// SynthesizeResponseStream 实现句粒度可选能力接口（voice-streaming-delivery
// 3.4）：整段 realizer 文本经句聚合器切句，逐句合成——synthesizer 支持
// tts.StreamingSynthesizer 时走流式分片（pcm16 聚合成整句 WAV），否则回
// 退整段合成单句。一句一帧、帧帧完整可播；ctx 取消即中止（打断语义，
// 剩余句不再合成）。
func (adapter responseSpeechSynthesizer) SynthesizeResponseStream(ctx context.Context, text string, presentation relationship.PresentationPlan, acts []relationship.CommunicationAct, onChunk func(conversation.StreamedSentence) error) error {
	aggregator := speech.NewAggregator()
	sentences := aggregator.Feed(text)
	if tail := aggregator.Flush(); tail != "" {
		sentences = append(sentences, tail)
	}
	if len(sentences) == 0 {
		return nil
	}
	streamer, _ := adapter.synthesizer.(tts.StreamingSynthesizer)
	for index, sentence := range sentences {
		opts := tts.VoiceOpts{Instruction: mimoPerformanceInstruction(presentation, acts, len([]rune(sentence)))}
		data, mime, err := adapter.synthesizeSentence(ctx, sentence, streamer, opts)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		if err := onChunk(conversation.StreamedSentence{
			Data: data, MIME: mime, SentenceIndex: index, SentenceCount: len(sentences),
			Final: index == len(sentences)-1,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (adapter responseSpeechSynthesizer) synthesizeSentence(ctx context.Context, sentence string, streamer tts.StreamingSynthesizer, opts tts.VoiceOpts) ([]byte, string, error) {
	if streamer == nil {
		result, err := adapter.synthesizer.Synthesize(ctx, sentence, opts)
		if err != nil {
			return nil, "", err
		}
		return result.AudioData, result.MimeType, nil
	}
	var pcm []byte
	passthrough := []byte(nil)
	passthroughMIME := ""
	err := streamer.SynthesizeStreamDetailed(ctx, sentence, opts, func(chunk tts.StreamChunk) error {
		if chunk.Degraded {
			// REPLACE 契约：回退分片是完整产物，丢弃此前残缺前缀。
			passthrough = chunk.Data
			passthroughMIME = chunk.MimeType
			return nil
		}
		if !strings.HasPrefix(chunk.MimeType, "audio/pcm") {
			// 完整产物分片（如 mock 回放 mp3）：直通，不二次封装。
			passthrough = chunk.Data
			passthroughMIME = chunk.MimeType
			return nil
		}
		pcm = append(pcm, chunk.Data...)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if passthrough != nil {
		return passthrough, passthroughMIME, nil
	}
	return tts.WAVFromPCM16(pcm, tts.PCMStreamSampleRate), "audio/wav", nil
}

func newResponseDeliveryService(writer *wsWriter, agent *companion.Agent, synthesizer speechSynthesizer, tracker *replyDeliveryTracker) *conversation.ResponseDeliveryService {
	var audioSynthesizer conversation.ResponseAudioSynthesizer
	if synthesizer != nil {
		audioSynthesizer = responseSpeechSynthesizer{synthesizer: synthesizer}
	}
	return conversation.NewResponseDeliveryService(websocketResponseSink{writer: writer}, audioSynthesizer, tracker, mediaDeliveryRecorder(agent))
}

// collectingDeliveryService 组装操作台 HTTP 语音路径的请求内投递服务：
// 收集型 sink + 请求内内存 tracker（一次性回合无跨推送去重需求）。
func collectingDeliveryService(agent *companion.Agent, synthesizer speechSynthesizer, sink conversation.ResponseSink) *conversation.ResponseDeliveryService {
	var audioSynthesizer conversation.ResponseAudioSynthesizer
	if synthesizer != nil {
		audioSynthesizer = responseSpeechSynthesizer{synthesizer: synthesizer}
	}
	return conversation.NewResponseDeliveryService(sink, audioSynthesizer, newReplyDeliveryTracker(), mediaDeliveryRecorder(agent))
}
