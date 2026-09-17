import { useState } from 'react';
import { Alert, Button, Card, Col, Drawer, Empty, Input, Row, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useParams } from 'react-router-dom';
import { consoleApi } from '../api/client';
import type { ConsoleUser, MatchEventRow, TraceRow } from '../api/client';
import { fmtDateTime, fmtTime, reasonCodeLabel, talkativenessLabel } from '../api/format';
import { useAsync } from '../api/useAsync';
import { ReactRouterLink } from '../components/ReactRouterLink';
import MatchSettings from '../components/MatchSettings';

const { Text } = Typography;

type TraceLike = TraceRow;

function traceReasonCodes(trace: TraceLike): string[] {
  if (trace.reasonCodes?.length) return trace.reasonCodes;
  return trace.relationshipDecision?.reasonCodes ?? [];
}

// citation 前缀在 reasonCodes 上做前缀匹配（与后端 citation= 过滤一致）。
function matchesCitation(trace: TraceLike, prefix: string): boolean {
  if (!prefix) return true;
  const haystack = [trace.reason ?? '', ...traceReasonCodes(trace)];
  return haystack.some((code) => code.startsWith(prefix));
}

export default function MatchPage() {
  const { matchId = '' } = useParams<{ matchId: string }>();
  const [citationFilter, setCitationFilter] = useState('proactive_citation:');
  const [appliedCitation, setAppliedCitation] = useState('proactive_citation:');
  const [drawerTrace, setDrawerTrace] = useState<TraceLike | null>(null);

  // 比赛层三路数据：事件流、审计轨迹、用户网格。
  const events = useAsync<{ events: MatchEventRow[] }>(
    () => consoleApi.matchEvents(matchId),
    [matchId],
  );
  const users = useAsync<{ users: ConsoleUser[] }>(() => consoleApi.matchUsers(matchId), [matchId]);
  const traces = useAsync<{ traces: TraceLike[] }>(
    () => consoleApi.traces(matchId, appliedCitation, 100),
    [matchId, appliedCitation],
  );

  const traceRows = (traces.data?.traces ?? []).filter((trace) => matchesCitation(trace, appliedCitation));

  const userColumns: ColumnsType<ConsoleUser> = [
    {
      title: '用户',
      dataIndex: 'userId',
      key: 'userId',
      render: (userId: string) => (
        <ReactRouterLink to={`/console/match/${matchId}/user/${userId}`}>
          <code>{userId}</code>
        </ReactRouterLink>
      ),
    },
    {
      title: '在线',
      dataIndex: 'online',
      key: 'online',
      width: 80,
      render: (online: boolean) =>
        online ? <Tag color="blue">在线</Tag> : <Tag>离线</Tag>,
    },
    {
      title: '话痨档位',
      dataIndex: 'talkativeness',
      key: 'talkativeness',
      width: 110,
      render: (tier: string) => talkativenessLabel(tier),
    },
    { title: '开放话题', dataIndex: 'openThreads', key: 'openThreads', width: 96, align: 'right' },
    {
      title: '画像更新',
      dataIndex: 'portraitUpdatedAt',
      key: 'portraitUpdatedAt',
      width: 130,
      render: (value: string | null) => fmtTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      render: (_, record) => (
        <ReactRouterLink to={`/console/match/${matchId}/user/${record.userId}`}>查看用户</ReactRouterLink>
      ),
    },
  ];

  const traceColumns: ColumnsType<TraceLike> = [
    { title: 'Trace', dataIndex: 'id', key: 'id', render: (id: string) => <code style={{ fontSize: 12 }}>{id}</code> },
    { title: '用户', dataIndex: 'userId', key: 'userId', width: 140 },
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

  return (
    <div>
      <Row gutter={[16, 16]}>
        <Col span={24}>
          <Card
            title={`比赛 · ${matchId}`}
            extra={
              <Button size="small" onClick={() => { void events.reload(); void users.reload(); void traces.reload(); }}>
                刷新
              </Button>
            }
          >
            <Space direction="vertical" style={{ width: '100%' }} size="middle">
              <div>
                <Text type="secondary" style={{ marginRight: 8 }}>
                  事件流
                </Text>
                {events.error ? <Alert type="error" showIcon message={events.error} /> : null}
                <Table<MatchEventRow>
                  size="small"
                  rowKey="id"
                  columns={[
                    { title: '时间', dataIndex: 'createdAt', width: 110, render: fmtTime },
                    { title: '时钟', dataIndex: 'clock', width: 90 },
                    {
                      title: '事件',
                      dataIndex: 'eventType',
                      width: 120,
                      render: (type: string) => <Tag color="orange">{type}</Tag>,
                    },
                    {
                      title: '说明',
                      key: 'description',
                      render: (_, record) =>
                        [record.teamName, record.playerName, record.description].filter(Boolean).join(' · ') || '—',
                    },
                  ]}
                  dataSource={events.data?.events ?? []}
                  loading={events.loading}
                  pagination={{ pageSize: 5, hideOnSinglePage: true }}
                  locale={{ emptyText: <Empty description="暂无比赛事件" imageStyle={{ height: 40 }} /> }}
                />
              </div>
            </Space>
          </Card>
        </Col>

        <Col span={12}>
          <Card title="引用审计（按引用前缀过滤）">
            <Space.Compact style={{ width: '100%', marginBottom: 12 }}>
              <Input
                aria-label="引用前缀"
                value={citationFilter}
                onChange={(event) => setCitationFilter(event.target.value)}
                placeholder="proactive_citation:"
                onPressEnter={() => setAppliedCitation(citationFilter)}
              />
              <Button type="primary" onClick={() => setAppliedCitation(citationFilter)}>
                过滤
              </Button>
            </Space.Compact>
            {traces.error ? <Alert type="error" showIcon message={traces.error} /> : null}
            <Table<TraceLike>
              size="small"
              rowKey="id"
              columns={traceColumns}
              dataSource={traceRows}
              loading={traces.loading}
              pagination={{ pageSize: 8, hideOnSinglePage: true }}
              locale={{
                emptyText: <Empty description="没有匹配该引用前缀的回合" imageStyle={{ height: 40 }} />,
              }}
            />
          </Card>
        </Col>

        <Col span={12}>
          <Card title="用户网格">
            {users.error ? <Alert type="error" showIcon message={users.error} /> : null}
            <Table<ConsoleUser>
              size="small"
              rowKey="userId"
              columns={userColumns}
              dataSource={users.data?.users ?? []}
              loading={users.loading}
              pagination={false}
              locale={{ emptyText: <Empty description="暂无在线用户" imageStyle={{ height: 40 }} /> }}
            />
          </Card>
        </Col>

        {/* 设置：自动化播报策略 + 数据源与人工接管（legacy #automation/#sources 迁移）。 */}
        <Col span={24}>
          <MatchSettings matchId={matchId} />
        </Col>
      </Row>

      <Drawer
        title="为什么说话"
        open={Boolean(drawerTrace)}
        onClose={() => setDrawerTrace(null)}
        width={480}
      >
        {drawerTrace ? (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <div>
              <Text type="secondary">Trace</Text>
              <div>
                <code>{drawerTrace.id}</code>
              </div>
            </div>
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
            <div>
              <Text type="secondary">时间</Text>
              <div>{fmtDateTime(drawerTrace.createdAt)}</div>
            </div>
          </Space>
        ) : null}
      </Drawer>
    </div>
  );
}
