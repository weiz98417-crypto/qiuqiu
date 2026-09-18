import { useState } from 'react';
import { App as AntApp, Button, Card, Collapse, Form, Input, Space, Typography } from 'antd';
import { ApiError, clearToken, login, setToken, api } from '../api/client';
import PasswordChangeModal from './PasswordChangeModal';

const { Title, Paragraph, Text } = Typography;

interface LoginPageProps {
  // 登录失败原因（如 401 被全局登出后的回跳提示）。
  message?: string;
  onAuthenticated: () => void;
}

// 登录页（ADR-0010 task 2.1/2.4）：人类通道走用户名+密码；个人令牌粘贴
// 保留为「高级」页签，供 evals 与脚本使用。
export default function LoginPage({ message, onAuthenticated }: LoginPageProps) {
  const { message: messageApi } = AntApp.useApp();
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(message ?? '');
  const [forceChange, setForceChange] = useState(false);
  const [machineToken, setMachineToken] = useState('');
  const [machineChecking, setMachineChecking] = useState(false);
  const [machineError, setMachineError] = useState('');

  const submit = async (values: { username: string; password: string }) => {
    setSubmitting(true);
    setError('');
    try {
      const result = await login(values.username.trim(), values.password);
      if (result.passwordChangeRequired) {
        // 首登强制改密：改完才算进入（task 2.3）。
        setForceChange(true);
        return;
      }
      onAuthenticated();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError('用户名或密码不正确');
      } else {
        setError(`登录失败：${err instanceof Error ? err.message : String(err)}`);
      }
    } finally {
      setSubmitting(false);
    }
  };

  // 机令牌路径（原 TokenGate 逻辑原样保留）。
  const validateMachineToken = async () => {
    const candidate = machineToken.trim();
    if (!candidate) {
      setMachineError('请输入运营员令牌');
      return;
    }
    setMachineChecking(true);
    setMachineError('');
    setToken(candidate);
    try {
      await api('/api/console/overview', { skipAuthRedirect: true });
      onAuthenticated();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        clearToken();
        setMachineError('运营员令牌不正确');
        messageApi.error('运营员令牌不正确');
      } else if (err instanceof ApiError && err.status === 403) {
        onAuthenticated();
        return;
      } else {
        setMachineError(`校验失败：${err instanceof Error ? err.message : String(err)}`);
      }
    } finally {
      setMachineChecking(false);
    }
  };

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#070B12',
      }}
    >
      <Card
        data-testid="login-page"
        style={{ width: 440, border: '1px solid #253142' }}
        styles={{ body: { padding: 32 } }}
      >
        <Title level={4} style={{ marginTop: 0 }}>
          QiuQiu 运营管理台
        </Title>
        <Paragraph type="secondary">用运营员账号登录；账号与临时密码由导演角色创建签发。</Paragraph>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          {error ? (
            <Text type="danger" data-testid="login-error">
              {error}
            </Text>
          ) : null}
          <Form form={form} layout="vertical" onFinish={submit}>
            <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
              <Input aria-label="用户名" placeholder="运营员用户名" autoComplete="username" />
            </Form.Item>
            <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
              <Input.Password aria-label="密码" placeholder="密码" autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" block loading={submitting}>
              登录
            </Button>
          </Form>
        </Space>
        <Collapse
          ghost
          items={[
            {
              key: 'machine',
              label: <Text type="secondary">高级：使用运营员个人令牌</Text>,
              children: (
                <Space direction="vertical" style={{ width: '100%' }} size="small">
                  <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                    供脚本与评测使用的机器通道：粘贴个人令牌，行为与令牌录入一致。
                  </Paragraph>
                  {machineError ? (
                    <Text type="danger" data-testid="token-error">
                      {machineError}
                    </Text>
                  ) : null}
                  <Input.Password
                    aria-label="运营员令牌"
                    placeholder="粘贴运营员个人令牌"
                    value={machineToken}
                    onChange={(event) => setMachineToken(event.target.value)}
                  />
                  <Button block loading={machineChecking} onClick={validateMachineToken}>
                    保存并验证
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <PasswordChangeModal
        open={forceChange}
        forced
        onClose={() => {}}
        onChanged={() => {
          setForceChange(false);
          onAuthenticated();
        }}
      />
    </div>
  );
}
