import { useCallback, useState } from 'react';
import { Alert, App as AntApp, Button, Card, Col, Form, Input, Modal, Popconfirm, Row, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { consoleApi } from '../api/client';
import type { OperatorRow } from '../api/client';
import { useOperator } from '../api/operator';
import { fmtDateTime } from '../api/format';
import { useAsync } from '../api/useAsync';

const { Text, Paragraph } = Typography;

// 角色 → scope 展示（后端 Operator 行只落 name/role/createdAt；
// scopes 由角色推导，见 operatorauth.ScopesFor）。
const DIRECTOR_SCOPES = [
  ['operator:match:write', '比赛写入'],
  ['operator:fact:confirm', '事实确认'],
  ['operator:fact:correct', '事实更正'],
  ['operator:trace:read', '轨迹读取'],
];
const AUDITOR_SCOPES = [['operator:trace:read', '轨迹读取']];

function scopesForRole(role: string): string[][] {
  return role === 'director' ? DIRECTOR_SCOPES : AUDITOR_SCOPES;
}

export default function Operators() {
  const { message: messageApi } = AntApp.useApp();
  const { operator, loading: operatorLoading, isDirector } = useOperator();
  const [form] = Form.useForm<{ name: string; role: 'director' | 'auditor' }>();
  const [creating, setCreating] = useState(false);
  const [issuedToken, setIssuedToken] = useState<{ name: string; token: string } | null>(null);

  const operators = useAsync<{ operators: OperatorRow[] }>(() => consoleApi.operators(), []);

  const createOperator = useCallback(
    async (values: { name: string; role: 'director' | 'auditor' }) => {
      setCreating(true);
      try {
        const result = await consoleApi.createOperator(values.name.trim(), values.role);
        setIssuedToken({ name: values.name.trim(), token: result.token });
        form.resetFields();
        await operators.reload();
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      } finally {
        setCreating(false);
      }
    },
    [form, messageApi, operators],
  );

  const revokeOperator = useCallback(
    async (name: string) => {
      try {
        await consoleApi.revokeOperator(name);
        messageApi.success(`已吊销 ${name}`);
        await operators.reload();
      } catch (err) {
        messageApi.error(err instanceof Error ? err.message : String(err));
      }
    },
    [messageApi, operators],
  );

  const columns: ColumnsType<OperatorRow> = [
    { title: '姓名', dataIndex: 'name', key: 'name', width: 140 },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 110,
      render: (role: string) =>
        role === 'director' ? <Tag color="orange">导演</Tag> : <Tag color="blue">审计</Tag>,
    },
    {
      title: '权限',
      key: 'scopes',
      render: (_, record) => (
        <Space size={4} wrap>
          {scopesForRole(record.role).map(([scope, label]) => (
            <Tag key={scope}>{label}</Tag>
          ))}
        </Space>
      ),
    },
    { title: '创建', dataIndex: 'createdAt', key: 'createdAt', width: 170, render: fmtDateTime },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, record) =>
        record.name === operator?.name ? (
          <Text type="secondary">当前账号</Text>
        ) : (
          <Popconfirm
            title="吊销运营员"
            description={`吊销后 ${record.name} 的令牌立即失效，审计记录保留姓名。`}
            okText="吊销"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => void revokeOperator(record.name)}
          >
            <Button danger type="link" size="small">
              吊销
            </Button>
          </Popconfirm>
        ),
    },
  ];

  const needsPersistentStore = operators.errorStatus === 501;

  return (
    <Row gutter={[16, 16]}>
      <Col span={24}>
        {!operatorLoading && !isDirector ? (
          <Alert
            type="warning"
            showIcon
            message="仅导演（director）角色可管理运营员"
            description="当前令牌为审计（auditor）角色，只有轨迹读取权限，页面为只读。"
            style={{ marginBottom: 16 }}
          />
        ) : null}
        <Card
          title="运营员"
          extra={
            <Button size="small" onClick={() => void operators.reload()} loading={operators.loading}>
              刷新
            </Button>
          }
        >
          {needsPersistentStore ? (
            <Alert
              type="info"
              showIcon
              message="运营员管理需要持久化存储（DATABASE_URL）"
              description="当前后端使用内存运营员存储，仅支持令牌鉴权；创建/吊销需要配置 DATABASE_URL。"
              style={{ marginBottom: 16 }}
            />
          ) : null}
          {isDirector && operators.error && !needsPersistentStore ? (
            <Alert type="error" showIcon message="运营员列表加载失败" description={operators.error} style={{ marginBottom: 16 }} />
          ) : null}
          {isDirector && !needsPersistentStore ? (
            <Form
              form={form}
              layout="inline"
              onFinish={(values) => void createOperator(values)}
              style={{ marginBottom: 16, rowGap: 8 }}
            >
              <Form.Item
                name="name"
                label="姓名"
                rules={[{ required: true, message: '请输入运营员姓名' }]}
              >
                <Input placeholder="运营员姓名" style={{ width: 180 }} />
              </Form.Item>
              <Form.Item name="role" label="角色" initialValue="auditor" rules={[{ required: true }]}>
                <Select
                  style={{ width: 140 }}
                  options={[
                    { value: 'auditor', label: '审计（只读）' },
                    { value: 'director', label: '导演（全部权限）' },
                  ]}
                />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" loading={creating}>
                  创建运营员
                </Button>
              </Form.Item>
            </Form>
          ) : null}
          <Table<OperatorRow>
            size="small"
            rowKey="id"
            columns={columns}
            dataSource={operators.data?.operators ?? []}
            loading={operators.loading || operatorLoading}
            pagination={false}
            locale={{ emptyText: '暂无运营员' }}
          />
          {isDirector ? (
            <Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
              令牌创建后仅显示一次；吊销 = 删除记录，立即生效，历史审计保留姓名。
            </Paragraph>
          ) : null}
        </Card>
      </Col>

      <Modal
        title="令牌仅显示一次"
        open={Boolean(issuedToken)}
        onCancel={() => setIssuedToken(null)}
        footer={[
          <Button key="done" type="primary" onClick={() => setIssuedToken(null)}>
            我已保存
          </Button>,
        ]}
      >
        {issuedToken ? (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Paragraph>
              运营员 <Text strong>{issuedToken.name}</Text> 的个人令牌如下，请立即交给本人保存，关闭后不再显示：
            </Paragraph>
            <Paragraph code copyable style={{ marginBottom: 0 }}>
              {issuedToken.token}
            </Paragraph>
          </Space>
        ) : null}
      </Modal>
    </Row>
  );
}
