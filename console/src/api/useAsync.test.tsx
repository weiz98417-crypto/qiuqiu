import { describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { runAction, useAsync } from './useAsync';

// useAsync / runAction 单测：薄封装层的三态行为与写操作三连。

describe('useAsync', () => {
  it('加载完成后 loading→false、data 就位', async () => {
    const { result } = renderHook(() => useAsync(() => Promise.resolve({ value: 41 }), []));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual({ value: 41 });
    expect(result.current.error).toBe('');
  });

  it('fetcher 失败时 error 就位，reload 换 fetcher 后恢复', async () => {
    // deps=[] 时只有 mount 与 reload 触发 load；fetcher 经 fetcherRef 在每次
    // 渲染时刷新——rerender 换入新 fetcher 后，reload 取到的就是最新者。
    const { result, rerender } = renderHook(
      (fetcher: () => Promise<{ value: number }>) => useAsync(fetcher, []),
      { initialProps: () => Promise.reject(new Error('后端炸了')) },
    );
    await waitFor(() => expect(result.current.error).toBe('后端炸了'));
    expect(result.current.data).toBeNull();

    rerender(() => Promise.resolve({ value: 42 }));
    await act(async () => {
      await result.current.reload();
    });
    expect(result.current.error).toBe('');
    expect(result.current.data).toEqual({ value: 42 });
  });
});

describe('runAction', () => {
  const messageApi = () => ({ success: vi.fn(), error: vi.fn() });

  it('成功路径：action → success 提示 → reload，busy 槽位先置后清', async () => {
    const message = messageApi();
    const reload = vi.fn();
    const setBusy = vi.fn();
    await runAction(message, async () => 'ok', {
      success: '已标记为已答',
      reload,
      busy: { set: setBusy, key: 'thread-1' },
    });
    expect(message.success).toHaveBeenCalledWith('已标记为已答');
    expect(reload).toHaveBeenCalledTimes(1);
    expect(setBusy).toHaveBeenNthCalledWith(1, 'thread-1');
    expect(setBusy).toHaveBeenLastCalledWith(null);
    expect(message.error).not.toHaveBeenCalled();
  });

  it('失败路径：不提示成功、不刷新，错误经 message.error 透出', async () => {
    const message = messageApi();
    const reload = vi.fn();
    await runAction(message, () => Promise.reject(new Error('提交内容冲突')), { success: '已标记为过期', reload });
    expect(message.error).toHaveBeenCalledWith('提交内容冲突');
    expect(message.success).not.toHaveBeenCalled();
    expect(reload).not.toHaveBeenCalled();
  });
});
