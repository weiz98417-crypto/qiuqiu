import { useState } from 'react';
import { Alert, Badge, Button, Card, Radio, Space, Typography } from 'antd';
import { ttsSupplyApi, ttsSupplyModeLabels } from '../api/ttsSupply';
import type { TtsSupplyState } from '../api/ttsSupply';
import { fmtDateTime } from '../api/format';
import { useAsync } from '../api/useAsync';

const { Text } = Typography;

// 语音供给卡（tts-supply-switch 7.3）：三态切换 + 本地腿健康门控快照 +
// 回退计数。供给是部署级事实（全局单值），健康门未过的本地选项置灰并
// 显示原因——门控在服务端 PATCH 也拦（409），前端置灰只是体验层。
export default function TtsSupplyCard() {
  const { data, loading, error, reload } = useAsync<TtsSupplyState>(() => ttsSupplyApi.get(), []);
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const mode = draft ?? data?.mode ?? 'cloud';
  const selectable = data?.localSelectable ?? false;
  const dirty = data != null && draft != null && draft !== data.mode;

  const save = async () => {
    if (!dirty || draft == null) return;
    setSaving(true);
    setSaveError(null);
    try {
      await ttsSupplyApi.update(draft);
      setDraft(null);
      await reload();
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const local = data?.local;

  return (
    <Card>
      {error ? <Alert type="error" showIcon message="语音供给状态加载失败" description={error} /> : null}
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <Radio.Group
          value={mode}
          onChange={(event) => setDraft(event.target.value)}
          disabled={loading || !data}
          options={[
            { value: 'cloud', label: ttsSupplyModeLabels.cloud },
            { value: 'local_first', label: ttsSupplyModeLabels.local_first, disabled: !selectable },
            { value: 'local', label: ttsSupplyModeLabels.local, disabled: !selectable },
          ]}
        />
        <div>
          <Badge
            status={local?.available ? 'success' : 'error'}
            text={
              local?.configured
                ? local.available
                  ? '本地引擎健康'
                  : `本地引擎不可用：${local.reason ?? '未知原因'}`
                : '本地引擎未配置（QIUQIU_TTS_LOCAL_URL 留空）'
            }
          />
        </div>
        <Text type="secondary" style={{ fontSize: 12 }}>
          探测 {local?.probes ?? 0} 次（失败 {local?.failures ?? 0}）
          {local?.lastCheckAt ? ` · 上次 ${fmtDateTime(local.lastCheckAt)}` : ''}
          {` · 本地失败累计 ${data?.localFailures ?? 0} · 回云兜底 ${data?.cloudFallbacks ?? 0}`}
        </Text>
        {saveError ? <Alert type="error" showIcon message={saveError} closable onClose={() => setSaveError(null)} /> : null}
        <Space>
          <Button type="primary" size="small" disabled={!dirty || saving} loading={saving} onClick={save}>
            应用切换
          </Button>
          {dirty ? (
            <Button size="small" disabled={saving} onClick={() => { setDraft(null); setSaveError(null); }}>
              撤销
            </Button>
          ) : null}
        </Space>
      </Space>
    </Card>
  );
}

