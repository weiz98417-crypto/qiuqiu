import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError } from '../api/client';

// 轻量取数 Hook：load() 触发请求，loading/error 状态内聚，
// 组件卸载后不再写入状态。
export function useAsync<T>(fetcher: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>('');
  const [errorStatus, setErrorStatus] = useState(0);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await fetcherRef.current();
      if (mounted.current) {
        setData(result);
        setError('');
        setErrorStatus(0);
      }
    } catch (err) {
      if (mounted.current) {
        const status = err instanceof ApiError ? err.status : 0;
        const text =
          err instanceof ApiError && status === 403
            ? '权限不足：当前令牌无权访问该资源'
            : err instanceof Error
              ? err.message
              : String(err);
        setError(text);
        setErrorStatus(status);
      }
    } finally {
      if (mounted.current) setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    void load();
  }, [load]);

  return { data, loading, error, errorStatus, reload: load };
}

// antd MessageInstance 的结构子集（App.useApp().message 可直接赋值）。
type ConsoleMessageApi = {
  success: (message: string) => void;
  error: (message: string) => void;
};

// runAction 收敛写操作后的页面侧三连：成功提示 → 刷新 → 失败提示（错误
// 文案与各页手写版逐字一致）。busy 槽位可选：按行 busy 传行 ID（如
// actingThreadId），整页二态 busy 传任意非空 key。
export async function runAction(
  messageApi: ConsoleMessageApi,
  action: () => Promise<unknown>,
  options: {
    success?: string;
    reload?: () => Promise<unknown> | void;
    busy?: { set: (busy: string | null) => void; key: string };
  } = {},
): Promise<void> {
  options.busy?.set(options.busy.key);
  try {
    await action();
    if (options.success) messageApi.success(options.success);
    await options.reload?.();
  } catch (err) {
    messageApi.error(err instanceof Error ? err.message : String(err));
  } finally {
    options.busy?.set(null);
  }
}
