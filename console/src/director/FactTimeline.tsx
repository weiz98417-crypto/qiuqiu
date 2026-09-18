import { useState } from 'react';
import { Alert, Button, Card, Empty, Space, Tag, Timeline, Typography } from 'antd';
import type { DirectorConflict, DirectorEventRow } from './api';

const { Text } = Typography;

const EVENT_LABELS: Record<string, string> = {
  match_end: '完场',
  penalty_awarded: '点球判罚',
  kickoff: '开球',
};

function eventLabel(eventType: string): string {
  return EVENT_LABELS[eventType] ?? eventType;
}

function factStatusBadge(factStatus?: string, confirmed?: boolean): { color: string; text: string } {
  switch (factStatus) {
    case 'confirmed':
      return { color: 'green', text: '已确认' };
    case 'provisional':
      return { color: 'gold', text: '候选' };
    case 'conflict':
      return { color: 'red', text: '冲突' };
    case 'reconciled':
      return { color: 'blue', text: '已采用' };
    case 'revoked':
      return { color: 'default', text: '已撤销' };
    default:
      return confirmed ? { color: 'green', text: '事实已确认' } : { color: 'gold', text: '等待事实确认' };
  }
}

interface FactTimelineProps {
  events: DirectorEventRow[];
  conflicts: DirectorConflict[];
  busy: boolean;
  onCorrect: (event: DirectorEventRow) => void;
  onFactTransition: (event: DirectorEventRow, action: string) => void;
  onResolveConflict: (conflict: DirectorConflict, chosenFactId: string) => void;
}

// 事实时间线（ADR-0011 task 4.1）：antd Timeline 展示事件事实、球球主动
// 说一行、事实状态徽标与更正/确认/撤销操作；开放冲突置顶并给单选裁决。
export default function FactTimeline({
  events,
  conflicts,
  busy,
  onCorrect,
  onFactTransition,
  onResolveConflict,
}: FactTimelineProps) {
  if (!events.length && !conflicts.length) {
    return (
      <Card data-testid="director-timeline" title="事实时间线" style={{ border: '1px solid #253142' }}>
        <Empty description="暂无事件。选择球员和事件后，最新记录会出现在这里。" imageStyle={{ height: 48 }} />
      </Card>
    );
  }

  const eventsByFactId = new Map<string, DirectorEventRow>();
  events.forEach((event) => {
    if (event.factId && !eventsByFactId.has(event.factId)) eventsByFactId.set(event.factId, event);
  });
  const openConflicts = conflicts.filter((conflict) => conflict.status === 'open');

  return (
    <Card
      data-testid="director-timeline"
      title="事实时间线"
      style={{ border: '1px solid #253142' }}
      styles={{ body: { paddingTop: 12 } }}
    >
      {openConflicts.map((conflict) => (
        <ConflictResolution
          key={conflict.id}
          conflict={conflict}
          events={events}
          busy={busy}
          onResolve={(chosenFactId) => onResolveConflict(conflict, chosenFactId)}
        />
      ))}
      <Timeline
        items={events.map((event) => {
          const badge = factStatusBadge(event.factStatus, event.confirmed);
          const isActiveRevision = event.status === 'active';
          const roleText = (event.participants || []).map((p) => `${p.role}=${p.name}`).join('；');
          const reportedScore = event.reportedScore || event.score || { home: 0, away: 0 };
          return {
            color: badge.color,
            children: (
              <div data-testid={`timeline-${event.id}`}>
                <Space wrap size={4}>
                  <Tag>{event.clock}</Tag>
                  <Tag color="blue">{event.teamName}</Tag>
                  <Text>{eventLabel(event.eventType)}</Text>
                  <Tag color={badge.color}>{badge.text}</Tag>
                  {!isActiveRevision ? <Tag>历史版本</Tag> : null}
                </Space>
                <div>{event.description}</div>
                {roleText ? <Text type="secondary">参与人：{roleText}</Text> : null}
                {event.proactiveText ? (
                  <div data-testid="proactive-line">
                    <Text type="warning">球球主动说：{event.proactiveText}</Text>
                  </div>
                ) : null}
                {event.evidence?.correctionReason ? (
                  <Text type="secondary">更正原因：{event.evidence.correctionReason}</Text>
                ) : null}
                <Space wrap size={4} style={{ marginTop: 4 }}>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    上报 {reportedScore.home}-{reportedScore.away}
                    {event.effectiveScoreAfter
                      ? ` · 生效 ${event.effectiveScoreAfter.home}-${event.effectiveScoreAfter.away}`
                      : ' · 尚未生效'}
                  </Text>
                  {isActiveRevision ? (
                    <Button size="small" disabled={busy} onClick={() => onCorrect(event)}>
                      {['provisional', 'conflict'].includes(event.factStatus || '') ? '采用到草稿' : '拉回更正'}
                    </Button>
                  ) : null}
                  {isActiveRevision && !(event.factStatus === 'conflict') ? (
                    <>
                      {event.factStatus === 'provisional' ? (
                        <Button size="small" disabled={busy} onClick={() => onFactTransition(event, 'confirm')}>
                          确认
                        </Button>
                      ) : null}
                      {event.factStatus === 'conflict' ? (
                        <Button size="small" disabled={busy} onClick={() => onFactTransition(event, 'reconcile')}>
                          选为事实
                        </Button>
                      ) : null}
                      {['provisional', 'confirmed', 'conflict', 'reconciled'].includes(event.factStatus || '') ? (
                        <Button size="small" danger disabled={busy} onClick={() => onFactTransition(event, 'revoke')}>
                          撤销
                        </Button>
                      ) : null}
                    </>
                  ) : null}
                </Space>
              </div>
            ),
          };
        })}
      />
    </Card>
  );
}

function ConflictResolution({
  conflict,
  events,
  busy,
  onResolve,
}: {
  conflict: DirectorConflict;
  events: DirectorEventRow[];
  busy: boolean;
  onResolve: (chosenFactId: string) => void;
}) {
  const byFactId = new Map<string, DirectorEventRow>();
  events.forEach((event) => {
    if (event.factId) byFactId.set(event.factId, event);
  });
  const members = (conflict.members || [])
    .map((member) => ({ ...member, event: byFactId.get(member.factId) }))
    .filter((member) => member.event) as Array<{ role: string; factId: string; event: DirectorEventRow }>;
  const accepted = members.filter((member) => member.role === 'accepted');
  const candidates = members.filter((member) => member.role === 'candidate');
  const options = [...accepted, ...candidates];
  const [selected, setSelected] = useState<string>(accepted[0]?.factId ?? '');

  if (!options.length) {
    return (
      <Alert type="error" showIcon message="多条赛况存在冲突" description="暂无可选事实成员，请稍后刷新。" style={{ marginBottom: 12 }} />
    );
  }

  return (
    <Alert
      type="error"
      showIcon
      style={{ marginBottom: 12 }}
      message="多条赛况存在冲突"
      description={
        <Space direction="vertical" style={{ width: '100%' }} size={8}>
          {options.map((member) => (
            <Space key={member.factId} wrap>
              <Button
                size="small"
                type={selected === member.factId ? 'primary' : 'default'}
                disabled={busy}
                onClick={() => setSelected(member.factId)}
              >
                {member.role === 'accepted' ? '保留当前' : '采用候选'} · {member.event.clock} {member.event.teamName}{' '}
                {eventLabel(member.event.eventType)}
              </Button>
              <Text type="secondary">{member.event.description}</Text>
            </Space>
          ))}
          <Button
            size="small"
            type="primary"
            danger
            disabled={busy || !selected}
            onClick={() => onResolve(selected)}
          >
            确认事实选择
          </Button>
        </Space>
      }
    />
  );
}
