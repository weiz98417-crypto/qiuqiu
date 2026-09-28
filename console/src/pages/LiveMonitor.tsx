import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge, Button, Card, Col, Descriptions, Empty, Input, List, Row, Select, Space, Tag, Typography } from 'antd';
import { Link } from 'react-router-dom';
import { getAccessToken, getToken } from '../api/client';
import { INTERACTION_KINDS, kindMeta } from '../api/kinds';
import { fmtDateTime, fmtTime } from '../api/format';
import ConsolePageShell from '../components/ConsolePageShell';

const { Text } = Typography;

// 直播监听（operations-live-stream，ADR-0021）：/ws/ops 只读旁路流的运营面。
// 隐私纪律——wire 事件不含任何正文字段，本页也只呈现元数据（kind/路由/状态/
// 延迟），正文请循「打开比赛页」回到业务页。

// /ws/ops 下行事件形状（backend/cmd/server/ops_stream.go OpsEventWire）。
interface OpsEventRow {
  type?: string;
  kind: string;
  matchId: string;
  userId?: string;
  traceId?: string;
  deliveryState?: string;
  playbackState?: string;
  latencyMs?: number;
  at: string;
}

type StreamStatus = 'connecting' | 'connected' | 'disconnected';

const STATUS_META: Record<StreamStatus, { status: 'success' | 'warning' | 'error'; text: string }> = {
  connected: { status: 'success', text: '实时流已连接' },
  connecting: { status: 'warning', text: '连接中…' },
  disconnected: { status: 'error', text: '实时流未连接' },
};

// 事件流条数上限：只读观测流不吃内存涨（最新在上，超出即弃）。
const MAX_ROWS = 200;
// 重连退避：5s 起步指数翻倍、封顶 30s——/ws/ops 不可达/未授权时优雅降级，
// 不刷错误风暴。
const RECONNECT_BASE_MS = 5000;
const RECONNECT_MAX_MS = 30000;

// base64url 编码（UTF-8 安全）：WS 子协议令牌惯用法，语义同 runtime-e2e
// openOpsSocket 的 Buffer.base64url；浏览器端用 TextEncoder 处理中文载荷。
function base64urlEncode(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = '';
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// 相对时间：列表行的 at 展示；超过一天回落绝对时间（fmtTime 口径）。
function fmtRelative(at: string, now: number): string {
  const time = new Date(at).getTime();
  if (Number.isNaN(time)) return at;
  const seconds = Math.max(0, Math.round((now - time) / 1000));
  if (seconds < 10) return '刚刚';
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return fmtTime(at);
}

const KIND_OPTIONS = [
  { value: '', label: '全部' },
  ...Object.entries(INTERACTION_KINDS).map(([value, meta]) => ({ value, label: meta.label })),
];

export default function LiveMonitor() {
  const [status, setStatus] = useState<StreamStatus>('connecting');
  const [rows, setRows] = useState<OpsEventRow[]>([]);
  const [selected, setSelected] = useState<OpsEventRow | null>(null);
  const [kindFilter, setKindFilter] = useState<string>('');
  const [userIdFilter, setUserIdFilter] = useState('');
  // connectNonce 驱动 WS 建连 effect：退避重连与手动重连都走同一条
  // cleanup 路径（关旧连接 + 清定时器），StrictMode 双跑也对称。
  const [connectNonce, setConnectNonce] = useState(0);
  const attemptRef = useRef(0);
  const reconnectTimer = useRef<number | null>(null);
  // 相对时间的时钟：15s 一跳，列表行的「N 秒前」保持新鲜。
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 15000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    // 令牌与 api() 同源：内存访问令牌优先（个人令牌 JWT 带 trace_read
    // scope，过 UpgradeOps 运营面），机令牌兜底（legacy 通道）。
    const token = getAccessToken() || getToken();
    setStatus('connecting');
    let disposed = false;
    const protocols = token ? [`qiuqiu-auth.${base64urlEncode(token)}`] : [];
    const socket = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/ops`,
      protocols,
    );

    socket.onopen = () => {
      if (disposed) return;
      attemptRef.current = 0;
      setStatus('connected');
    };
    socket.onmessage = (event) => {
      if (disposed || typeof event.data !== 'string') return;
      let message: OpsEventRow;
      try {
        message = JSON.parse(event.data);
      } catch {
        return;
      }
      if (message?.type !== 'ops_event' || !message.kind) return;
      setRows((prev) => [message, ...prev].slice(0, MAX_ROWS));
    };
    // 403（无 scope）/ 后端未起都汇到 onclose：只翻状态并安排退避重连，
    // 不弹错误——静态伺服（无 WS）环境下页面也稳定渲染未连接态。
    socket.onclose = () => {
      if (disposed) return;
      setStatus('disconnected');
      const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** attemptRef.current);
      attemptRef.current += 1;
      reconnectTimer.current = window.setTimeout(() => {
        if (!disposed) setConnectNonce((nonce) => nonce + 1);
      }, delay);
    };
    socket.onerror = () => {
      if (!disposed) setStatus('disconnected');
    };

    return () => {
      disposed = true;
      if (reconnectTimer.current !== null) {
        window.clearTimeout(reconnectTimer.current);
        reconnectTimer.current = null;
      }
      // 先摘回调再关：主动关闭不触发 onclose 的重连调度。
      socket.onopen = null;
      socket.onmessage = null;
      socket.onerror = null;
      socket.onclose = null;
      if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
        socket.close();
      }
    };
  }, [connectNonce]);

  const reconnect = useCallback(() => {
    attemptRef.current = 0;
    if (reconnectTimer.current !== null) {
      window.clearTimeout(reconnectTimer.current);
      reconnectTimer.current = null;
    }
    setConnectNonce((nonce) => nonce + 1);
  }, []);

  const visibleRows = useMemo(() => {
    const needle = userIdFilter.trim().toLowerCase();
    return rows.filter((row) => {
      if (kindFilter && row.kind !== kindFilter) return false;
      if (needle && !(row.userId ?? '').toLowerCase().includes(needle)) return false;
      return true;
    });
  }, [rows, kindFilter, userIdFilter]);

  const statusMeta = STATUS_META[status];

  return (
    <ConsolePageShell
      title="直播监听"
      subtitle={<Badge status={statusMeta.status} text={statusMeta.text} />}
      extra={
        <Button size="small" onClick={reconnect} disabled={status === 'connected'}>
          重连
        </Button>
      }
    >
      <Row gutter={[16, 16]}>
        <Col span={14}>
          <Card
            title="实时事件流"
            extra={
              <Space wrap>
                <Select
                  aria-label="按类型过滤"
                  placeholder="全部"
                  style={{ width: 130 }}
                  options={KIND_OPTIONS}
                  value={kindFilter || undefined}
                  onChange={(value) => setKindFilter(value ?? '')}
                />
                <Input
                  aria-label="按用户过滤"
                  placeholder="按用户 ID 过滤"
                  style={{ width: 170 }}
                  allowClear
                  value={userIdFilter}
                  onChange={(event) => setUserIdFilter(event.target.value)}
                />
              </Space>
            }
          >
            <div style={{ maxHeight: 560, overflowY: 'auto' }}>
              <List
                size="small"
                dataSource={visibleRows}
                locale={{
                  emptyText: (
                    <Empty
                      description={
                        status === 'disconnected'
                          ? '未连接实时流，连接后事件将在此滚动显示'
                          : '暂无实时事件'
                      }
                      imageStyle={{ height: 48 }}
                    />
                  ),
                }}
                renderItem={(row, index) => {
                  const meta = kindMeta(row.kind);
                  return (
                    <List.Item
                      key={`${row.at}-${row.kind}-${row.traceId ?? row.userId ?? ''}-${index}`}
                      onClick={() => setSelected(row)}
                      style={{
                        cursor: 'pointer',
                        padding: '8px 12px',
                        background: selected === row ? '#182234' : undefined,
                      }}
                    >
                      <List.Item.Meta
                        avatar={<Tag color={meta.color}>{meta.label}</Tag>}
                        title={
                          <Space size={6} wrap>
                            <Text strong style={{ fontSize: 13 }}>
                              {row.userId || '—'}
                            </Text>
                            <Text type="secondary" style={{ fontSize: 12 }}>
                              <code>{row.matchId}</code>
                            </Text>
                          </Space>
                        }
                        description={
                          <Space size={10} wrap style={{ fontSize: 12 }}>
                            {row.latencyMs !== undefined ? <span>延迟 {row.latencyMs} ms</span> : null}
                            {row.deliveryState ? <span>投递 {row.deliveryState}</span> : null}
                            {row.playbackState ? <span>播放 {row.playbackState}</span> : null}
                          </Space>
                        }
                      />
                      <Text type="secondary" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
                        {fmtRelative(row.at, now)}
                      </Text>
                    </List.Item>
                  );
                }}
              />
            </div>
          </Card>
        </Col>
        <Col span={10}>
          <Card
            title="事件详情"
            extra={
              selected ? (
                <Link to={`/console/match/${encodeURIComponent(selected.matchId)}`}>
                  <Button size="small" type="primary" disabled={!selected.matchId}>
                    打开比赛页
                  </Button>
                </Link>
              ) : null
            }
          >
            {selected ? (
              <Descriptions
                size="small"
                column={1}
                items={[
                  {
                    key: 'kind',
                    label: '类型',
                    children: <Tag color={kindMeta(selected.kind).color}>{kindMeta(selected.kind).label}</Tag>,
                  },
                  { key: 'type', label: 'Wire', children: <code style={{ fontSize: 12 }}>{selected.type || 'ops_event'}</code> },
                  { key: 'userId', label: '用户', children: selected.userId || '—' },
                  { key: 'matchId', label: '比赛', children: selected.matchId || '—' },
                  {
                    key: 'traceId',
                    label: 'Trace',
                    children: selected.traceId ? <code style={{ fontSize: 12 }}>{selected.traceId}</code> : '—',
                  },
                  { key: 'deliveryState', label: '投递', children: selected.deliveryState || '—' },
                  { key: 'playbackState', label: '播放', children: selected.playbackState || '—' },
                  {
                    key: 'latencyMs',
                    label: '延迟',
                    children: selected.latencyMs !== undefined ? `${selected.latencyMs} ms` : '—',
                  },
                  { key: 'at', label: '时间', children: fmtDateTime(selected.at) },
                ]}
              />
            ) : (
              <Empty description="从左侧选择一条事件查看详情" imageStyle={{ height: 48 }} />
            )}
          </Card>
        </Col>
      </Row>
    </ConsolePageShell>
  );
}
