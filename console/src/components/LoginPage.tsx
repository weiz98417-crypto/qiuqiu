import { useEffect, useState } from 'react';
import { App as AntApp, Button, Card, Collapse, Form, Grid, Input, Space, Typography } from 'antd';
import { ApiError, clearToken, login, setToken, api } from '../api/client';
import PasswordChangeModal from './PasswordChangeModal';

const { Title, Paragraph, Text } = Typography;

// 品牌区背景图约定路径：console/public/login-bg.jpg（public 资产按 base
// 前缀 /console/ 发布，dev 与 dist 挂载下同径）。文件存在即显示；不存在则
// 落深蓝渐变占位。
const LOGIN_BG_URL = '/console/login-bg.jpg';

// 占位态：深蓝工作台渐变 + 细微暖光与球场圆环装饰。
const BRAND_FALLBACK_BG = [
  'radial-gradient(ellipse 640px 420px at 18% 8%, rgba(255, 107, 53, 0.16), transparent 62%)',
  'radial-gradient(ellipse 720px 540px at 88% 96%, rgba(85, 168, 255, 0.12), transparent 60%)',
  'radial-gradient(circle at 78% 24%, transparent 128px, rgba(244, 241, 232, 0.05) 130px, rgba(244, 241, 232, 0.05) 132px, transparent 135px)',
  'linear-gradient(158deg, #0B2E68 0%, #0E2344 38%, #070B12 82%)',
].join(', ');

// 图位就位：暗色渐变遮罩压住背景图，保品牌区文字可读。
const BRAND_IMAGE_BG = [
  'linear-gradient(180deg, rgba(7, 11, 18, 0.36) 0%, rgba(7, 11, 18, 0.5) 58%, rgba(7, 11, 18, 0.78) 100%)',
  `url(${LOGIN_BG_URL})`,
  'linear-gradient(158deg, #0B2E68 0%, #070B12 82%)',
].join(', ');

interface LoginPageProps {
  // 登录失败原因（如 401 被全局登出后的回跳提示）。
  message?: string;
  onAuthenticated: () => void;
}

// 登录页（ADR-0010 task 2.1/2.4）：人类通道走用户名+密码；个人令牌粘贴
// 保留为「高级」页签，供 evals 与脚本使用。布局为左品牌区 + 右表单卡：
// 品牌区背景图按 LOGIN_BG_URL 约定路径探测，未就位时占位渐变自成立。
export default function LoginPage({ message, onAuthenticated }: LoginPageProps) {
  const { message: messageApi } = AntApp.useApp();
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(message ?? '');
  const [forceChange, setForceChange] = useState(false);
  const [machineToken, setMachineToken] = useState('');
  const [machineChecking, setMachineChecking] = useState(false);
  const [machineError, setMachineError] = useState('');
  // 窄屏（<md）收起品牌区，只留表单卡；首帧按宽屏渲染防闪烁。
  const screens = Grid.useBreakpoint();
  const showBrand = screens.md !== false;
  // 背景图探测：加载成功才叠加图位，404 时保持占位渐变。
  const [bgLoaded, setBgLoaded] = useState(false);
  useEffect(() => {
    const probe = new Image();
    probe.onload = () => setBgLoaded(true);
    probe.src = LOGIN_BG_URL;
  }, []);

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
    <div style={{ minHeight: '100vh', display: 'flex', background: '#070B12' }}>
      {showBrand ? (
        <aside
          style={{
            flex: '1 1 58%',
            minWidth: 0,
            display: 'flex',
            flexDirection: 'column',
            padding: '44px 56px 28px',
            backgroundImage: bgLoaded ? BRAND_IMAGE_BG : BRAND_FALLBACK_BG,
            backgroundSize: 'cover',
            backgroundPosition: 'center',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 12 }}>
            <Text strong style={{ fontSize: 16, letterSpacing: 1, color: '#F4F1E8' }}>
              <span style={{ color: '#FF6B35' }}>●</span> 球球 QIUQIU
            </Text>
            <Text style={{ fontSize: 12, letterSpacing: 3, color: 'rgba(244, 241, 232, 0.58)' }}>
              运营管理台
            </Text>
          </div>
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
            <Title
              level={2}
              style={{
                maxWidth: 560,
                marginTop: 0,
                marginBottom: 16,
                color: '#F4F1E8',
                fontSize: 34,
                fontWeight: 700,
                lineHeight: 1.4,
              }}
            >
              球球 · 每一场比赛都有人陪你聊
            </Title>
            <Paragraph style={{ marginBottom: 0, color: 'rgba(244, 241, 232, 0.62)', fontSize: 15 }}>
              直播监听 · 话题台账 · 引用审计，一站掌控。
            </Paragraph>
          </div>
          <Text style={{ fontSize: 12, color: 'rgba(244, 241, 232, 0.42)' }}>
            © 2026 QiuQiu · 内部运营环境
          </Text>
        </aside>
      ) : null}
      <main
        style={{
          flex: '1 1 42%',
          minWidth: 420,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: 24,
        }}
      >
        <Card
          data-testid="login-page"
          style={{ width: 440, border: '1px solid #253142', boxShadow: '0 24px 64px rgba(0, 0, 0, 0.4)' }}
          styles={{ body: { padding: 32 } }}
        >
          <Title level={4} style={{ marginTop: 0 }}>
            <span style={{ color: '#FF6B35' }}>●</span> QiuQiu 运营管理台
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
      </main>
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
