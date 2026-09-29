// 用户语音情绪 sidecar 的确定性假替身（user-voice-affect 波1，与
// cmd/ambient-fake 同角色）：POST /affect 按输入字节数的简单确定性规则
// 回一个标签——本地/CI 环境验证 relay 接线与降级链，不承载语义质量。
// 真实推理在 deploy 的 voice-input-sidecar 镜像（SenseVoice）。
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("VOICE_SIDECAR_PORT")
	if port == "" {
		port = "8092"
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "fake": true})
	})
	http.HandleFunc("/affect", func(w http.ResponseWriter, r *http.Request) {
		pcm, err := io.ReadAll(r.Body)
		if err != nil || len(pcm) < 320 {
			http.Error(w, `{"error":"pcm too short"}`, http.StatusBadRequest)
			return
		}
		// 确定性规则：音频首样本非零→happy，零→neutral。
		label := "neutral"
		if pcm[0] != 0 || pcm[1] != 0 {
			label = "happy"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"label": label, "confidence": 0.9})
	})
	log.Printf("voice-input fake sidecar listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
