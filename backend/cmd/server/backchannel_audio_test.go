package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"qiuqiu/internal/backchannel"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

type fakeBackchannelSynth struct {
	instruction string
	text        string
	audio       []byte
	err         error
}

func (f *fakeBackchannelSynth) Synthesize(_ context.Context, text string, opts tts.VoiceOpts) (*tts.SynthesizeResult, error) {
	f.instruction = opts.Instruction
	f.text = text
	if f.err != nil {
		return nil, f.err
	}
	return &tts.SynthesizeResult{AudioData: f.audio, MimeType: ""}, nil
}

func (f *fakeBackchannelSynth) SynthesizeStream(context.Context, string, tts.VoiceOpts) (<-chan []byte, error) {
	return nil, tts.ErrNotSupported
}

type fakeBackchannelSink struct {
	meta map[string]interface{}
	data []byte
	err  error
}

func (f *fakeBackchannelSink) SendAudio(meta interface{}, data []byte) error {
	f.meta = meta.(map[string]interface{})
	f.data = data
	return f.err
}

func TestDeliverBackchannelAudioSendsPairedMetadata(t *testing.T) {
	verdict := backchannel.Verdict{
		Phrase: "神扑！稳住了！", EventType: "save",
		Affect: relationship.AffectState{Arousal: 0.72, Valence: 0.45},
	}
	synth := &fakeBackchannelSynth{audio: []byte("wav-bytes")}
	sink := &fakeBackchannelSink{}

	if err := deliverBackchannelAudio(context.Background(), synth, sink, verdict, "ev-1"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if synth.text != verdict.Phrase {
		t.Fatalf("synth text = %q, want %q", synth.text, verdict.Phrase)
	}
	// 指令两段式：基础人设 + 情绪×react（短语 ≤10 字必咬合紧凑尾巴）；
	// style/energy/speed 数值段不参与——微反应没有表演计划。
	if !strings.Contains(synth.instruction, "20岁左右中文女生") {
		t.Fatalf("instruction missing base persona: %q", synth.instruction)
	}
	if !strings.Contains(synth.instruction, "反应一闪而过") {
		t.Fatalf("instruction missing compact tail: %q", synth.instruction)
	}
	if sink.meta["type"] != "voice_audio" {
		t.Fatalf("meta type = %v", sink.meta["type"])
	}
	if sink.meta["mime"] != "audio/wav" {
		t.Fatalf("empty synth mime must default to wav, got %v", sink.meta["mime"])
	}
	if sink.meta["deliveryKey"] != "backchannel-ev-1" {
		t.Fatalf("deliveryKey = %v", sink.meta["deliveryKey"])
	}
	if sink.meta["source"] != "backchannel" {
		t.Fatalf("source = %v", sink.meta["source"])
	}
	if sink.meta["byteLength"] != len(synth.audio) {
		t.Fatalf("byteLength = %v", sink.meta["byteLength"])
	}
	// 语音通道不进回合送达台账：不带 traceId/eventId，客户端只回
	// playback_result（服务端按 client_late 记账），不产生 unknown-trace 噪音。
	if _, ok := sink.meta["traceId"]; ok {
		t.Fatalf("backchannel audio must not carry traceId: %v", sink.meta)
	}
	if _, ok := sink.meta["eventId"]; ok {
		t.Fatalf("backchannel audio must not carry eventId: %v", sink.meta)
	}
	if !bytes.Equal(sink.data, synth.audio) {
		t.Fatalf("audio bytes mismatch")
	}
}

func TestDeliverBackchannelAudioSkipsAndFails(t *testing.T) {
	verdict := backchannel.Verdict{Phrase: "哎呀，太可惜了！", EventType: "miss"}

	// nil 合成器（无 key 且未开 mock 的合法形态）：v1 纯文字，静默跳过。
	sink := &fakeBackchannelSink{}
	if err := deliverBackchannelAudio(context.Background(), nil, sink, verdict, "ev-1"); err != nil {
		t.Fatalf("nil synthesizer must no-op, got %v", err)
	}
	if sink.meta != nil {
		t.Fatalf("nil synthesizer must not send, got %v", sink.meta)
	}

	// 合成失败：错误上抛（调用方记日志即弃），不发半包。
	failing := &fakeBackchannelSynth{err: errors.New("tts down")}
	if err := deliverBackchannelAudio(context.Background(), failing, sink, verdict, "ev-1"); err == nil {
		t.Fatal("synth error must propagate")
	}
	if sink.meta != nil {
		t.Fatalf("failed synth must not send, got %v", sink.meta)
	}

	// 下发失败：错误上抛。
	ok := &fakeBackchannelSynth{audio: []byte("wav-bytes")}
	broken := &fakeBackchannelSink{err: errors.New("socket closed")}
	if err := deliverBackchannelAudio(context.Background(), ok, broken, verdict, "ev-1"); err == nil {
		t.Fatal("sink error must propagate")
	}
}
