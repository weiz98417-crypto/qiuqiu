import { Drawer, Space, Tag, Typography } from 'antd';
import type { TraceRow } from './client';
import { fmtDateTime, reasonCodeLabel } from './format';

// 「为什么说话」证据 module：trace 原因码提取、引用前缀匹配、详情抽屉。
// Match 页与引用审计页共用，改 trace 语义只动这一个文件。

export function traceReasonCodes(trace: TraceRow): string[] {
  if (trace.reasonCodes?.length) return trace.reasonCodes;
  return trace.relationshipDecision?.reasonCodes ?? [];
}

// citation 前缀在 reasonCodes 上做前缀匹配（与后端 citation= 过滤一致）。
export function matchesCitation(trace: TraceRow, prefix: string): boolean {
  if (!prefix) return true;
  const haystack = [trace.reason ?? '', ...traceReasonCodes(trace)];
  return haystack.some((code) => code.startsWith(prefix));
}

const { Text } = Typography;

export function WhyDrawer({ trace, onClose }: { trace: TraceRow | null; onClose: () => void }) {
  return (
    <Drawer title="为什么说话" open={Boolean(trace)} onClose={onClose} width={480}>
      {trace ? (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <div>
            <Text type="secondary">Trace</Text>
            <div>
              <code>{trace.id}</code>
            </div>
          </div>
          <div>
            <Text type="secondary">原因码</Text>
            <div>
              <Space size={4} wrap>
                {traceReasonCodes(trace).map((code) => (
                  <Tag key={code} color="orange">
                    {reasonCodeLabel(code)}
                  </Tag>
                ))}
                {!traceReasonCodes(trace).length ? <Text type="secondary">{trace.reason || '—'}</Text> : null}
              </Space>
            </div>
          </div>
          <div>
            <Text type="secondary">用户输入</Text>
            <div>{trace.input || '（主动回合，无用户输入）'}</div>
          </div>
          <div>
            <Text type="secondary">球球输出</Text>
            <div>{trace.output || '—'}</div>
          </div>
          <div>
            <Text type="secondary">时间</Text>
            <div>{fmtDateTime(trace.createdAt)}</div>
          </div>
        </Space>
      ) : null}
    </Drawer>
  );
}
