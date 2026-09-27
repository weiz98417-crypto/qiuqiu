import { Button, Card, Col, Empty, Row, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link, useParams } from 'react-router-dom';
import { consoleApi } from '../api/client';
import type { ConsoleUser, DeliveryInterruption, DirectorEventRow } from '../api/client';
import { fmtTime, talkativenessLabel } from '../api/format';
import { useConsoleQuery } from '../api/useConsoleQuery';
import ObservationPage from '../components/ObservationPage';
import MatchSettings from '../components/MatchSettings';

const { Text } = Typography;

// 投递打断（interruptionRing.Recent）：跨比赛最近被抢话/打断的回合。
const interruptionColumns: ColumnsType<DeliveryInterruption> = [
  {
    title: 'Trace',
    dataIndex: 'traceId',
    key: 'traceId',
    render: (traceId: string) => <code style={{ fontSize: 12 }}>{traceId}</code>,
  },
  {
    title: '比赛',
    dataIndex: 'matchId',
    key: 'matchId',
    width: 150,
    render: (matchId: string) => <Link to={`/console/match/${matchId}`}>{matchId}</Link>,
  },
  { title: '时间', dataIndex: 'at', key: 'at', width: 110, render: fmtTime },
];

export default function MatchPage() {
  const { matchId = '' } = useParams<{ matchId: string }>();

  // 比赛层两路数据：事件流、用户网格 + 投递打断。轨迹深查收敛到引用审计页（#c8）。
  const events = useConsoleQuery<{ events: DirectorEventRow[] }>(
    () => consoleApi.matchEvents(matchId),
    [matchId],
  );
  const users = useConsoleQuery<{ users: ConsoleUser[] }>(() => consoleApi.matchUsers(matchId), [matchId]);
  const interruptions = useConsoleQuery(() => consoleApi.deliveryInterruptions(), []);

  const reloadAll = () => {
    void events.reload();
    void users.reload();
    void interruptions.reload();
  };

  const userColumns: ColumnsType<ConsoleUser> = [
    {
      title: '用户',
      dataIndex: 'userId',
      key: 'userId',
      render: (userId: string) => (
        <Link to={`/console/match/${matchId}/user/${userId}`}>
          <code>{userId}</code>
        </Link>
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
        <Link to={`/console/match/${matchId}/user/${record.userId}`}>查看用户</Link>
      ),
    },
  ];

  return (
    <ObservationPage
      title="比赛"
      subtitle={<code>{matchId}</code>}
      error={events.error || users.error || interruptions.error}
      loading={events.loading || users.loading}
      onReload={reloadAll}
    >
      <Row gutter={[16, 16]}>
        <Col span={24}>
          <Card title={`比赛 · ${matchId}`}>
            <Space direction="vertical" style={{ width: '100%' }} size="middle">
              <div>
                <Text type="secondary" style={{ marginRight: 8 }}>
                  事件流
                </Text>
                <Table<DirectorEventRow>
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
                  pagination={{ pageSize: 5, hideOnSinglePage: true }}
                  locale={{ emptyText: <Empty description="暂无比赛事件" imageStyle={{ height: 40 }} /> }}
                />
              </div>
            </Space>
          </Card>
        </Col>

        <Col span={12}>
          <Card title="用户网格">
            <Table<ConsoleUser>
              size="small"
              rowKey="userId"
              columns={userColumns}
              dataSource={users.data?.users ?? []}
              pagination={false}
              locale={{ emptyText: <Empty description="暂无在线用户" imageStyle={{ height: 40 }} /> }}
            />
          </Card>
        </Col>

        <Col span={12}>
          <Card title="审计轨迹">
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              <Text type="secondary">
                轨迹深查（按引用前缀过滤、「为什么说话」详情）收敛在引用审计页，本页留入口。
              </Text>
              <Space wrap>
                <Link to={`/console/citations?matchId=${encodeURIComponent(matchId)}`}>
                  <Button size="small" type="primary">
                    去引用审计查本场轨迹
                  </Button>
                </Link>
              </Space>
            </Space>
          </Card>
        </Col>

        <Col span={12}>
          {/* 投递打断（operations-turn-replay）：本场最近的抢话/打断回合。
              ring 是跨比赛全局容量 20，这里按 matchId 过滤出本场所见。 */}
          <Card data-cell="delivery-interruptions" title="投递打断">
            <Table<DeliveryInterruption>
              size="small"
              rowKey="traceId"
              columns={interruptionColumns}
              dataSource={(interruptions.data?.recent ?? []).filter((row) => row.matchId === matchId)}
              pagination={false}
              locale={{ emptyText: <Empty description="暂无打断记录" imageStyle={{ height: 40 }} /> }}
            />
          </Card>
        </Col>

        {/* 设置：赛前配置 + 自动化播报策略 + 数据源与人工接管。 */}
        <Col span={24}>
          <MatchSettings matchId={matchId} />
        </Col>
      </Row>
    </ObservationPage>
  );
}
