import { useCallback, useState } from 'react';
import { App as AntApp, Button, Card, Input, Space, Typography } from 'antd';
import { ApiError, api, clearToken, setToken } from '../api/client';

const { Title, Paragraph, Text } = Typography;

interface TokenGateProps {
  message?: string;
  onValidated: () => void;
}

// 令牌录入页：个人令牌粘贴一次即持久化到 localStorage
// ['qiuqiu.console.token']（沿用 operator.html 的令牌惯用法）。
export default function TokenGate({ message, onValidated }: TokenGateProps) {
  const { message: messageApi } = AntApp.useApp();
  const [token, setTokenValue] = useState('');
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState(message ?? '');

  const validate = useCallback(async () => {
    const candidate = token.trim();
    if (!candidate) {
      setError('请输入运营员令牌');
      return;
    }
    setChecking(true);
    setError('');
    // 先落库再校验：校验请求需要携带 Authorization 头。
    setToken(candidate);
    try {
      // 概览是最低成本的读校验（auditor 也可见）；skipAuthRedirect
      // 避免 401 触发全局跳转，由本页自己处理错误态。
      await api('/api/console/overview', { skipAuthRedirect: true });
      onValidated();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        clearToken();
        setError('运营员令牌不正确');
        messageApi.error('运营员令牌不正确');
      } else if (err instanceof ApiError && err.status === 403) {
        // 已认证但无概览读取权限：令牌本身有效，允许进入（各页自行处理 403）。
        onValidated();
        return;
      } else {
        setError(`校验失败：${err instanceof Error ? err.message : String(err)}`);
      }
    } finally {
      setChecking(false);
    }
  }, [token, messageApi, onValidated]);

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
        data-testid="token-gate"
        style={{ width: 420, border: '1px solid #253142' }}
        styles={{ body: { padding: 32 } }}
      >
        <Title level={4} style={{ marginTop: 0 }}>
          QiuQiu 运营管理台
        </Title>
        <Paragraph type="secondary">
          输入你的运营员个人令牌。令牌只需录入一次，之后保存在本机浏览器中；令牌丢失请联系导演角色重新签发。
        </Paragraph>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          {error ? <Text type="danger" data-testid="token-error">{error}</Text> : null}
          <Input.Password
            aria-label="运营员令牌"
            placeholder="粘贴运营员个人令牌"
            value={token}
            onChange={(event) => setTokenValue(event.target.value)}
            onPressEnter={validate}
          />
          <Button type="primary" block loading={checking} onClick={validate}>
            保存并验证
          </Button>
        </Space>
      </Card>
    </div>
  );
}
