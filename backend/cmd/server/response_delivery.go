package main

import (
	"context"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/relationship"
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

// Audio 返回收集的音频帧。句粒度投递降级回整段后（2026-10-01 盲测裁决）
// 恒为单帧；多帧合并逻辑随 deliverStreaming 删除（git 历史可查）。
func (sink *collectingResponseSink) Audio() conversation.AudioDelivery {
	if len(sink.frames) == 0 {
		return conversation.AudioDelivery{}
	}
	return sink.frames[0]
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
