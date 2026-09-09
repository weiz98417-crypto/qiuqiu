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
	return sink.writer.SendAudio(map[string]interface{}{
		"type":        "voice_audio",
		"mime":        delivery.MIME,
		"traceId":     delivery.TraceID,
		"byteLength":  len(delivery.Data),
		"eventId":     delivery.EventID,
		"deliveryKey": delivery.DeliveryKey,
		"source":      delivery.Source,
	}, delivery.Data)
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

type responseSpeechSynthesizer struct{ synthesizer speechSynthesizer }

func (adapter responseSpeechSynthesizer) SynthesizeResponse(ctx context.Context, text string, presentation relationship.PresentationPlan) (conversation.SynthesizedAudio, error) {
	result, err := synthesizeReply(ctx, adapter.synthesizer, text, "", presentation)
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
	recorder := conversation.MediaDeliveryRecorderFunc(func(ctx context.Context, event conversation.MediaDeliveryEvent) error {
		return agent.RecordMediaDelivery(ctx, interaction.Event{
			ID: event.ID, Kind: interaction.KindMediaDelivery, UserID: event.UserID, MatchID: event.MatchID,
			TraceID: event.TraceID, DeliveryKey: event.DeliveryKey, MediaType: event.MediaType,
			DeliveryState: event.DeliveryState, DeliveryReason: event.Reason,
			Source: event.Source, CreatedAt: event.CreatedAt,
		})
	})
	return conversation.NewResponseDeliveryService(websocketResponseSink{writer: writer}, audioSynthesizer, tracker, recorder)
}
