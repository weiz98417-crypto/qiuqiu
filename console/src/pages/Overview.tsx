import { useMemo } from 'react';
import { Alert, Badge, Button, Card, Col, Empty, List, Row, Statistic, Table, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link } from 'react-router-dom';
import { consoleApi } from '../api/client';
import type { AuditRow, ConsoleMatch, Overview as OverviewData } from '../api/client';
import { fmtDateTime, fmtTime } from '../api/format';
import { useAsync } from '../api/useAsync';

const MATCH_STATE_LABELS: Record<string, { label: string; color: string }> = {
  live: { label: '直播中', color: 'processing' },
  pre_match: { label: '赛前', color: 'default' },
  halftime: { label: '中场', color: 'default' },
  finished: { label: '已结束', color: 'default' },
};

const matchColumns: ColumnsType<ConsoleMatch> = [
  {
    title: '比赛',
    dataIndex: 'matchId',
    key: 'matchId',
    render: (matchId: string) => (
      <Link to={`/console/match/${matchId}`}>
        <code>{matchId}</code>
      </Link>
    ),
  },
  {
    title: '状态',
    dataIndex: 'state',
    key: 'state',
    width: 96,
    render: (state: string) => {
      const meta = MATCH_STATE_LABELS[state] ?? { label: state, color: 'default' };
      return <Tag color={meta.color}>{meta.label}</Tag>;
    },
  },
  { title: '在线用户', dataIndex: 'onlineUsers', key: 'onlineUsers', width: 96, align: 'right' },
];

const auditColumns: ColumnsType<AuditRow> = [
  { title: '运营员', dataIndex: 'operatorName', key: 'operatorName', width: 90 },
  { title: '动作', dataIndex: 'action', key: 'action', width: 120 },
  { title: '对象', dataIndex: 'object', key: 'object', ellipsis: true },
  { title: '时间', dataIndex: 'createdAt', key: 'createdAt', width: 110, render: fmtTime },
];

function cellBorder(style: React.CSSProperties): React.CSSProperties {
  return { border: '1px solid #253142', ...style };
}

export default function Overview() {
  const { data, loading, error, reload } = useAsync<OverviewData>(() => consoleApi.overview(), []);

  const aging = data?.threadAging ?? { today: 0, d1to3: 0, d3plus: 0 };
  const memory = data?.memory;
  const matchRows = useMemo(() => data?.matches ?? [], [data]);

  return (
    <div>
      {error ? (
        <Alert type="error" showIcon message="概览加载失败" description={error} style={{ marginBottom: 16 }} />
      ) : null}
      <Row gutter={[16, 16]}>
        {/* 一格 · 活跃比赛 */}
        <Col span={12}>
          <Card
            data-cell="matches"
            title="活跃比赛"
            extra={
              <Button size="small" onClick={reload} loading={loading}>
                刷新
              </Button>
            }
            style={cellBorder({ height: '100%' })}
          >
            <Table<ConsoleMatch>
              size="small"
              rowKey="matchId"
              columns={matchColumns}
              dataSource={matchRows}
              loading={loading}
              pagination={false}
              locale={{ emptyText: <Empty description="暂无活跃比赛" imageStyle={{ height: 48 }} /> }}
            />
          </Card>
        </Col>

        {/* 二格 · 在线会话 + 三格 · 话题老化 */}
        <Col span={12}>
          <Row gutter={[16, 16]}>
            <Col span={12}>
              <Card data-cell="sessions" title="在线会话" style={cellBorder({ height: '100%' })}>
                <Statistic value={data?.onlineSessions ?? 0} suffix="个会话在线" loading={loading} />
                <div style={{ marginTop: 8 }}>
                  <Link to="/console/threads">查看话题台账 →</Link>
                </div>
              </Card>
            </Col>
            <Col span={12}>
              <Card data-cell="aging" title="话题老化" style={cellBorder({ height: '100%' })}>
                <Row gutter={8}>
                  <Col span={8}>
                    <Statistic title="今日" value={aging.today} valueStyle={{ color: '#5FCB8B' }} />
                  </Col>
                  <Col span={8}>
                    <Statistic title="1-3 天" value={aging.d1to3} valueStyle={{ color: '#F2C94C' }} />
                  </Col>
                  <Col span={8}>
                    <Statistic title="3 天以上" value={aging.d3plus} valueStyle={{ color: '#F05D5E' }} />
                  </Col>
                </Row>
              </Card>
            </Col>
            <Col span={24}>
              {/* 四格 · 记忆健康 */}
              <Card
                data-cell="memory"
                title="记忆健康"
                style={cellBorder({})}
                extra={
                  memory ? (
                    <Badge
                      status={memory.degraded ? 'error' : 'success'}
                      text={memory.degraded ? '降级' : '健康'}
                    />
                  ) : null
                }
              >
                <Row gutter={16} align="middle">
                  <Col span={6}>
                    <Statistic title="积压深度" value={memory?.backlogDepth ?? 0} loading={loading} />
                  </Col>
                  <Col span={18}>
                    <div style={{ marginBottom: 4, color: '#AAB4C0', fontSize: 12 }}>最近审计</div>
                    <Table<AuditRow>
                      size="small"
                      rowKey={(row) => `${row.createdAt}-${row.operatorName}-${row.action}`}
                      columns={auditColumns}
                      dataSource={memory?.recentAudit ?? []}
                      loading={loading}
                      pagination={false}
                      scroll={{ y: 160 }}
                      locale={{ emptyText: <Empty description="暂无审计记录" imageStyle={{ height: 40 }} /> }}
                    />
                  </Col>
                </Row>
              </Card>
            </Col>
          </Row>
        </Col>

        {/* 五格 · 最近主动引用 + 六格 · 词汇漏斗 */}
        <Col span={12}>
          <Card
            data-cell="proactive"
            title="最近主动引用"
            extra={<Link to="/console/citations">进入引用审计 →</Link>}
            style={cellBorder({ height: '100%' })}
          >
            <List
              size="small"
              loading={loading}
              dataSource={data?.recentProactive ?? []}
              locale={{ emptyText: <Empty description="暂无主动引用记录" imageStyle={{ height: 48 }} /> }}
              renderItem={(item) => (
                <List.Item
                  actions={[
                    <Link key="match" to={`/console/match/${item.matchId}`}>
                      比赛 {item.matchId}
                    </Link>,
                  ]}
                >
                  <List.Item.Meta
                    title={<code style={{ fontSize: 12 }}>{item.traceId}</code>}
                    description={
                      <span>
                        <Tag color="blue">{item.citation}</Tag>
                        <span style={{ color: '#AAB4C0' }}>{fmtDateTime(item.createdAt)}</span>
                      </span>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col span={12}>
          {/* intent-router C3 词汇漏斗：没接明白率 + top unroutable 样本 */}
          <Card data-cell="router-funnel" title="词汇漏斗（没接明白）" style={cellBorder({ height: '100%' })}>
            <Row gutter={16}>
              <Col span={6}>
                <Statistic
                  title="没接明白率（24h）"
                  value={((data?.router?.unknownRate ?? 0) * 100).toFixed(1)}
                  suffix="%"
                  loading={loading}
                />
                <div style={{ color: '#AAB4C0', fontSize: 12 }}>
                  {data?.router?.unknownTurns ?? 0} / {data?.router?.totalTurns ?? 0} 回合
                </div>
              </Col>
              <Col span={18}>
                <div style={{ marginBottom: 4, color: '#AAB4C0', fontSize: 12 }}>最新没接明白样本</div>
                <Table
                  size="small"
                  rowKey={(row) => `${row.userId}-${row.createdAt}-${row.content}`}
                  columns={[
                    { title: '用户', dataIndex: 'userId', width: 120, ellipsis: true },
                    { title: '原话', dataIndex: 'content', ellipsis: true },
                    { title: '时间', dataIndex: 'createdAt', width: 110, render: fmtDateTime },
                  ]}
                  dataSource={data?.router?.topUnroutable ?? []}
                  loading={loading}
                  pagination={false}
                  scroll={{ y: 160 }}
                  locale={{ emptyText: <Empty description="暂无 unroutable 样本" imageStyle={{ height: 40 }} /> }}
                />
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
