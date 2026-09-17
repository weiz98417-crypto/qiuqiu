import { useCallback, useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Drawer, Empty, Input, Row, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { consoleApi } from '../api/client';
import type { TraceRow } from '../api/client';
import { fmtDateTime, fmtTime, reasonCodeLabel } from '../api/format';
import { useAsync } from '../api/useAsync';
import { ReactRouterLink } from '../components/ReactRouterLink';

const { Text } = Typography;

type TraceLike = TraceRow;

function traceReasonCodes(trace: TraceLike): string[] {
  if (trace.reasonCodes?.length) return trace.reasonCodes;
  return trace.relationshipDecision?.reasonCodes ?? [];
}

function matchesCitation(trace: TraceLike, prefix: string): boolean {
  if (!prefix) return true;
  const haystack = [trace.reason ?? '', ...traceReasonCodes(trace)];
  return haystack.some((code) => code.startsWith(prefix));
}

export default function CitationAudit() {
  // 跨比赛最近主动引用（来自概览聚合，cross-match limit 10）。
  const overview = useAsync(() => consoleApi.overview(), []);

  // 按引用前缀深查某场比赛的轨迹（backend citation= 过滤）。
  const [matchIdInput, setMatchIdInput] = useState('');
  const [appliedMatchId, setAppliedMatchId] = useState('');
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
        matchId ? <ReactRouterLink to={`/console/match/${matchId}`}>{matchId}</ReactRouterLink> : '—',
    },
    { title: '用户', dataIndex: 'userId', key: 'userId', width: 130 },
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
    <Row gutter={[16, 16]}>
      <Col span={24}>
        <Card title="跨比赛最近主动引用">
          {overview.error ? <Alert type="error" showIcon message={overview.error} style={{ marginBottom: 12 }} /> : null}
          <Table
            size="small"
            rowKey="traceId"
            columns={[
              { title: 'Trace', dataIndex: 'traceId', render: (id: string) => <code style={{ fontSize: 12 }}>{id}</code> },
              {
                title: '比赛',
                dataIndex: 'matchId',
                render: (matchId: string) => (
                  <ReactRouterLink to={`/console/match/${matchId}`}>{matchId}</ReactRouterLink>
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
            loading={overview.loading}
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
          {traceQuery.error ? <Alert type="error" showIcon message={traceQuery.error} /> : null}
          {!canQuery ? (
            <Empty description="输入比赛 ID 与引用前缀开始审计" imageStyle={{ height: 48 }} />
          ) : (
            <Table<TraceLike>
              size="small"
              rowKey="id"
              columns={traceColumns}
              dataSource={traceRows}
              loading={traceQuery.loading}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              locale={{ emptyText: <Empty description="没有匹配该引用前缀的回合" imageStyle={{ height: 48 }} /> }}
            />
          )}
        </Card>
      </Col>

      <Drawer
        title="为什么说话"
        open={Boolean(drawerTrace)}
        onClose={() => setDrawerTrace(null)}
        width={480}
      >
        {drawerTrace ? (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <div>
              <Text type="secondary">原因码</Text>
              <div>
                <Space size={4} wrap>
                  {traceReasonCodes(drawerTrace).map((code) => (
                    <Tag key={code} color="orange">
                      {reasonCodeLabel(code)}
                    </Tag>
                  ))}
                  {!traceReasonCodes(drawerTrace).length ? (
                    <Text type="secondary">{drawerTrace.reason || '—'}</Text>
                  ) : null}
                </Space>
              </div>
            </div>
            <div>
              <Text type="secondary">用户输入</Text>
              <div>{drawerTrace.input || '（主动回合，无用户输入）'}</div>
            </div>
            <div>
              <Text type="secondary">球球输出</Text>
              <div>{drawerTrace.output || '—'}</div>
            </div>
          </Space>
        ) : null}
      </Drawer>
    </Row>
  );
}
