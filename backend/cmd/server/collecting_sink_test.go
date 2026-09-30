package main

import (
	"bytes"
	"context"
	"testing"

	"qiuqiu/internal/conversation"
	"qiuqiu/internal/tts"
)

func appendSinkFrame(sink *collectingResponseSink, pcm []byte, index int) {
	_ = sink.DeliverAudio(context.Background(), conversation.AudioDelivery{
		Data: tts.WAVFromPCM16(pcm, tts.PCMStreamSampleRate), MIME: "audio/wav",
		SentenceIndex: index,
	})
}

// 回归（code-review P1）：句粒度路径下收集型 sink 曾逐帧覆盖——多句回复的
// 操作台语音会话只回末句音频。合并语义：同构 canonical WAV 剥头拼 PCM。
func TestCollectingSinkMergesSentenceFrames(t *testing.T) {
	sink := &collectingResponseSink{}
	appendSinkFrame(sink, []byte{1, 2, 3, 4}, 0)
	appendSinkFrame(sink, []byte{5, 6, 7, 8}, 1)

	audio := sink.Audio()
	if audio.MIME != "audio/wav" {
		t.Fatalf("mime = %q", audio.MIME)
	}
	want := append(append([]byte(nil), []byte{1, 2, 3, 4}...), []byte{5, 6, 7, 8}...)
	if !bytes.Equal(audio.Data[44:], want) {
		t.Fatalf("merged pcm = %v, want %v", audio.Data[44:], want)
	}
	if !isCanonicalWAV(audio.Data) {
		t.Fatal("merged output must be a canonical WAV container")
	}
}

func TestCollectingSinkMixedContainerFallsBackToFirstFrame(t *testing.T) {
	sink := &collectingResponseSink{}
	appendSinkFrame(sink, []byte{1, 2, 3, 4}, 0)
	_ = sink.DeliverAudio(context.Background(), conversation.AudioDelivery{
		Data: []byte("mp3-bytes-not-wav"), MIME: "audio/mpeg", SentenceIndex: 1,
	})

	audio := sink.Audio()
	if string(audio.Data) != string(tts.WAVFromPCM16([]byte{1, 2, 3, 4}, tts.PCMStreamSampleRate)) {
		t.Fatalf("mixed containers must fall back to the first frame, got %v", audio.Data)
	}
}
