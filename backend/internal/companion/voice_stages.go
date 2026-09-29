package companion

// 语音链路延迟 stage 名（operations-turn-replay）：latencyStages 的键单源
// 于此——watchconnection/main.go 的 attach 端与 console 呈现端的字符串字
// 面量都从这里出发（前端契约常量见 console client.ts，跨语言以本注释互
// 指）。值语义见 VoiceTraceMetadata.LatencyStages：除 TTSSynthesized 为合
// 成段耗时（相邻差）外，其余为相对 SpeechReceived 的累计毫秒。
// TTSSynthesized 与 TTSFirstAudio 语义见 voice-streaming-delivery 3.6：
// TTSFirstAudio = 首句可下发时刻（turn_decided 耗时 + 首帧相对 Deliver
// 进入毫秒），仅句粒度路径存在；首响观测与 grafana-alerting 的语音规则
// 消费它。
const (
	VoiceStageSpeechReceived = "speech_received"
	VoiceStageASRFinal       = "asr_final"
	VoiceStageTurnDecided    = "turn_decided"
	VoiceStageTTSFirstAudio  = "tts_first_audio"
	VoiceStageTTSSynthesized = "tts_synthesized"
	VoiceStageAudioDelivered = "audio_delivered"
)
