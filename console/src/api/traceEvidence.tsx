import { Drawer, Space, Tag, Tooltip, Typography } from 'antd';
import { VOICE_LATENCY_STAGES, VOICE_STAGE_LABELS } from './client';
import type { TraceRow } from './client';
import { fmtDateTime, reasonCodeLabel } from './format';

// 「为什么说话」证据 module：trace 原因码提取、引用前缀匹配、详情抽屉、
// 语音回合回放。Match 页与引用审计页共用，改 trace 语义只动这一个文件。

export function traceReasonCodes(trace: TraceRow): string[] {
  if (trace.reasonCodes?.length) return trace.reasonCodes;
  return trace.relationshipDecision?.reasonCodes ?? [];
}

// citation 前缀在 reasonCodes 上做前缀匹配（与后端 citation= 过滤一致）。
export function matchesCitation(trace: TraceRow, prefix: string): boolean {
  if (!prefix) return true;
  const haystack = [trace.reason ?? '', ...traceReasonCodes(trace)];
  return haystack.some((code) => code.startsWith(prefix));
}

// RAG 命中数：toolCalls 里 knowledge/search 类调用的次数（0 = 本回合未检索）。
export function ragCallCount(trace: TraceRow): number {
  return (trace.toolCalls ?? []).filter((call) => /knowledge|search/i.test(call.name)).length;
}

const { Text } = Typography;

export function WhyDrawer({ trace, onClose }: { trace: TraceRow | null; onClose: () => void }) {
  return (
    <Drawer title="为什么说话" open={Boolean(trace)} onClose={onClose} width={480}>
      {trace ? (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <div>
            <Text type="secondary">Trace</Text>
            <div>
              <code>{trace.id}</code>
            </div>
          </div>
          <div>
            <Text type="secondary">原因码</Text>
            <div>
              <Space size={4} wrap>
                {traceReasonCodes(trace).map((code) => (
                  <Tag key={code} color="orange">
                    {reasonCodeLabel(code)}
                  </Tag>
                ))}
                {!traceReasonCodes(trace).length ? <Text type="secondary">{trace.reason || '—'}</Text> : null}
              </Space>
            </div>
          </div>
          <div>
            <Text type="secondary">用户输入</Text>
            <div>{trace.input || '（主动回合，无用户输入）'}</div>
          </div>
          <div>
            <Text type="secondary">球球输出</Text>
            <div>{trace.output || '—'}</div>
          </div>
          <div>
            <Text type="secondary">时间</Text>
            <div>{fmtDateTime(trace.createdAt)}</div>
          </div>
          <TurnReplay trace={trace} />
        </Space>
      ) : null}
    </Drawer>
  );
}

// ---- 语音回合回放（operations-turn-replay）----

// 延迟分段顺序与中文标签单源自 client.ts（VOICE_LATENCY_STAGES /
// VOICE_STAGE_LABELS，与后端 companion/voice_stages.go 互指）；配色本地维护
// （色源 theme/palette：skyBlue/yellow/green；橙色为 TTS 首响段——主强调色
// 标记用户听到第一声的时刻；紫色为 TTS 合成段）。缺 stage = 未测得，时间轴
// 只画测得的段（整段合成路径无 tts_first_audio，照常缺段容错）。
const STAGE_COLORS: Record<string, string> = {
  speech_received: '#7C8794',
  asr_final: '#55A8FF',
  turn_decided: '#F2C94C',
  tts_first_audio: '#FF6B35',
  tts_synthesized: '#9B7EDE',
  audio_delivered: '#5FCB8B',
};

function formatBytes(count: number): string {
  if (count >= 1024 * 1024) return `${(count / 1024 / 1024).toFixed(1)} MB`;
  if (count >= 1024) return `${(count / 1024).toFixed(1)} KB`;
  return `${count} B`;
}

// formatAffectConfidence 伪置信展示口径：两位小数后去尾零（0.9 → "0.9"，
// 0.87 → "0.87"），与 sidecar 的非校准伪置信精度相称。
function formatAffectConfidence(confidence: number): string {
  return String(Number(confidence.toFixed(2)));
}

function turnDecisionLabel(isComplete: boolean | null | undefined): string {
  if (isComplete === true) return '判定完句';
  if (isComplete === false) return '判定未完';
  return '未决（降级）';
}

// TurnReplay 渲染一条语音回合的链路观测：横向延迟分段轨道 + TTS/轮次判定/
// RAG/ASR 状态小字，失败段红色标注。隐私纪律：不渲染 input/output/asrText
// 正文——正文展示仍由 WhyDrawer 等既有抽屉负责，本组件只做链路侧写。
export function TurnReplay({ trace }: { trace: TraceRow }) {
  const voice = trace.voice;
  // 只取测得的段（缺 stage 不画）；latencyStages 为累计毫秒，轨道按相对
  // 总长百分比分宽。只有锚点（恒 0）或没有分段时不画轨道。
  const stages = VOICE_LATENCY_STAGES.filter((stage) => typeof voice?.latencyStages?.[stage] === 'number').map(
    (stage) => ({ stage, at: voice?.latencyStages?.[stage] ?? 0 }),
  );
  const totalMs = stages.length ? Math.max(...stages.map((entry) => entry.at)) : 0;
  const ragCalls = ragCallCount(trace);
  const decision = voice?.turnDecision;
  const decisionParts = decision
    ? [
        turnDecisionLabel(decision.isComplete),
        decision.source,
        decision.queryLatencyMs !== undefined ? `${decision.queryLatencyMs}ms` : '',
      ].filter(Boolean)
    : [];
  const ttsParts = [voice?.ttsStatus, voice?.ttsByteCount !== undefined ? formatBytes(voice.ttsByteCount) : '', voice?.ttsMime].filter(
    Boolean,
  );

  return (
    <Space direction="vertical" size={4} style={{ width: '100%' }}>
      <Text type="secondary">语音回放</Text>
      {stages.length > 1 ? (
        <div style={{ display: 'flex', height: 14, borderRadius: 4, overflow: 'hidden', background: '#182234' }}>
          {stages.map(({ stage, at }) => (
            <Tooltip key={stage} title={`${VOICE_STAGE_LABELS[stage] ?? stage} · 累计 ${at} ms`}>
              <div
                data-stage={stage}
                style={{
                  width: `${totalMs > 0 ? (at / totalMs) * 100 : 0}%`,
                  minWidth: at > 0 ? 3 : 0,
                  background: STAGE_COLORS[stage] ?? '#7C8794',
                }}
              />
            </Tooltip>
          ))}
        </div>
      ) : (
        <Text type="secondary">无分段数据</Text>
      )}
      {voice?.asrError || voice?.ttsError ? (
        <Space size={4} wrap>
          {voice?.asrError ? <Tag color="red">ASR 失败：{voice.asrError}</Tag> : null}
          {voice?.ttsError ? <Tag color="red">TTS 失败：{voice.ttsError}</Tag> : null}
        </Space>
      ) : null}
      {/* 用户语音情绪（user-voice-affect）：可选字段，缺 = 本回合未测得不占位。
          三层隐私纪律：chip 只含标签与伪置信数值，无任何正文。 */}
      {voice?.userAffect ? (
        <div>
          <Tag color="magenta">{`用户情绪：${voice.userAffect.label} ${formatAffectConfidence(voice.userAffect.confidence)}`}</Tag>
        </div>
      ) : null}
      <div style={{ fontSize: 12, color: '#AAB4C0' }}>TTS {ttsParts.length ? ttsParts.join(' · ') : '—'}</div>
      <div style={{ fontSize: 12, color: '#AAB4C0' }}>
        轮次判定 {decisionParts.length ? decisionParts.join(' · ') : '—'}
      </div>
      <div style={{ fontSize: 12, color: '#AAB4C0' }}>RAG 检索 {ragCalls > 0 ? `${ragCalls} 次` : '—'}</div>
      <div style={{ fontSize: 12, color: '#AAB4C0' }}>
        ASR {voice?.asrStatus || '—'}
        {voice?.asrError ? <span style={{ color: '#F05D5E' }}> · {voice.asrError}</span> : null}
      </div>
    </Space>
  );
}
