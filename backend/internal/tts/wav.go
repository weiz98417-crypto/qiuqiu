package tts

import "encoding/binary"

// WAVFromPCM16 把裸 pcm16 单声道字节封装成 WAV 容器（voice-streaming-
// delivery 3.4：MiMo 流式分片是 24kHz pcm16，句帧需要完整可播格式下发）。
// 与 asr.PCM16ToWAV（16k 硬编码，ASR 上行用）分属两个方向，不共用常量。
func WAVFromPCM16(pcm []byte, sampleRate int) []byte {
	dataSize := len(pcm)
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(wav[22:24], 1) // mono
	binary.LittleEndian.PutUint32(wav[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(wav[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(dataSize))
	copy(wav[44:], pcm)
	return wav
}

// PCMStreamSampleRate 是 MiMo 流式输出的采样率（官方文档：24kHz pcm16）。
const PCMStreamSampleRate = 24000
