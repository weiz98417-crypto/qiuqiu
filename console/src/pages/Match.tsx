import { Alert, Button, Card, Col, Empty, Row, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { Link, useParams } from 'react-router-dom';
import { consoleApi } from '../api/client';
import type { ConsoleUser, DirectorEventRow } from '../api/client';
import { fmtTime, talkativenessLabel } from '../api/format';
import { useAsync } from '../api/useAsync';
import MatchSettings from '../components/MatchSettings';

const { Text } = Typography;

export default function MatchPage() {
  const { matchId = '' } = useParams<{ matchId: string }>();

  // 比赛层两路数据：事件流、用户网格。轨迹深查收敛到引用审计页（#c8）。
  const events = useAsync<{ events: DirectorEventRow[] }>(
    () => consoleApi.matchEvents(matchId),
    [matchId],
  );
  const users = useAsync<{ users: ConsoleUser[] }>(() => consoleApi.matchUsers(matchId), [matchId]);

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
    <div>
      <Row gutter={[16, 16]}>
        <Col span={24}>
          <Card
            title={`比赛 · ${matchId}`}
            extra={
              <Button size="small" onClick={() => { void events.reload(); void users.reload(); }}>
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
                  loading={events.loading}
                  pagination={{ pageSize: 5, hideOnSinglePage: true }}
                  locale={{ emptyText: <Empty description="暂无比赛事件" imageStyle={{ height: 40 }} /> }}
                />
              </div>
            </Space>
          </Card>
        </Col>

        <Col span={12}>
          <Card
            title="用户网格"
            extra={
              <Button size="small" onClick={() => void users.reload()}>
                刷新
              </Button>
            }
          >
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

        {/* 设置：赛前配置 + 自动化播报策略 + 数据源与人工接管。 */}
        <Col span={24}>
          <MatchSettings matchId={matchId} />
        </Col>
      </Row>
    </div>
  );
}
