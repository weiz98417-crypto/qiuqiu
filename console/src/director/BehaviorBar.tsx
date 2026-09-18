import { Button, Card, Space, Tag } from 'antd';
import { behaviorGroups } from './templates';

interface BehaviorBarProps {
  onSelectEvent: (eventType: string) => void;
  disabled?: boolean;
}

// 行为按钮条（ADR-0011 tasks 2.2）：按行为组展示 18 类事件，点击只把
// 预设写进当前草稿（draft-only semantics，绝不直接发布）。
export default function BehaviorBar({ onSelectEvent, disabled }: BehaviorBarProps) {
  return (
    <Card
      data-testid="director-behavior-bar"
      title="比赛行为（点击写入草稿）"
      style={{ border: '1px solid #253142' }}
      styles={{ body: { padding: 12 } }}
    >
      <Space direction="vertical" style={{ width: '100%' }} size={8}>
        {behaviorGroups.map((group) => (
          <div key={group.group}>
            <Tag color="geekblue" style={{ marginBottom: 4 }}>
              {group.group}
            </Tag>
            <Space wrap size={4}>
              {group.events.map(({ type, definition }) => (
                <Button
                  key={type}
                  size="small"
                  disabled={disabled}
                  data-testid={`behavior-${type}`}
                  onClick={() => onSelectEvent(type)}
                >
                  {definition.label}
                  {definition.scoreDelta ? <b> +{definition.scoreDelta}</b> : null}
                </Button>
              ))}
            </Space>
          </div>
        ))}
      </Space>
    </Card>
  );
}
