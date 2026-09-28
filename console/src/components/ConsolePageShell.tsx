import { Alert, Button, Space, Spin, Typography } from 'antd';
import type { ReactNode } from 'react';

const { Title } = Typography;

// ConsolePageShell 是运营台页面统一壳：标题栏（title/subtitle/extra + 刷新按钮）
// + 顶部错误 Alert + 内容区 loading 遮罩，替代各页手写的「Alert error +
// 刷新 Button + Table loading」重复组合。三态由调用页传入（与
// useAsync 返回对齐），组件不接管取数——多数据源页面（比赛页/
// 用户页）先聚合再传即可。
export default function ConsolePageShell({
  title,
  subtitle,
  extra,
  error,
  errorTitle,
  loading,
  onReload,
  children,
}: {
  title: string;
  subtitle?: ReactNode;
  extra?: ReactNode;
  // error 非空时顶部展示；errorTitle 给出时作为主文案、error 作描述
  //（概览/运营员页的既有文案习惯），否则 error 本身就是主文案。
  error?: string;
  errorTitle?: string;
  loading?: boolean;
  onReload?: () => void;
  children: ReactNode;
}) {
  return (
    <div>
      {error ? (
        <Alert
          type="error"
          showIcon
          message={errorTitle ?? error}
          description={errorTitle ? error : undefined}
          style={{ marginBottom: 16 }}
        />
      ) : null}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
          flexWrap: 'wrap',
          marginBottom: 16,
        }}
      >
        <Space size={8} align="baseline">
          <Title level={4} style={{ margin: 0 }}>
            {title}
          </Title>
          {subtitle}
        </Space>
        <Space size={8} wrap>
          {extra}
          {onReload ? (
            <Button size="small" onClick={onReload} loading={loading}>
              刷新
            </Button>
          ) : null}
        </Space>
      </div>
      <Spin spinning={Boolean(loading)}>
        {children}
      </Spin>
    </div>
  );
}
