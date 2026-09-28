import { useCallback, useMemo, useState } from 'react';
import { App as AntApp, Button, Card, Col, Empty, Input, Popconfirm, Row, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { consoleApi } from '../api/client';
import type { ConsoleThread } from '../api/client';
import { useOperator } from '../api/operator';
import { fmtTime, threadKindLabels, threadStateLabels, threadStateTag } from '../api/format';
import { runAction, useAsync } from '../api/useAsync';
import ConsolePageShell from '../components/ConsolePageShell';

const { Text } = Typography;


const STATE_OPTIONS = [
  { value: 'open', label: '待答' },
  { value: 'addressed', label: '已答' },
  { value: 'expired', label: '过期' },
];

export default function Threads() {
  const { message: messageApi } = AntApp.useApp();
  const { isDirector } = useOperator();
  const [userIdInput, setUserIdInput] = useState('');
  const [appliedUserId, setAppliedUserId] = useState('');
  const [stateFilter, setStateFilter] = useState<string>('');
  const [actingThreadId, setActingThreadId] = useState<string | null>(null);

  const lister = useCallback(
    () => consoleApi.threads({ userId: appliedUserId || undefined, state: stateFilter || undefined }),
    [appliedUserId, stateFilter],
  );
  const threads = useAsync(lister, [appliedUserId, stateFilter]);

  const rows = useMemo(() => threads.data?.threads ?? [], [threads.data]);

  const patchThread = useCallback(
    (threadId: string, action: 'address' | 'expire') =>
      runAction(messageApi, () => consoleApi.patchThread(threadId, action), {
        success: action === 'address' ? '已标记为已答' : '已标记为过期',
        reload: threads.reload,
        busy: { set: setActingThreadId, key: threadId },
      }),
    [messageApi, threads],
  );

  const columns: ColumnsType<ConsoleThread> = [
    { title: '话题', dataIndex: 'content', key: 'content', ellipsis: true },
    {
      title: '类型',
      dataIndex: 'kind',
      key: 'kind',
      width: 110,
      render: (kind: string) => threadKindLabels[kind] ?? kind,
    },
    {
      title: '用户',
      dataIndex: 'userId',
      key: 'userId',
      width: 150,
      render: (userId: string) => <code style={{ fontSize: 12 }}>{userId}</code>,
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
      width: 180,
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
          <Text type="secondary">{threadStateLabels[record.state] ?? record.state}</Text>
        ),
    },
  ];

  return (
    <ConsolePageShell
      title="话题台账"
      error={threads.error}
      loading={threads.loading}
      onReload={() => void threads.reload()}
      extra={
        <Space wrap>
          <Input
            aria-label="按用户过滤"
            placeholder="按用户 ID 过滤"
            style={{ width: 200 }}
            allowClear
            value={userIdInput}
            onChange={(event) => setUserIdInput(event.target.value)}
            onPressEnter={() => setAppliedUserId(userIdInput.trim())}
          />
          <Button onClick={() => setAppliedUserId(userIdInput.trim())}>过滤</Button>
          <Select
            aria-label="按状态过滤"
            placeholder="状态"
            style={{ width: 120 }}
            allowClear
            options={STATE_OPTIONS}
            value={stateFilter || undefined}
            onChange={(value) => setStateFilter(value ?? '')}
          />
        </Space>
      }
    >
      <Row gutter={[16, 16]}>
        <Col span={24}>
          <Card>
            <Table<ConsoleThread>
              size="small"
              rowKey="id"
              columns={columns}
              dataSource={rows}
              pagination={{ pageSize: 15, hideOnSinglePage: true }}
              locale={{ emptyText: <Empty description="没有匹配的话题" imageStyle={{ height: 48 }} /> }}
            />
          </Card>
        </Col>
      </Row>
    </ConsolePageShell>
  );
}
