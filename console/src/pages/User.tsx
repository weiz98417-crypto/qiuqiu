import { useCallback, useState } from 'react';
import { Alert, App as AntApp, Button, Card, Col, Empty, Popconfirm, Row, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useParams } from 'react-router-dom';
import { consoleApi } from '../api/client';
import type { ConsoleThread, InteractionEventRow, PortraitEntry } from '../api/client';
import { useOperator } from '../api/operator';
import { fmtTime, threadKindLabels, threadStateTag } from '../api/format';
import { useAsync } from '../api/useAsync';

const { Text, Paragraph } = Typography;


const KIND_LABELS: Record<string, string> = {
  user_message: '用户发言',
  assistant_reply: '球球回复',
  proactive: '主动发言',
  playback_result: '播报回执',
};

export default function UserPage() {
  const { matchId = '', userId = '' } = useParams<{ matchId: string; userId: string }>();
  const { message: messageApi } = AntApp.useApp();
  const { isDirector } = useOperator();
  const [actingThreadId, setActingThreadId] = useState<string | null>(null);

  const portrait = useAsync(() => consoleApi.portrait(userId), [userId]);
  const threads = useAsync(() => consoleApi.threads({ userId }), [userId]);
  const history = useAsync(() => consoleApi.interaction(matchId, userId), [matchId, userId]);

  const patchThread = useCallback(
    async (threadId: string, action: 'address' | 'expire') => {
      setActingThreadId(threadId);
      try {
        await consoleApi.patchThread(threadId, action);
        messageApi.success(action === 'address' ? '已标记为已答' : '已标记为过期');
        await threads.reload();
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      } finally {
        setActingThreadId(null);
      }
    },
    [messageApi, threads],
  );

  const deleteSlot = useCallback(
    async (entry: PortraitEntry) => {
      try {
        await consoleApi.deletePortraitSlot(userId, entry.topic, entry.subTopic);
        messageApi.success(`已删除画像槽位 ${entry.topic}/${entry.subTopic}（操作已入审计）`);
        await portrait.reload();
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      }
    },
    [messageApi, portrait, userId],
  );

  const portraitColumns: ColumnsType<PortraitEntry> = [
    { title: '主题', dataIndex: 'topic', key: 'topic', width: 130 },
    { title: '子主题', dataIndex: 'subTopic', key: 'subTopic', width: 130 },
    { title: '内容', dataIndex: 'content', key: 'content', ellipsis: true },
    { title: '更新', dataIndex: 'updatedAt', key: 'updatedAt', width: 110, render: fmtTime },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      render: (_, record) =>
        isDirector ? (
          <Popconfirm
            title="删除画像槽位"
            description={`确认以用户利益为先删除「${record.topic}/${record.subTopic}」？该操作会记入审计且不可导出内容。`}
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => void deleteSlot(record)}
          >
            <Button danger type="link" size="small">
              删除
            </Button>
          </Popconfirm>
        ) : (
          <Text type="secondary">只读</Text>
        ),
    },
  ];

  const threadColumns: ColumnsType<ConsoleThread> = [
    { title: '话题', dataIndex: 'content', key: 'content', ellipsis: true },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 100,
      render: (kind: string) => threadKindLabels[kind] ?? kind,
    },
    {
      title: '状态',
      dataIndex: 'state',
      key: 'state',
      width: 90,
      render: (state: string) => (
        <Tag color={threadStateTag(state).color}>{threadStateTag(state).label}</Tag>
      ),
    },
    { title: '台账序号', dataIndex: 'ledgerSequence', key: 'ledgerSequence', width: 90, align: 'right' },
    { title: '创建', dataIndex: 'createdAt', key: 'createdAt', width: 110, render: fmtTime },
    {
      title: '操作',
      key: 'actions',
      width: 170,
      render: (_, record) =>
        !isDirector ? (
          <Text type="secondary">只读</Text>
        ) : record.state === 'open' ? (
          <Space size={4}>
            <Button
              type="link"
              size="small"
              disabled={actingThreadId === record.id}
              onClick={() => void patchThread(record.id, 'address')}
            >
              标记已答
            </Button>
            <Popconfirm
              title="过期该话题"
              description="过期表示话题超时未答，将从开放台账移除。"
              okText="过期"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={() => void patchThread(record.id, 'expire')}
            >
              <Button danger type="link" size="small" disabled={actingThreadId === record.id}>
                过期
              </Button>
            </Popconfirm>
          </Space>
        ) : (
          <Text type="secondary">—</Text>
        ),
    },
  ];

  const historyColumns: ColumnsType<InteractionEventRow> = [
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 110,
      render: (kind: string) => <Tag>{KIND_LABELS[kind] ?? kind}</Tag>,
    },
    {
      title: '内容',
      key: 'content',
      render: (_, record) => record.outputText || record.inputText || record.deliveryState || '—',
      ellipsis: true,
    },
    { title: '投递', dataIndex: 'deliveryState', key: 'deliveryState', width: 100 },
    { title: '时间', dataIndex: 'createdAt', key: 'createdAt', width: 110, render: fmtTime },
  ];

  return (
    <div>
      {portrait.error ? <Alert type="error" showIcon message={portrait.error} style={{ marginBottom: 16 }} /> : null}
      <Row gutter={[16, 16]}>
        <Col span={12}>
          <Card title="画像（只读 + 代客删除）" extra={<Text type="secondary">用户 {userId}</Text>}>
            <Table<PortraitEntry>
              size="small"
              rowKey={(record) => `${record.topic}/${record.subTopic}`}
              columns={portraitColumns}
              dataSource={portrait.data?.entries ?? []}
              loading={portrait.loading}
              pagination={false}
              locale={{ emptyText: <Empty description="画像暂无条目" imageStyle={{ height: 40 }} /> }}
            />
            <Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
              画像对运营只读；删除槽位需二次确认并记入审计，不支持导出。
            </Paragraph>
          </Card>
        </Col>
        <Col span={12}>
          <Card title="话题台账">
            {threads.error ? <Alert type="error" showIcon message={threads.error} /> : null}
            <Table<ConsoleThread>
              size="small"
              rowKey="id"
              columns={threadColumns}
              dataSource={threads.data?.threads ?? []}
              loading={threads.loading}
              pagination={false}
              locale={{ emptyText: <Empty description="该用户暂无话题" imageStyle={{ height: 40 }} /> }}
            />
          </Card>
        </Col>
        <Col span={24}>
          <Card title="交互历史">
            {history.error ? <Alert type="error" showIcon message={history.error} /> : null}
            <Table<InteractionEventRow>
              size="small"
              rowKey="id"
              columns={historyColumns}
              dataSource={history.data?.events ?? []}
              loading={history.loading}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              locale={{ emptyText: <Empty description="暂无交互记录" imageStyle={{ height: 40 }} /> }}
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
}
