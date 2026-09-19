// 事件词汇表单源：控制台里「事件类型 → 中文标签」只在这一处定义。
// 三个来源按优先级合并——
//  1. automationEventLabels：后端自动化播报清单（matchstate 默认策略 +
//     operator.html automationEventOptions 的控制台侧镜像）；
//  2. draft 模型 event-model.ts 的 definition.label（与旧页 byte-shape 锁定）；
//  3. extraEventLabels：自动化/外部事件在时间线上的补充展示标签。
// 读方：FactTimeline（时间线展示）、MatchSettings（自动化范围选项）。

export const automationEventLabels: Record<string, string> = {
  kickoff: '开球',
  goal: '进球',
  shot: '射门',
  big_chance: '绝佳机会',
  save: '扑救',
  miss: '错失',
  foul: '犯规',
  yellow_card: '黄牌',
  red_card: '红牌',
  var_check: 'VAR检查',
  var_result: 'VAR结果',
  goal_cancelled: '进球取消',
  penalty: '点球',
  penalty_awarded: '点球判定',
  substitution: '换人',
  injury: '伤停',
  tactical_shift: '战术变化',
  pressure: '持续压迫',
  halftime: '中场',
  fulltime: '完场',
  match_end: '比赛结束',
};

const extraEventLabels: Record<string, string> = {};

export function eventLabel(eventType: string): string {
  return automationEventLabels[eventType] ?? extraEventLabels[eventType] ?? eventType;
}

// 自动化范围选项（MatchSettings 的清单由这里派生，保证与展示标签同源）。
export const automationEventOptions: [string, string][] = Object.entries(automationEventLabels);
