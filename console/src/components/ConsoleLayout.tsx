import { useState } from 'react';
import { Breadcrumb, Button, Layout, Menu, Space, Typography, theme as antdTheme } from 'antd';
import { Outlet, Link as RouterLink, useLocation, useNavigate } from 'react-router-dom';
import { ReactRouterLink } from './ReactRouterLink';
import { clearToken, logout, sessionOperator } from '../api/client';
import { useOperator } from '../api/operator';
import PasswordChangeModal from './PasswordChangeModal';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

const NAV_ITEMS = [
  { key: '/console', label: '全局概览' },
  { key: '/console/threads', label: '话题台账' },
  { key: '/console/citations', label: '引用审计' },
  { key: '/console/operators', label: '运营员' },
];

function ConsoleLayout({ onLogout }: { onLogout: () => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { token: antToken } = antdTheme.useToken();
  const { operator } = useOperator();
  // ADR-0010 task 2.3：自助改密入口（首登强制改密走登录页的 forced 弹窗）。
  const [passwordModalOpen, setPasswordModalOpen] = useState(false);

  const selectedKey =
    NAV_ITEMS.find(
      (item) => location.pathname === item.key || location.pathname.startsWith(`${item.key}/`),
    )?.key ?? '/console';
  const onMatchLayer = location.pathname.startsWith('/console/match/');

  const crumbs: { title: React.ReactNode }[] = [{ title: <RouterLink to="/console">运营台</RouterLink> }];
  if (onMatchLayer) {
    const parts = location.pathname.split('/').filter(Boolean); // console, match, :id, user, :uid
    const matchId = parts[2] ?? '';
    crumbs.push({ title: <RouterLink to={`/console/match/${matchId}`}>比赛 {matchId}</RouterLink> });
    if (parts[3] === 'user') {
      crumbs.push({ title: `用户 ${parts[4] ?? ''}` });
    }
  } else {
    const item = NAV_ITEMS.find((nav) => nav.key === selectedKey);
    if (item) crumbs.push({ title: item.label });
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={208} breakpoint="lg" collapsedWidth="0" style={{ borderRight: `1px solid ${antToken.colorBorder}` }}>
        <div style={{ height: 56, display: 'flex', alignItems: 'center', padding: '0 20px' }}>
          <Text strong style={{ fontSize: 16, letterSpacing: 1 }}>
            <span style={{ color: antToken.colorPrimary }}>●</span> QiuQiu 运营台
          </Text>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={[
            ...NAV_ITEMS.map((item) => ({
              key: item.key,
              label: <ReactRouterLink to={item.key}>{item.label}</ReactRouterLink>,
            })),
            { type: 'divider' as const },
            {
              key: 'live',
              label: (
                <a href="/operator.html#live" target="_blank" rel="noreferrer">
                  实战导演台
                </a>
              ),
            },
          ]}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            height: 56,
            lineHeight: '56px',
            padding: '0 24px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            borderBottom: `1px solid ${antToken.colorBorder}`,
          }}
        >
          <Breadcrumb items={crumbs} />
          <Space size="middle">
            {/* ADR-0010 task 2.2：姓名优先取访问令牌的 JWT claims；机令牌
                会话或 claims 未就绪时回退 GET /api/console/whoami。 */}
            <Text type="secondary" data-testid="operator-name">
              {sessionOperator()?.name ?? operator?.name ?? '运营员'}
            </Text>
            <Button size="small" onClick={() => setPasswordModalOpen(true)}>
              修改密码
            </Button>
            <Button
              size="small"
              onClick={async () => {
                // ADR-0010 task 2.2：吊销当前设备刷新令牌后回登录页。
                await logout();
                clearToken();
                onLogout();
                navigate('/console/login', { replace: true });
              }}
            >
              退出
            </Button>
          </Space>
        </Header>
        <Content style={{ padding: 24, overflow: 'auto' }}>
          <Outlet />
        </Content>
      </Layout>
      <PasswordChangeModal
        open={passwordModalOpen}
        onClose={() => setPasswordModalOpen(false)}
        onChanged={() => setPasswordModalOpen(false)}
      />
    </Layout>
  );
}

export default ConsoleLayout;
