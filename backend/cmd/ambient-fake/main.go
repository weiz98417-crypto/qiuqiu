package main

// ambient-fake 是气氛旁路的确定性假 sidecar（openspec/changes/ambient-audio-
// observation 6.1，pr tier 用）：实现与真 SenseVoice AED sidecar 相同的调用
// 形状——POST /aevents 收 pcm16/16k 单声道音频分片，返回 JSON 事件数组
// {kind: cheer|boo|volume_spike, confidence, ts}。
//
// 判定规则内置且确定性（同一分片字节 → 同一事件序列，无随机、无状态、
// 不落盘）：对分片取 FNV-1a 哈希，h%4==0 判静默段（无事件），否则按
// h%3 从 cheer/boo/volume_spike 中取一种，置信度 0.55 + (h%40)/100。
// 真镜像的部署与调参不在本轮（docker-compose 的 sensevoice-aed 为占位）。

import (
	"encoding/json"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"qiuqiu/internal/ambient"
)

// fakePortKey 是假 sidecar 的监听端口环境变量，与 compose 定义对齐。
const fakePortKey = "AMBIENT_AED_PORT"

func main() {
	port := portFromEnv()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/aevents", handleAEvents)
	addr := ":" + port
	log.Printf("ambient-fake sidecar listening on %s (deterministic pr-tier stub)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("ambient-fake server: %v", err)
	}
}

// portFromEnv 读 AMBIENT_AED_PORT（默认 8090，compose 占位服务同端口）。
func portFromEnv() string {
	if raw := os.Getenv(fakePortKey); raw != "" {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 && port < 65536 {
			return strconv.Itoa(port)
		}
	}
	return "8090"
}

// handleAEvents 实现与真 sidecar 相同的请求/响应形状；判定见文件头注释。
func handleAEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	pcm := make([]byte, 0, r.ContentLength)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Body.Read(buf)
		pcm = append(pcm, buf[:n]...)
		if err != nil {
			break
		}
	}
	events := classifyDeterministic(pcm, time.Now().UTC())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

// classifyDeterministic 是假 sidecar 的确定性判定核心（可单测）。
func classifyDeterministic(pcm []byte, now time.Time) []ambient.Event {
	if len(pcm) == 0 {
		return nil
	}
	hash := fnv.New64a()
	_, _ = hash.Write(pcm)
	h := hash.Sum64()
	if h%4 == 0 {
		return []ambient.Event{}
	}
	kinds := []string{ambient.KindCheer, ambient.KindBoo, ambient.KindVolumeSpike}
	return []ambient.Event{{
		Kind:       kinds[h%3],
		Confidence: 0.55 + float64(h%40)/100,
		TS:         now,
	}}
}
