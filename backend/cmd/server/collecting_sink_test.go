package main

import (
	"bytes"
	"context"
	"encoding/binary"
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

// ── 猎虫追加：isCanonicalWAV 只看前 16 字节，夹了额外块的 WAV 被误判 ──

// buildWAVWithExtraChunk 拼一个合法但非规范的 WAV：fmt 与 data 之间夹
// LIST/INFO 块（不少 WAV 写出器的真实形态，供应商完整产物可能如此）。
// 头 16 字节与规范 WAV 无异——现有 isCanonicalWAV 挡不住它。
func buildWAVWithExtraChunk(t *testing.T, pcm []byte) []byte {
	t.Helper()
	listPayload := []byte("INFOqiuqiu-test\x00\x00")
	wav := make([]byte, 0, 44+len(listPayload)+len(pcm))
	wav = append(wav, "RIFF"...)
	riffSize := 36 + 8 + len(listPayload) + len(pcm)
	wav = binary.LittleEndian.AppendUint32(wav, uint32(riffSize))
	wav = append(wav, "WAVE"...)
	wav = append(wav, "fmt "...)
	wav = binary.LittleEndian.AppendUint32(wav, 16)
	wav = binary.LittleEndian.AppendUint16(wav, 1)  // 线性 PCM
	wav = binary.LittleEndian.AppendUint16(wav, 1)  // 单声道
	wav = binary.LittleEndian.AppendUint32(wav, tts.PCMStreamSampleRate)
	wav = binary.LittleEndian.AppendUint32(wav, tts.PCMStreamSampleRate*2)
	wav = binary.LittleEndian.AppendUint16(wav, 2)  // blockAlign
	wav = binary.LittleEndian.AppendUint16(wav, 16) // 位深
	wav = append(wav, "LIST"...)
	wav = binary.LittleEndian.AppendUint32(wav, uint32(len(listPayload)))
	wav = append(wav, listPayload...)
	wav = append(wav, "data"...)
	wav = binary.LittleEndian.AppendUint32(wav, uint32(len(pcm)))
	wav = append(wav, pcm...)
	return wav
}

// isCanonicalWAV 的契约：非规范头的 WAV 完整产物（降级回退/mock 回放混入）
// 「无法安全拼容器——如实回退首帧」。fmt 与 data 之间夹块的 WAV 剥 data[44:]
// 会把块字节当 PCM 拼进合并产物（音频腐蚀），必须同样走首帧回退。
func TestBugWAVWithExtraChunksMustFallBackToFirstFrame(t *testing.T) {
	extraWAV := buildWAVWithExtraChunk(t, []byte{9, 9, 9, 9})
	if isCanonicalWAV(extraWAV) {
		t.Fatal("extra-chunk WAV must not pass the canonical check (splicing it corrupts PCM)")
	}
	// 反向护栏：收紧后的判定不得误伤真正的规范产物。
	if !isCanonicalWAV(tts.WAVFromPCM16([]byte{1, 2, 3, 4}, tts.PCMStreamSampleRate)) {
		t.Fatal("canonical WAVFromPCM16 output must still pass the canonical check")
	}
	sink := &collectingResponseSink{}
	appendSinkFrame(sink, []byte{1, 2, 3, 4}, 0)
	_ = sink.DeliverAudio(context.Background(), conversation.AudioDelivery{
		Data: extraWAV, MIME: "audio/wav", SentenceIndex: 1,
	})

	audio := sink.Audio()
	want := tts.WAVFromPCM16([]byte{1, 2, 3, 4}, tts.PCMStreamSampleRate)
	if !bytes.Equal(audio.Data, want) {
		t.Fatalf("extra-chunk WAV must fall back to first frame; got merged pcm tail %v", audio.Data[44:])
	}
}
