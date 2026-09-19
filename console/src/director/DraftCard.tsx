import { useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Form, Input, Row, Select, Space, Tag, Typography } from 'antd';
import type { Draft } from './event-model';
import { eventDefinitions, validateDraft } from './event-model';
import type { DraftFormState } from './draft-form';
import { directorTemplates } from './templates';

// 表单状态形状住在纯 module draft-form（映射逻辑与形状同源），这里再导出。
export type { DraftFormState };

const { Text } = Typography;

const ACTION_OPTIONS = [
  { value: '', label: '自动' },
  { value: 'celebrate', label: 'celebrate' },
  { value: 'tense', label: 'tense' },
  { value: 'miss', label: 'miss' },
  { value: 'complain', label: 'complain' },
  { value: 'angry', label: 'angry' },
  { value: 'analysis', label: 'analysis' },
  { value: 'focus', label: 'focus' },
  { value: 'surprise', label: 'surprise' },
  { value: 'comfort', label: 'comfort' },
];

const MODE_OPTIONS = [
  { value: 'auto', label: '自动反应' },
  { value: 'quiet', label: '只记事实' },
  { value: 'manual', label: '人工话术' },
];

const FACT_STATUS_OPTIONS = [
  { value: 'pending', label: '候选，等待确认' },
  { value: 'confirmed', label: '确认事实' },
];

const INTENSITY_OPTIONS = [1, 2, 3, 4, 5].map((value) => ({
  value: String(value),
  label: `${value} ${['', '安静', '轻微', '明显', '强烈', '爆发'][value]}`,
}));

interface DraftCardProps {
  draft: Draft;
  form: DraftFormState;
  onFormChange: (patch: Partial<DraftFormState>) => void;
  homeTeam: string;
  awayTeam: string;
  confirmedScore: { home: number; away: number };
  correctingId: string;
  busy: boolean;
  onSubmit: (factStatus: 'confirmed' | 'pending') => void;
  onClear: () => void;
}

// 事件草稿卡（ADR-0011 tasks 2.1）：事实状态、比分更正 + 原因、球球处理
// auto/quiet/manual + 人工话术、模板、发送预览。请求形状与 operator-control
// evals 断言逐字一致（提交由父组件经 toEventPayload 组装）。
export default function DraftCard({
  draft,
  form,
  onFormChange,
  homeTeam,
  awayTeam,
  confirmedScore,
  correctingId,
  busy,
  onSubmit,
  onClear,
}: DraftCardProps) {
  const [templateSearch, setTemplateSearch] = useState('');
  const definition = draft.eventType ? eventDefinitions[draft.eventType] : null;
  const validation = useMemo(() => validateDraft(draft), [draft]);
  const isScoreCorrection = draft.eventType === 'score_correction';
  const showCorrectionReason = Boolean(draft.revisionOf) || isScoreCorrection;
  const showProactive = draft.deliveryMode === 'manual';

  const templates = directorTemplates.filter(
    (template) =>
      template.type === draft.eventType &&
      (!templateSearch.trim() || template.text.includes(templateSearch.trim())),
  );

  const preview = `${homeTeam || '主队'} ${confirmedScore.home}-${confirmedScore.away} ${awayTeam || '客队'}\n${
    draft.eventType ? `${definition?.label ?? draft.eventType} · ${form.occurredClock} · ${form.factStatus}` : '尚未选择行为'
  }\n${draft.description || form.description || '（描述待填）'}`;

  return (
    <Card
      data-testid="director-draft-card"
      title={correctingId ? `更正事实 · ${correctingId.slice(0, 12)}…` : '当前事件草稿'}
      extra={
        <Space>
          <Button size="small" onClick={onClear} disabled={busy}>
            清空草稿
          </Button>
        </Space>
      }
      style={{ border: '1px solid #253142' }}
    >
      <Space direction="vertical" style={{ width: '100%' }} size="small">
        <Space wrap size="small">
          <Tag color="blue">{definition ? definition.label : '未选行为'}</Tag>
          <Text type="secondary">参与人：{draft.primaryParticipant?.name || '未选'}</Text>
          <Text type="secondary">时钟版本 {draft.capturedClockVersion}</Text>
        </Space>

        {!validation.ready ? (
          <Alert
            type="warning"
            showIcon
            data-testid="draft-errors"
            message={validation.errors[0]}
            description={
              validation.errors.length > 1 ? (
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {validation.errors.slice(1).map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              ) : undefined
            }
          />
        ) : (
          <Alert type="success" showIcon message="草稿就绪，可发送" />
        )}

        <Row gutter={[8, 8]}>
          <Col span={8}>
            <Form.Item label="主参与人" style={{ marginBottom: 0 }}>
              <Input
                aria-label="主参与人"
                value={form.mainPlayer}
                placeholder="从左侧选择球员"
                onChange={(event) => onFormChange({ mainPlayer: event.target.value })}
              />
            </Form.Item>
          </Col>
          <Col span={6}>
            <Form.Item label="事件时间" style={{ marginBottom: 0 }}>
              <Input
                aria-label="事件时间"
                value={form.occurredClock}
                inputMode="numeric"
                onChange={(event) => onFormChange({ occurredClock: event.target.value })}
              />
            </Form.Item>
          </Col>
          <Col span={5}>
            <Form.Item label="强度" style={{ marginBottom: 0 }}>
              <Select
                aria-label="强度"
                options={INTENSITY_OPTIONS}
                value={form.intensity}
                onChange={(value) => onFormChange({ intensity: value })}
              />
            </Form.Item>
          </Col>
          <Col span={5}>
            <Form.Item label="事实状态" style={{ marginBottom: 0 }}>
              <Select
                aria-label="事实状态"
                options={FACT_STATUS_OPTIONS}
                value={form.factStatus}
                onChange={(value) => onFormChange({ factStatus: value })}
              />
            </Form.Item>
          </Col>
          <Col span={6}>
            <Form.Item label="推荐动作" style={{ marginBottom: 0 }}>
              <Select
                aria-label="推荐动作"
                options={ACTION_OPTIONS}
                value={form.action}
                onChange={(value) => onFormChange({ action: value })}
              />
            </Form.Item>
          </Col>
          <Col span={8}>
            <Form.Item label="球球处理" style={{ marginBottom: 0 }}>
              <Select
                data-testid={"draft-mode-select"}
                aria-label="球球处理"
                options={MODE_OPTIONS}
                value={form.mode}
                onChange={(value) => onFormChange({ mode: value })}
              />
            </Form.Item>
          </Col>
        </Row>

        <Form.Item label="事件描述" style={{ marginBottom: 0 }}>
          <Input.TextArea
            aria-label="事件描述"
            rows={2}
            value={form.description}
            placeholder="描述场上发生了什么"
            onChange={(event) => onFormChange({ description: event.target.value })}
          />
        </Form.Item>

        {isScoreCorrection ? (
          <Row gutter={8}>
            <Col span={6}>
              <Form.Item label="更正后主队比分" style={{ marginBottom: 0 }}>
                <Input
                  aria-label="更正后主队比分"
                  type="number"
                  min={0}
                  value={form.scoreHome}
                  onChange={(event) => onFormChange({ scoreHome: event.target.value })}
                />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item label="更正后客队比分" style={{ marginBottom: 0 }}>
                <Input
                  aria-label="更正后客队比分"
                  type="number"
                  min={0}
                  value={form.scoreAway}
                  onChange={(event) => onFormChange({ scoreAway: event.target.value })}
                />
              </Form.Item>
            </Col>
          </Row>
        ) : null}

        {showCorrectionReason ? (
          <Form.Item label="更正或采用原因" style={{ marginBottom: 0 }}>
            <Input.TextArea
              aria-label="更正或采用原因"
              rows={2}
              value={form.correctionReason}
              placeholder="说明为什么修改原记录，便于后续审计"
              onChange={(event) => onFormChange({ correctionReason: event.target.value })}
            />
          </Form.Item>
        ) : null}

        {showProactive ? (
          <Form.Item label="球球主动话术" style={{ marginBottom: 0 }}>
            <Input.TextArea
              aria-label="球球主动话术"
              rows={2}
              value={form.proactive}
              placeholder="仅在选择人工话术时填写"
              onChange={(event) => onFormChange({ proactive: event.target.value })}
            />
          </Form.Item>
        ) : null}

        <details>
          <summary style={{ cursor: 'pointer', color: '#AAB4C0' }}>模板与发送预览</summary>
          <Space direction="vertical" style={{ width: '100%', marginTop: 8 }} size="small">
            <Input.Search
              aria-label="搜索模板"
              placeholder="搜索当前行为模板"
              value={templateSearch}
              onChange={(event) => setTemplateSearch(event.target.value)}
              allowClear
            />
            <Space wrap size={4}>
              {templates.map((template) => (
                <Button
                  key={template.text}
                  size="small"
                  type="dashed"
                  onClick={() => onFormChange({ description: template.text })}
                >
                  {template.text}
                </Button>
              ))}
              {!templates.length ? <Text type="secondary">当前行为暂无模板</Text> : null}
            </Space>
            <div>
              <Text type="secondary">发送预览</Text>
              <Input.TextArea aria-label="发送预览" rows={3} readOnly value={preview} />
            </div>
          </Space>
        </details>

        <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
          <Button data-testid="draft-candidate" disabled={busy} onClick={() => onSubmit('pending')}>
            暂存为候选
          </Button>
          <Button
            data-testid="draft-submit"
            type="primary"
            loading={busy}
            onClick={() => onSubmit('confirmed')}
          >
            确认并发送给球球
          </Button>
        </Space>
      </Space>
    </Card>
  );
}
