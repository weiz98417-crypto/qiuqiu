import { useCallback, useMemo, useState } from 'react';
import { Button, Card, Col, Empty, Input, Row, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link, useSearchParams } from 'react-router-dom';
import { consoleApi } from '../api/client';
import type { TraceRow } from '../api/client';
import { fmtDateTime, fmtTime, reasonCodeLabel } from '../api/format';
import { matchesCitation, ragCallCount, traceReasonCodes, WhyDrawer } from '../api/traceEvidence';
import { useAsync } from '../api/useAsync';
import ConsolePageShell from '../components/ConsolePageShell';

const { Text } = Typography;

type TraceLike = TraceRow;

export default function CitationAudit() {
  // 跨比赛最近主动引用（来自概览聚合，cross-match limit 10）。
  const overview = useAsync(() => consoleApi.overview(), []);

  // URL 预填：比赛页「去引用审计」入口带 matchId 进来。
  const [searchParams] = useSearchParams();
  const urlMatchId = searchParams.get('matchId') ?? '';

  // 按引用前缀深查某场比赛的轨迹（backend citation= 过滤）。
  const [matchIdInput, setMatchIdInput] = useState(urlMatchId);
  const [appliedMatchId, setAppliedMatchId] = useState(urlMatchId);
  const [citationInput, setCitationInput] = useState('proactive_citation:');
  const [appliedCitation, setAppliedCitation] = useState('proactive_citation:');
  const [drawerTrace, setDrawerTrace] = useState<TraceLike | null>(null);

  const canQuery = Boolean(appliedMatchId.trim());
  // 未输入比赛 ID 时不发请求（避免 /api/matches//traces 这类无效调用）。
  const traceQuery = useAsync<{ traces: TraceRow[] }>(
    () =>
      canQuery
        ? consoleApi.traces(appliedMatchId.trim(), appliedCitation, 100)
        : Promise.resolve({ traces: [] }),
    [appliedMatchId, appliedCitation, canQuery],
  );
  // 后端 citation= 过滤未落地时前端兜底过滤，保证口径一致。
  const traceRows = useMemo(
    () => (canQuery ? (traceQuery.data?.traces ?? []).filter((t) => matchesCitation(t, appliedCitation)) : []),
    [canQuery, traceQuery.data, appliedCitation],
  );

  const proactiveRows = overview.data?.recentProactive ?? [];

  const traceColumns: ColumnsType<TraceLike> = [
    {
      title: 'Trace',
      dataIndex: 'id',
      key: 'id',
      width: 220,
      render: (id: string) => <code style={{ fontSize: 12 }}>{id}</code>,
    },
    {
      title: '比赛',
      dataIndex: 'matchId',
      key: 'matchId',
      width: 150,
      render: (matchId: string) =>
        matchId ? <Link to={`/console/match/${matchId}`}>{matchId}</Link> : '—',
    },
    { title: '用户', dataIndex: 'userId', key: 'userId', width: 130 },
    {
      // intent-router 4.2：被路由回合的原始路由判定（意图 + 置信度）。
      title: '路由意图',
      key: 'routerIntent',
      width: 170,
      render: (_, record) =>
        record.router ? (
          <Space size={4} wrap>
            <Tag color="geekblue">{record.router.intent}</Tag>
            <Text type="secondary">{(record.router.confidence * 100).toFixed(0)}%</Text>
            {record.router.replyUsed ? <Tag color="green">回复已采用</Tag> : null}
          </Space>
        ) : (
          <Text type="secondary">—</Text>
        ),
    },
    {
      // RAG 命中：toolCalls 里 knowledge/search 类调用的次数（0 = 未检索）。
      title: 'RAG',
      key: 'rag',
      width: 70,
      align: 'right',
      render: (_, record) => {
        const calls = ragCallCount(record);
        return calls > 0 ? <Tag color="purple">{calls}</Tag> : <Text type="secondary">—</Text>;
      },
    },
    {
      title: '原因码',
      key: 'reasonCodes',
      render: (_, record) => (
        <Space size={4} wrap>
          {traceReasonCodes(record).length ? (
            traceReasonCodes(record).map((code) => (
              <Tag key={code} color={code.startsWith('proactive_citation:') ? 'orange' : 'default'}>
                {reasonCodeLabel(code)}
              </Tag>
            ))
          ) : (
            <Text type="secondary">{record.reason || '—'}</Text>
          )}
        </Space>
      ),
    },
    { title: '时间', dataIndex: 'createdAt', key: 'createdAt', width: 110, render: fmtTime },
    {
      title: '操作',
      key: 'why',
      width: 110,
      render: (_, record) => (
        <Button type="link" size="small" onClick={() => setDrawerTrace(record)}>
          为什么说话
        </Button>
      ),
    },
  ];

  const queryTraces = useCallback(() => {
    setAppliedMatchId(matchIdInput.trim());
    setAppliedCitation(citationInput.trim());
  }, [citationInput, matchIdInput]);

  return (
    <ConsolePageShell
      title="引用审计"
      error={overview.error || traceQuery.error}
      loading={overview.loading || traceQuery.loading}
    >
      <Row gutter={[16, 16]}>
        <Col span={24}>
          <Card title="跨比赛最近主动引用">
            <Table
              size="small"
              rowKey="traceId"
              columns={[
                { title: 'Trace', dataIndex: 'traceId', render: (id: string) => <code style={{ fontSize: 12 }}>{id}</code> },
                {
                  title: '比赛',
                  dataIndex: 'matchId',
                  render: (matchId: string) => (
                    <Link to={`/console/match/${matchId}`}>{matchId}</Link>
                  ),
                },
                {
                  title: '引用',
                  dataIndex: 'citation',
                  render: (citation: string) => <Tag color="orange">{reasonCodeLabel(citation)}</Tag>,
                },
                { title: '时间', dataIndex: 'createdAt', render: fmtDateTime },
              ]}
              dataSource={proactiveRows}
              pagination={false}
              locale={{ emptyText: <Empty description="暂无主动引用记录" imageStyle={{ height: 48 }} /> }}
            />
          </Card>
        </Col>

        <Col span={24}>
          <Card title="按引用前缀审计轨迹">
            <Space.Compact style={{ width: '100%', marginBottom: 12 }}>
              <Input
                aria-label="比赛 ID"
                placeholder="比赛 ID（如 demo-operator-control-e2e）"
                style={{ width: 320 }}
                value={matchIdInput}
                onChange={(event) => setMatchIdInput(event.target.value)}
                onPressEnter={queryTraces}
              />
              <Input
                aria-label="引用前缀"
                placeholder="proactive_citation:"
                style={{ width: 260 }}
                value={citationInput}
                onChange={(event) => setCitationInput(event.target.value)}
                onPressEnter={queryTraces}
              />
              <Button type="primary" onClick={queryTraces}>
                审计
              </Button>
            </Space.Compact>
            {!canQuery ? (
              <Empty description="输入比赛 ID 与引用前缀开始审计" imageStyle={{ height: 48 }} />
            ) : (
              <Table<TraceLike>
                size="small"
                rowKey="id"
                columns={traceColumns}
                dataSource={traceRows}
                pagination={{ pageSize: 10, hideOnSinglePage: true }}
                locale={{ emptyText: <Empty description="没有匹配该引用前缀的回合" imageStyle={{ height: 48 }} /> }}
              />
            )}
          </Card>
        </Col>

        <WhyDrawer trace={drawerTrace} onClose={() => setDrawerTrace(null)} />
      </Row>
    </ConsolePageShell>
  );
}
