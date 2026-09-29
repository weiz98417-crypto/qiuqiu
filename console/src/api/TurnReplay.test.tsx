import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { TurnReplay } from './traceEvidence';
import { VOICE_LATENCY_STAGES } from './client';
import type { TraceRow } from './client';

// TurnReplay 单测：延迟分段时间轴的多态（六段全测/缺段容错）+ userAffect
// 显形 + 隐私纪律（不渲染任何正文）。

function traceWith(voice: TraceRow['voice']): TraceRow {
  // input/output/asrText 塞入哨兵正文，专供隐私断言（渲染产物必须不含）。
  return {
    id: 'trace-1',
    input: '哨兵输入正文',
    output: '哨兵输出正文',
    voice: { ...voice, asrText: '哨兵语音正文' },
  };
}

// 完整六段（累计毫秒，总长 2100）：锚点 0 宽不可见，视觉为五色段。
const FULL_STAGES = {
  speech_received: 0,
  asr_final: 320,
  turn_decided: 900,
  tts_first_audio: 1200,
  tts_synthesized: 1600,
  audio_delivered: 2100,
};

describe('TurnReplay', () => {
  it('完整六段渲染全部色段，宽度为累计毫秒相对总长的百分比，段序与契约常量一致', () => {
    const { container } = render(<TurnReplay trace={traceWith({ latencyStages: FULL_STAGES })} />);
    const segments = container.querySelectorAll<HTMLElement>('[data-stage]');
    // 六段都在 DOM（锚点 speech_received 宽 0），tts_first_audio 插在
    // turn_decided 与 tts_synthesized 之间。
    expect(segments).toHaveLength(6);
    expect(Array.from(segments).map((segment) => segment.dataset.stage)).toEqual([...VOICE_LATENCY_STAGES]);
    const expectWidth = (stage: string, at: number) => {
      const segment = container.querySelector<HTMLElement>(`[data-stage="${stage}"]`);
      expect(segment).not.toBeNull();
      expect(parseFloat(segment!.style.width)).toBeCloseTo((at / 2100) * 100, 6);
    };
    expectWidth('speech_received', 0);
    expectWidth('asr_final', 320);
    expectWidth('turn_decided', 900);
    expectWidth('tts_first_audio', 1200);
    expectWidth('tts_synthesized', 1600);
    expectWidth('audio_delivered', 2100);
  });

  it('整段合成路径缺 tts_first_audio（缺段容错）：只画测得的五段', () => {
    // 整段合成路径没有 tts_first_audio 段（backend 不落该键）。
    const withoutFirstAudio: NonNullable<TraceRow['voice']>['latencyStages'] = {
      speech_received: 0,
      asr_final: 320,
      turn_decided: 900,
      tts_synthesized: 1600,
      audio_delivered: 2100,
    };
    const { container } = render(<TurnReplay trace={traceWith({ latencyStages: withoutFirstAudio })} />);
    const segments = container.querySelectorAll<HTMLElement>('[data-stage]');
    expect(segments).toHaveLength(5);
    expect(container.querySelector('[data-stage="tts_first_audio"]')).toBeNull();
    // 剩余段宽度仍按各自累计毫秒相对总长分宽（总长不受缺段影响）。
    const synthesized = container.querySelector<HTMLElement>('[data-stage="tts_synthesized"]');
    expect(parseFloat(synthesized!.style.width)).toBeCloseTo((1600 / 2100) * 100, 6);
  });

  it('缺段（只有锚点）时不画轨道，显示无分段数据占位', () => {
    render(<TurnReplay trace={traceWith({ latencyStages: { speech_received: 0 } })} />);
    expect(screen.getByText('无分段数据')).toBeInTheDocument();
  });

  it('asrError/ttsError 非空时出红色失败标注', () => {
    const { container } = render(
      <TurnReplay
        trace={traceWith({
          latencyStages: FULL_STAGES,
          asrError: '识别超时',
          ttsError: '合成失败',
        })}
      />,
    );
    // antd Tag 内文本节点分段，getByText 会多匹配；直接断言容器全文。
    expect(container.textContent).toContain('ASR 失败：识别超时');
    expect(container.textContent).toContain('TTS 失败：合成失败');
  });

  it('turnDecision 三态文案：判定完句 / 判定未完 / 未决（降级）', () => {
    const decisionOf = (isComplete: boolean | null) => ({
      latencyStages: FULL_STAGES,
      turnDecision: { isComplete, source: 'model', queryLatencyMs: 320 },
    });
    const rowText = (isComplete: boolean | null) => {
      const { container, unmount } = render(<TurnReplay trace={traceWith(decisionOf(isComplete))} />);
      const text = container.textContent ?? '';
      unmount();
      return text;
    };
    expect(rowText(true)).toContain('轮次判定 判定完句 · model · 320ms');
    expect(rowText(false)).toContain('轮次判定 判定未完 · model · 320ms');
    expect(rowText(null)).toContain('轮次判定 未决（降级） · model · 320ms');
  });

  it('userAffect 存在时出情绪 chip：标签 + 伪置信（0.9 → "0.9"）', () => {
    const { container } = render(
      <TurnReplay trace={traceWith({ latencyStages: FULL_STAGES, userAffect: { label: 'excited', confidence: 0.9 } })} />,
    );
    expect(container.textContent).toContain('用户情绪：excited 0.9');
  });

  it('userAffect 伪置信两位小数后去尾零：0.87 → "0.87"，1 → "1"', () => {
    const rowText = (confidence: number) => {
      const { container, unmount } = render(
        <TurnReplay trace={traceWith({ latencyStages: FULL_STAGES, userAffect: { label: 'calm', confidence } })} />,
      );
      const text = container.textContent ?? '';
      unmount();
      return text;
    };
    expect(rowText(0.87)).toContain('用户情绪：calm 0.87');
    expect(rowText(1)).toContain('用户情绪：calm 1');
  });

  it('userAffect 缺省不占位：渲染产物无「用户情绪」字样', () => {
    const { container } = render(<TurnReplay trace={traceWith({ latencyStages: FULL_STAGES })} />);
    expect(container.textContent).not.toContain('用户情绪');
  });

  it('隐私纪律：渲染产物不含 input/output/asrText 哨兵正文', () => {
    const { container } = render(
      <TurnReplay
        trace={traceWith({
          latencyStages: FULL_STAGES,
          turnDecision: { isComplete: null, source: 'unavailable' },
          userAffect: { label: 'excited', confidence: 0.9 },
        })}
      />,
    );
    expect(container.textContent).not.toContain('哨兵输入正文');
    expect(container.textContent).not.toContain('哨兵输出正文');
    expect(container.textContent).not.toContain('哨兵语音正文');
    expect(screen.queryByText('哨兵输入正文')).not.toBeInTheDocument();
    expect(screen.queryByText('哨兵输出正文')).not.toBeInTheDocument();
  });
});
