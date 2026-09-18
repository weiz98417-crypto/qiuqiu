import { useRef, useState } from 'react';
import { Alert, App as AntApp, Button, Card, Input, Select, Space, Typography } from 'antd';
import type { VoiceConflict } from './event-model';
import { applyVoiceConflict, applyVoiceDraft } from './event-model';
import type { Draft } from './event-model';
import { submitVoiceDraft, publishVoiceDraft } from './api';

const { Text } = Typography;

interface VoiceDraftProps {
  matchId: string;
  draft: Draft;
  clock: { period: string; elapsedSeconds: number; capturedClockVersion: number };
  busy: boolean;
  onDraftApplied: (draft: Draft, conflicts: VoiceConflict[]) => void;
  onPublished: (snapshot?: { score?: { home: number; away: number } }) => void;
}

interface AudioRecording {
  chunks: Float32Array[];
  context: AudioContext;
  sampleRate: number;
  deviceName: string;
}

// 语音录入流（ADR-0011 task 3.1）：getUserMedia 采集 → 16k WAV →
// POST drafts/voice → 转写 + 结构化草稿 + 冲突卡 → 确认后
// drafts/voice/publish。采集与编码方式与老页面逐字对齐。
export default function VoiceDraft({ matchId, draft, clock, busy, onDraftApplied, onPublished }: VoiceDraftProps) {
  const { message: messageApi } = AntApp.useApp();
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([]);
  const [deviceId, setDeviceId] = useState('');
  const [recording, setRecording] = useState(false);
  const [status, setStatus] = useState('准备录音');
  const [transcript, setTranscript] = useState('');
  const [conflicts, setConflicts] = useState<VoiceConflict[]>([]);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [transcribing, setTranscribing] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const recordingRef = useRef<AudioRecording | null>(null);
  const draftBeforeVoiceRef = useRef<Draft | null>(null);

  const refreshDevices = async () => {
    if (!navigator.mediaDevices?.enumerateDevices) return;
    const inputs = (await navigator.mediaDevices.enumerateDevices()).filter((device) => device.kind === 'audioinput');
    setDevices(inputs);
    if (!deviceId && inputs.length) {
      const preferred = inputs.find((device) => /usb.*(condenser|microphone)|condenser.*microphone/i.test(device.label));
      if (preferred) setDeviceId(preferred.deviceId);
    }
  };

  const startRecording = async () => {
    try {
      const constraints: MediaStreamConstraints = {
        audio: {
          channelCount: 1,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
          ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
        } as MediaTrackConstraints,
      };
      const stream = await navigator.mediaDevices.getUserMedia(constraints);
      const context = new AudioContext();
      const sourceNode = context.createMediaStreamSource(stream);
      const processor = context.createScriptProcessor(4096, 1, 1);
      const chunks: Float32Array[] = [];
      processor.onaudioprocess = (event) => {
        chunks.push(new Float32Array(event.inputBuffer.getChannelData(0)));
      };
      sourceNode.connect(processor);
      processor.connect(context.destination);
      const deviceName = inputsLabel(stream, deviceId) || '默认麦克风';
      recordingRef.current = { chunks, context, sampleRate: context.sampleRate, deviceName };
      setRecording(true);
      setStatus(`录音中（${deviceName}）… 说完后再点一次停止`);
    } catch (err) {
      messageApi.error(`麦克风打开失败：${err instanceof Error ? err.message : String(err)}`);
    }
  };

  const inputsLabel = (stream: MediaStream, id: string): string => {
    const track = stream.getAudioTracks()[0];
    void id;
    return track?.label || '';
  };

  const stopRecording = async () => {
    const current = recordingRef.current;
    if (!current) return;
    setRecording(false);
    setStatus('正在转写语音');
    setTranscribing(true);
    try {
      await current.context.close();
      const metrics = audioMetrics(current.chunks, current.sampleRate);
      if (metrics.durationSeconds < 0.4 || metrics.rms < 0.002) {
        throw new Error(
          `没有采集到足够清晰的人声（${current.deviceName}，平均音量 ${metrics.rms.toFixed(4)}），请切换麦克风后重试`,
        );
      }
      const normalizedPCM = resamplePCM(current.chunks, current.sampleRate);
      const blob = pcmToWAV([normalizedPCM], 16000);
      if (blob.size <= 44) throw new Error('没有采集到可识别的语音，请说完后再停止录音');
      const audioBase64 = await blobToDataURL(blob);
      const result = await submitVoiceDraft(matchId, {
        audioBase64,
        audioMime: 'audio/wav',
        audioDurationMs: Math.round(metrics.durationSeconds * 1000),
        audioRMS: Number(metrics.rms.toFixed(4)),
        audioSampleRate: 16000,
        occurredPeriod: clock.period,
        occurredSeconds: clock.elapsedSeconds,
        capturedClockVersion: clock.capturedClockVersion,
      });
      const transcriptText = result.transcript || '';
      const structured = (result.draft && typeof result.draft === 'object' ? result.draft : {}) as Partial<Draft>;
      const applied = applyVoiceDraft(draft, {
        ...structured,
        source: 'operator_voice',
        transcript: transcriptText,
        description: (structured.description as string) || transcriptText,
      } as Partial<Draft>);
      draftBeforeVoiceRef.current = draft;
      onDraftApplied(applied.draft, applied.conflicts);
      setConflicts(applied.conflicts || []);
      const nextWarnings = [...(result.warnings || [])];
      if (!containsChineseText(transcriptText)) {
        nextWarnings.push(`转写结果未包含中文（录音 ${metrics.durationSeconds.toFixed(1)} 秒，音量 ${metrics.rms.toFixed(3)}），请重试或手动更正。`);
      }
      setWarnings(nextWarnings);
      setTranscript(transcriptText);
      setStatus(nextWarnings.length ? '转写结果可能异常，请重试或手动更正' : '语音已转成文字；可直接修改，确认后发布事件');
    } catch (err) {
      setStatus('转写失败，请重试');
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setTranscribing(false);
      recordingRef.current = null;
    }
  };

  const undoVoice = () => {
    if (draftBeforeVoiceRef.current) {
      onDraftApplied(draftBeforeVoiceRef.current, []);
      setTranscript('');
      setConflicts([]);
      setStatus('已撤回本次语音');
    }
  };

  const resolveConflict = (conflict: VoiceConflict) => {
    onDraftApplied(applyVoiceConflict(draft, conflict), conflicts.filter((item) => item !== conflict));
    setConflicts((current) => current.filter((item) => item !== conflict));
  };

  const publish = async () => {
    const text = cleanDirectorText(transcript);
    if (!text) {
      messageApi.warning('请先确认或补充语音转写内容');
      return;
    }
    setPublishing(true);
    try {
      const result = await publishVoiceDraft(matchId, {
        text,
        occurredPeriod: clock.period,
        occurredSeconds: clock.elapsedSeconds,
        capturedClockVersion: clock.capturedClockVersion,
      });
      onPublished(result.snapshot);
      setTranscript('');
      setConflicts([]);
      setStatus('已确认并发送');
      messageApi.success(`已确认并发送：${result.event?.eventType || '赛事事件'}`);
    } catch (err) {
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setPublishing(false);
    }
  };

  return (
    <Card
      data-testid="director-voice"
      title="语音录入"
      style={{ border: '1px solid #253142' }}
      styles={{ body: { padding: 12 } }}
    >
      <Space direction="vertical" style={{ width: '100%' }} size={8}>
        <Space wrap>
          <Select
            aria-label="语音录入麦克风"
            style={{ minWidth: 180 }}
            value={deviceId}
            onFocus={refreshDevices}
            onDropdownVisibleChange={refreshDevices}
            onChange={(value) => setDeviceId(value)}
            options={[
              { value: '', label: '默认麦克风' },
              ...devices.map((device, index) => ({ value: device.deviceId, label: device.label || `麦克风 ${index + 1}` })),
            ]}
          />
          {recording ? (
            <Button danger onClick={stopRecording} loading={transcribing}>
              停止录音
            </Button>
          ) : (
            <Button type="primary" ghost onClick={startRecording} disabled={busy || transcribing}>
              语音录入
            </Button>
          )}
          <Button size="small" onClick={undoVoice} disabled={!draftBeforeVoiceRef.current}>
            撤回本次语音
          </Button>
        </Space>
        <Text type="secondary">{status}</Text>
        {warnings.map((warning) => (
          <Alert key={warning} type="warning" showIcon message={warning} />
        ))}
        <Input.TextArea
          aria-label="语音转写内容"
          rows={2}
          value={transcript}
          placeholder="语音转写会显示在这里，可直接修改"
          onChange={(event: React.ChangeEvent<HTMLTextAreaElement>) => setTranscript(event.target.value)}
        />
        {conflicts.map((conflict) => (
          <Alert
            key={conflict.field}
            type="warning"
            showIcon
            message={`冲突：${conflict.field}`}
            description={
              <Space direction="vertical" size={4} style={{ width: '100%' }}>
                <Text type="secondary">
                  当前 {String(conflict.current ?? '空')} → 语音识别 {String(conflict.incoming ?? '')}
                </Text>
                <Button size="small" onClick={() => resolveConflict(conflict)}>
                  采用语音识别结果
                </Button>
              </Space>
            }
          />
        ))}
        <Button
          data-testid="voice-publish"
          type="primary"
          block
          loading={publishing}
          disabled={busy || recording}
          onClick={publish}
        >
          确认语音并发布事件
        </Button>
      </Space>
    </Card>
  );
}

function cleanDirectorText(text: string): string {
  return String(text || '')
    .split('\n')
    .filter((line) => line.trim() && !/^(参与人|球球主动说)[:：]/.test(line.trim()))
    .join('\n')
    .trim();
}

function containsChineseText(text: string): boolean {
  return /[\u4e00-\u9fff]/.test(String(text || ''));
}

function blobToDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error || new Error('读取录音失败'));
    reader.onload = () => resolve(String(reader.result || ''));
    reader.readAsDataURL(blob);
  });
}

function pcmToWAV(chunks: Float32Array[], sampleRate: number): Blob {
  const sampleCount = chunks.reduce((total, chunk) => total + chunk.length, 0);
  const buffer = new ArrayBuffer(44 + sampleCount * 2);
  const view = new DataView(buffer);
  const writeText = (offset: number, text: string) =>
    [...text].forEach((character, index) => view.setUint8(offset + index, character.charCodeAt(0)));
  writeText(0, 'RIFF');
  view.setUint32(4, 36 + sampleCount * 2, true);
  writeText(8, 'WAVE');
  writeText(12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  writeText(36, 'data');
  view.setUint32(40, sampleCount * 2, true);
  let offset = 44;
  chunks.forEach((chunk) => {
    chunk.forEach((sample) => {
      const normalized = Math.max(-1, Math.min(1, sample));
      view.setInt16(offset, normalized < 0 ? normalized * 32768 : normalized * 32767, true);
      offset += 2;
    });
  });
  return new Blob([buffer], { type: 'audio/wav' });
}

function resamplePCM(chunks: Float32Array[], sourceRate: number, targetRate = 16000): Float32Array {
  const sampleCount = chunks.reduce((total, chunk) => total + chunk.length, 0);
  const source = new Float32Array(sampleCount);
  let sourceOffset = 0;
  chunks.forEach((chunk) => {
    source.set(chunk, sourceOffset);
    sourceOffset += chunk.length;
  });
  if (sourceRate === targetRate) return source;
  const target = new Float32Array(Math.max(1, Math.round((source.length * targetRate) / sourceRate)));
  const ratio = sourceRate / targetRate;
  for (let index = 0; index < target.length; index++) {
    const start = Math.floor(index * ratio);
    const end = Math.min(source.length, Math.max(start + 1, Math.floor((index + 1) * ratio)));
    let total = 0;
    for (let sourceIndex = start; sourceIndex < end; sourceIndex++) total += source[sourceIndex];
    target[index] = total / Math.max(1, end - start);
  }
  return target;
}

function audioMetrics(chunks: Float32Array[], sampleRate: number): { durationSeconds: number; rms: number } {
  let sampleCount = 0;
  let sumSquares = 0;
  for (const chunk of chunks) {
    sampleCount += chunk.length;
    for (const sample of chunk) sumSquares += sample * sample;
  }
  return {
    durationSeconds: sampleCount / Math.max(1, Number(sampleRate || 1)),
    rms: sampleCount ? Math.sqrt(sumSquares / sampleCount) : 0,
  };
}
