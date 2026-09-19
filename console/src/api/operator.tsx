import { createContext, useContext, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { consoleApi, SCOPE_MATCH_WRITE, sessionOperator } from './client';
import type { WhoAmI } from './client';

// 当前运营员身份上下文：身份单源 —— JWT 会话先解 claims 即时上屏（姓名
// 与 scopes 预热），whoami 随后校准（机令牌会话由此获得身份）；组件不自行
// 解码令牌，写操作 UI 按 scopes 门控（auditor 只读）。

interface OperatorContextValue {
  operator: WhoAmI | null;
  loading: boolean;
  isDirector: boolean;
}

const OperatorContext = createContext<OperatorContextValue>({
  operator: null,
  loading: true,
  isDirector: false,
});

export function OperatorProvider({ children }: { children: ReactNode }) {
  const [operator, setOperator] = useState<WhoAmI | null>(() => {
    const claims = sessionOperator();
    return claims ? { name: claims.name, subject: claims.name, scopes: claims.scopes } : null;
  });
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    consoleApi
      .whoami()
      .then((who) => {
        if (!cancelled) setOperator(who);
      })
      .catch(() => {
        // 401 由全局层处理（清除令牌回录入页）；whoami 对任何已认证令牌可见。
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const isDirector = Boolean(operator?.scopes.includes(SCOPE_MATCH_WRITE));

  return (
    <OperatorContext.Provider value={{ operator, loading, isDirector }}>
      {children}
    </OperatorContext.Provider>
  );
}

export function useOperator(): OperatorContextValue {
  return useContext(OperatorContext);
}
