package asr

import "encoding/binary"

const (
	pcmSampleRate    = 16000
	pcmChannelCount  = 1
	pcmBitsPerSample = 16
)

func PCM16ToWAV(pcm []byte) []byte {
	dataSize := len(pcm)
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], pcmChannelCount)
	binary.LittleEndian.PutUint32(wav[24:28], pcmSampleRate)
	binary.LittleEndian.PutUint32(wav[28:32], pcmSampleRate*pcmChannelCount*pcmBitsPerSample/8)
	binary.LittleEndian.PutUint16(wav[32:34], pcmChannelCount*pcmBitsPerSample/8)
	binary.LittleEndian.PutUint16(wav[34:36], pcmBitsPerSample)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(dataSize))
	copy(wav[44:], pcm)
	return wav
}
