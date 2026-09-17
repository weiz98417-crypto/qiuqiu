import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Card,
  Checkbox,
  Col,
  Form,
  InputNumber,
  Popconfirm,
  Radio,
  Row,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { consoleApi } from '../api/client';
import type { AutomationPolicy, SourceStatus } from '../api/client';
import { useOperator } from '../api/operator';
import { useAsync } from '../api/useAsync';

const { Text } = Typography;

// 自动化事件范围（与 operator.html automationEventOptions、
// matchstate.DefaultAutomationPolicy 同一清单）。
const AUTOMATION_EVENT_OPTIONS: [string, string][] = [
  ['kickoff', '开球'], ['goal', '进球'], ['shot', '射门'], ['big_chance', '绝佳机会'],
  ['save', '扑救'], ['miss', '错失'], ['foul', '犯规'], ['yellow_card', '黄牌'],
  ['red_card', '红牌'], ['var_check', 'VAR检查'], ['var_result', 'VAR结果'],
  ['goal_cancelled', '进球取消'], ['penalty', '点球'], ['penalty_awarded', '点球判定'],
  ['substitution', '换人'], ['injury', '伤停'], ['tactical_shift', '战术变化'],
  ['pressure', '持续压迫'], ['halftime', '中场'], ['fulltime', '完场'], ['match_end', '比赛结束'],
];

const ACTIVE_SOURCE_LABELS: Record<string, { label: string; color: string }> = {
  'api-sports': { label: '实时数据', color: 'green' },
  replay: { label: '回放数据', color: 'blue' },
  manual: { label: '人工导演', color: 'orange' },
};

const STATE_LABELS: Record<string, { label: string; color: string }> = {
  ready: { label: '就绪', color: 'default' },
  standby: { label: '待命', color: 'default' },
  running: { label: '运行中', color: 'processing' },
  stopped: { label: '已停止', color: 'default' },
  error: { label: '错误', color: 'error' },
  unconfigured: { label: '未配置', color: 'default' },
};

const FRESHNESS_LABELS: Record<string, string> = {
  fresh: '新鲜',
  degraded: '滞后',
  unknown: '未知',
  offline: '离线',
};

const SOURCE_TYPE_LABELS: Record<string, string> = {
  'api-sports': '实时数据源',
  replay: '回放数据源',
  manual: '人工导演',
};

export default function MatchSettings({ matchId }: { matchId: string }) {
  const { message: messageApi } = AntApp.useApp();
  const { loading: operatorLoading, isDirector } = useOperator();
  const [form] = Form.useForm<{ mode: 'active' | 'paused'; eventTypes: string[]; cooldownSeconds: number }>();
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [acting, setActing] = useState('');

  const automation = useAsync(() => consoleApi.getAutomation(matchId), [matchId]);
  const sources = useAsync(() => consoleApi.getSources(matchId), [matchId]);

  // 表单初值跟随后端策略（首次加载 / 接管等外部变更后）。
  useEffect(() => {
    if (automation.data?.policy) {
      form.setFieldsValue({
        mode: automation.data.policy.mode,
        eventTypes: automation.data.policy.eventTypes,
        cooldownSeconds: automation.data.policy.cooldownSeconds,
      });
    }
  }, [automation.data, form]);

  const savePolicy = useCallback(
    async (values: { mode: 'active' | 'paused'; eventTypes: string[]; cooldownSeconds: number }) => {
      setSavingPolicy(true);
      try {
        const policy: AutomationPolicy = {
          mode: values.mode,
          eventTypes: values.eventTypes ?? [],
          cooldownSeconds: values.cooldownSeconds ?? 0,
        };
        const result = await consoleApi.setAutomation(matchId, policy);
        messageApi.success(`自动化策略已保存（${result.policy.mode === 'active' ? '自动播报' : '暂停'}）`);
        await Promise.all([automation.reload(), sources.reload()]);
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      } finally {
        setSavingPolicy(false);
      }
    },
    [automation, matchId, messageApi, sources],
  );

  const runSourceAction = useCallback(
    async (action: 'start-replay' | 'takeover' | 'stop') => {
      setActing(action);
      try {
        if (action === 'start-replay') {
          await consoleApi.startSource(matchId, { type: 'replay', expectedDelay: 'normal' });
          messageApi.success('回放数据源已启动');
        } else if (action === 'takeover') {
          await consoleApi.takeover(matchId);
          messageApi.success('已切换人工导演源，自动播报暂停');
        } else {
          await consoleApi.stopSources(matchId);
          messageApi.success('外部数据源已停止');
        }
        await Promise.all([sources.reload(), automation.reload()]);
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      } finally {
        setActing('');
      }
    },
    [form, matchId, messageApi, sources, automation],
  );

  const status = sources.data?.status;
  const sourceRows: SourceStatus[] = status
    ? ['api-sports', 'replay', 'manual']
        .map((type) => status.sources[type])
        .filter((row): row is SourceStatus => Boolean(row))
    : [];

  const sourceColumns: ColumnsType<SourceStatus> = [
    {
      title: '数据源',
      dataIndex: 'type',
      key: 'type',
      width: 130,
      render: (type: string) => SOURCE_TYPE_LABELS[type] ?? type,
    },
    {
      title: '状态',
      dataIndex: 'state',
      key: 'state',
      width: 100,
      render: (state: string) => {
        const meta = STATE_LABELS[state] ?? { label: state, color: 'default' };
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: '新鲜度',
      dataIndex: 'freshness',
      key: 'freshness',
      width: 90,
      render: (freshness: string) => FRESHNESS_LABELS[freshness] ?? freshness,
    },
    {
      title: 'P95 延迟',
      dataIndex: 'latencyP95Ms',
      key: 'latencyP95Ms',
      width: 100,
      align: 'right',
      render: (value?: number) => (value ? `${value} ms` : '—'),
    },
    {
      title: '备注',
      key: 'note',
      ellipsis: true,
      render: (_, record) => record.error || (record.expectedDelay ? `预期延迟：${record.expectedDelay}` : '—'),
    },
  ];

  const activeBadge = status ? ACTIVE_SOURCE_LABELS[status.activeSource] ?? { label: status.activeSource, color: 'default' } : null;

  return (
    <Card
      title="设置"
      extra={
        <Space>
          {status ? (
            <span>
              <Text type="secondary">当前源</Text>{' '}
              <Badge
                color={activeBadge?.color === 'processing' ? 'blue' : activeBadge?.color}
                text={<strong>{activeBadge?.label ?? status.activeSource}</strong>}
              />
            </span>
          ) : null}
          {!operatorLoading && !isDirector ? <Tag color="blue">审计只读</Tag> : null}
        </Space>
      }
    >
      <Row gutter={[24, 24]}>
        <Col span={12}>
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            <Text strong>自动化播报策略</Text>
            {automation.error ? <Alert type="error" showIcon message={automation.error} /> : null}
            <Form
              form={form}
              layout="vertical"
              disabled={!isDirector}
              onFinish={(values) => void savePolicy(values)}
              initialValues={{ mode: 'active', eventTypes: [], cooldownSeconds: 90 }}
            >
              <Space size="large" wrap>
                <Form.Item name="mode" label="模式" style={{ marginBottom: 8 }}>
                  <Radio.Group
                    options={[
                      { value: 'active', label: '自动播报' },
                      { value: 'paused', label: '暂停' },
                    ]}
                    optionType="button"
                    buttonStyle="solid"
                  />
                </Form.Item>
                <Form.Item
                  name="cooldownSeconds"
                  label="冷却秒数（0-300）"
                  style={{ marginBottom: 8 }}
                  rules={[{ required: true, message: '请输入冷却秒数' }]}
                >
                  <InputNumber min={0} max={300} style={{ width: 120 }} />
                </Form.Item>
              </Space>
              <Form.Item name="eventTypes" label="事件范围" style={{ marginBottom: 8 }}>
                <Checkbox.Group
                  options={AUTOMATION_EVENT_OPTIONS.map(([value, label]) => ({ value, label }))}
                  style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 16px' }}
                />
              </Form.Item>
              {isDirector ? (
                <Button type="primary" htmlType="submit" loading={savingPolicy}>
                  保存策略
                </Button>
              ) : (
                <Text type="secondary">话痨与播报策略对审计角色只读。</Text>
              )}
            </Form>
          </Space>
        </Col>
        <Col span={12}>
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            <Space size="middle" wrap>
              <Text strong>数据源与人工接管</Text>
              {isDirector ? (
                <>
                  <Button
                    size="small"
                    disabled={acting !== ''}
                    loading={acting === 'start-replay'}
                    onClick={() => void runSourceAction('start-replay')}
                  >
                    启动回放数据源
                  </Button>
                  <Popconfirm
                    title="人工接管"
                    description="停用外部数据源、自动播报转入暂停，由导演亲自发声。"
                    okText="接管"
                    cancelText="取消"
                    onConfirm={() => void runSourceAction('takeover')}
                  >
                    <Button size="small" type="primary" danger loading={acting === 'takeover'}>
                      人工接管
                    </Button>
                  </Popconfirm>
                  <Button size="small" disabled={acting !== ''} loading={acting === 'stop'} onClick={() => void runSourceAction('stop')}>
                    停止外部数据源
                  </Button>
                </>
              ) : null}
            </Space>
            {sources.error ? <Alert type="error" showIcon message={sources.error} /> : null}
            <Table<SourceStatus>
              size="small"
              rowKey="type"
              columns={sourceColumns}
              dataSource={sourceRows}
              loading={sources.loading || operatorLoading}
              pagination={false}
              locale={{ emptyText: '数据源状态不可用' }}
            />
          </Space>
        </Col>
      </Row>
    </Card>
  );
}
