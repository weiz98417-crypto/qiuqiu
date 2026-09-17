import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';

// antd 与 React Router 的衔接：路由内链接统一走 HashRouter 的 Link。
export function ReactRouterLink({ to, children }: { to: string; children: ReactNode }) {
  return <Link to={to}>{children}</Link>;
}
