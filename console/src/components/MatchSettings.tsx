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
  Input,
  InputNumber,
  Popconfirm,
  Radio,
  Row,
  Select,
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
import { automationEventOptions } from '../director/event-vocabulary';

const { Text } = Typography;

// 自动化事件范围（后端 matchstate 默认策略清单的控制台侧镜像，标签单源）。
const AUTOMATION_EVENT_OPTIONS = automationEventOptions;

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

// 阵容文本行格式（旧页 #setup 同款）：`号码 名字 位置 [首发|替补]`。
interface ParsedPlayer {
  number: string;
  name: string;
  position: string;
}

function parsePlayerLines(text: string): ParsedPlayer[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split(/\s+/);
      const number = /^\d{1,3}$/.test(parts[0]) ? parts[0] : '';
      const name = number ? parts[1] ?? '' : parts[0] ?? '';
      const position = number ? parts[2] ?? '' : parts[1] ?? '';
      return { number, name, position };
    })
    .filter((player) => player.name !== '');
}

export default function MatchSettings({ matchId }: { matchId: string }) {
  const { message: messageApi } = AntApp.useApp();
  const { loading: operatorLoading, isDirector } = useOperator();
  const [form] = Form.useForm<{ mode: 'active' | 'paused'; eventTypes: string[]; cooldownSeconds: number }>();
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [acting, setActing] = useState('');

  // —— 数据源启动表单（旧页 #sources 迁移：replay / api-sports + fixtureId / 预期延迟）——
  const [startType, setStartType] = useState<'replay' | 'api-sports'>('replay');
  const [fixtureId, setFixtureId] = useState<number | null>(null);
  const [expectedDelay, setExpectedDelay] = useState<string>('normal');

  // —— 赛前配置（旧页 #setup 迁移）——
  const config = useAsync(() => consoleApi.matchConfig(matchId), [matchId]);
  const [homeTeam, setHomeTeam] = useState('');
  const [awayTeam, setAwayTeam] = useState('');
  const [homePlayersText, setHomePlayersText] = useState('');
  const [awayPlayersText, setAwayPlayersText] = useState('');
  const [savingConfig, setSavingConfig] = useState(false);

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

  // 赛前配置预填（GET /config 响应是 { config, snapshot } 两层）。
  useEffect(() => {
    const data = config.data as { config?: Record<string, unknown> } | undefined;
    const raw = (data?.config ?? data) as Record<string, unknown> | undefined;
    if (!raw) return;
    setHomeTeam(String(raw.homeTeam ?? ''));
    setAwayTeam(String(raw.awayTeam ?? ''));
    const toText = (players: unknown) =>
      (Array.isArray(players) ? players : [])
        .map((p) => {
          const row = p as { number?: string; name?: string; position?: string };
          return [row.number, row.name, row.position].filter(Boolean).join(' ');
        })
        .filter(Boolean)
        .join('\n');
    setHomePlayersText(toText(raw.homePlayers));
    setAwayPlayersText(toText(raw.awayPlayers));
  }, [config.data]);

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
    async (action: 'start' | 'takeover' | 'stop') => {
      setActing(action);
      try {
        if (action === 'start') {
          if (startType === 'api-sports' && !fixtureId) {
            messageApi.warning('实时数据源需要比赛 ID（fixtureId）');
            return;
          }
          await consoleApi.startSource(matchId, {
            type: startType,
            ...(startType === 'api-sports' && fixtureId ? { fixtureId } : {}),
            expectedDelay: expectedDelay || undefined,
          });
          messageApi.success(`${SOURCE_TYPE_LABELS[startType]}已启动`);
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
    [fixtureId, expectedDelay, startType, matchId, messageApi, sources, automation],
  );

  const saveLineup = useCallback(async () => {
    setSavingConfig(true);
    try {
      await consoleApi.saveMatchConfig(matchId, {
        homeTeam: homeTeam.trim(),
        awayTeam: awayTeam.trim(),
        homePlayers: parsePlayerLines(homePlayersText),
        awayPlayers: parsePlayerLines(awayPlayersText),
      });
      messageApi.success('阵容已保存');
      await Promise.all([config.reload(), sources.reload()]);
    } catch (err) {
      messageApi.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingConfig(false);
    }
  }, [awayPlayersText, awayTeam, config, homePlayersText, homeTeam, matchId, messageApi, sources]);

  const lifecycleAction = useCallback(
    async (action: 'start' | 'reset') => {
      setActing(`lifecycle-${action}`);
      try {
        if (action === 'start') {
          // /start 的冻结形状携带整份配置（重置+存盘+开赛一体）：
          // 用当前表单状态作为请求体，与保存阵容同一来源。
          await consoleApi.startMatch(matchId, {
            homeTeam: homeTeam.trim(),
            awayTeam: awayTeam.trim(),
            homePlayers: parsePlayerLines(homePlayersText),
            awayPlayers: parsePlayerLines(awayPlayersText),
          });
          messageApi.success('比赛已开始');
        } else {
          await consoleApi.resetMatch(matchId);
          messageApi.success('比赛已重置');
        }
        await Promise.all([config.reload(), automation.reload(), sources.reload()]);
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      } finally {
        setActing('');
      }
    },
    [config, automation, sources, matchId, messageApi, homeTeam, awayTeam, homePlayersText, awayPlayersText],
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

  const rosterInputs = (value: string, onChange: (text: string) => void, label: string) => (
    <div style={{ flex: 1, minWidth: 220 }}>
      <Text type="secondary">{label}（每行：号码 名字 位置）</Text>
      <Input.TextArea
        aria-label={label}
        autoSize={{ minRows: 4, maxRows: 10 }}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={!isDirector}
        placeholder={'10 佩德里 CM\n8 法比安 CM'}
      />
    </div>
  );

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
        {/* 赛前配置（旧页 #setup 迁移）。 */}
        <Col span={24}>
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            <Space size="middle" wrap>
              <Text strong>赛前配置</Text>
              {isDirector ? (
                <>
                  <Button size="small" loading={savingConfig} disabled={acting !== ''} onClick={() => void saveLineup()}>
                    保存阵容
                  </Button>
                  <Button size="small" disabled={acting !== ''} loading={acting === 'lifecycle-start'} onClick={() => void lifecycleAction('start')}>
                    开始比赛
                  </Button>
                  <Popconfirm
                    title="重置比赛"
                    description="清空事件、时钟与比分，回到赛前。不可撤销。"
                    okText="重置"
                    cancelText="取消"
                    onConfirm={() => void lifecycleAction('reset')}
                  >
                    <Button size="small" danger disabled={acting !== ''} loading={acting === 'lifecycle-reset'}>
                      重置比赛
                    </Button>
                  </Popconfirm>
                </>
              ) : null}
              {config.error ? <Alert type="error" showIcon message={config.error} /> : null}
            </Space>
            <Space size="small" wrap>
              <Input
                aria-label="主队名"
                style={{ width: 160 }}
                value={homeTeam}
                onChange={(event) => setHomeTeam(event.target.value)}
                disabled={!isDirector}
                placeholder="主队名"
              />
              <Input
                aria-label="客队名"
                style={{ width: 160 }}
                value={awayTeam}
                onChange={(event) => setAwayTeam(event.target.value)}
                disabled={!isDirector}
                placeholder="客队名"
              />
            </Space>
            <Space size="small" wrap style={{ width: '100%' }}>
              {rosterInputs(homePlayersText, setHomePlayersText, '主队球员')}
              {rosterInputs(awayPlayersText, setAwayPlayersText, '客队球员')}
            </Space>
          </Space>
        </Col>

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
            </Space>
            {isDirector ? (
              <Space size="small" wrap>
                {/* 旧页 #sources 的启动面：replay / api-sports + fixtureId + 预期延迟。 */}
                <Select<'replay' | 'api-sports'>
                  aria-label="数据源类型"
                  style={{ width: 150 }}
                  value={startType}
                  onChange={setStartType}
                  disabled={acting !== ''}
                  options={[
                    { value: 'replay', label: '回放数据源' },
                    { value: 'api-sports', label: '实时数据源' },
                  ]}
                />
                {startType === 'api-sports' ? (
                  <InputNumber
                    aria-label="比赛 ID（fixtureId）"
                    placeholder="Fixture ID"
                    style={{ width: 130 }}
                    min={1}
                    value={fixtureId ?? undefined}
                    onChange={(value) => setFixtureId(value ?? null)}
                    disabled={acting !== ''}
                  />
                ) : null}
                <Input
                  aria-label="预期延迟"
                  placeholder="预期延迟（如 normal）"
                  style={{ width: 150 }}
                  value={expectedDelay}
                  onChange={(event) => setExpectedDelay(event.target.value)}
                  disabled={acting !== ''}
                />
                <Button size="small" type="primary" disabled={acting !== ''} loading={acting === 'start'} onClick={() => void runSourceAction('start')}>
                  启动数据源
                </Button>
                <Popconfirm
                  title="人工接管"
                  description="停用外部数据源、自动播报转入暂停，由导演亲自发声。"
                  okText="接管"
                  cancelText="取消"
                  onConfirm={() => void runSourceAction('takeover')}
                >
                  <Button size="small" danger loading={acting === 'takeover'} disabled={acting !== ''}>
                    人工接管
                  </Button>
                </Popconfirm>
                <Button size="small" disabled={acting !== ''} loading={acting === 'stop'} onClick={() => void runSourceAction('stop')}>
                  停止外部数据源
                </Button>
              </Space>
            ) : null}
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
