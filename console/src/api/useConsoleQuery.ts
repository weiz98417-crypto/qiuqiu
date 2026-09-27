import { useAsync } from './useAsync';

// useConsoleQuery 是 useAsync 的运营台薄壳：把观测页需要的三态
// （data/loading/error）+ reload 收敛成一个入口，供 ObservationPage 壳统一
// 消费。useAsync 本身已等价（卸载保护、403 文案、deps 触发），这里不重复
// 造轮，只做签名对齐；errorStatus 原样透传（Operators 页 501 降级分支仍依赖）。
export function useConsoleQuery<T>(fetcher: () => Promise<T>, deps: unknown[] = []) {
  return useAsync(fetcher, deps);
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
