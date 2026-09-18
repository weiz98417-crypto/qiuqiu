// 实战导演页（新版）行为按钮 + 描述模板：从 operator.html#live 移植
// （templates 表 + 行为分组语义）。按钮只写草稿，绝不直接发布
// （director-rewrite tasks 2.2：draft-only semantics）。

import type { EventDefinition } from './event-model';
import { eventDefinitions } from './event-model';

export interface DirectorTemplate {
  type: string;
  text: string;
}

export const directorTemplates: DirectorTemplate[] = [
  ['goal', '禁区内抢点破门。'], ['goal', '远射破门，门将没能碰到。'], ['goal', '反击推进后冷静推射得分。'], ['goal', '定位球机会转化成进球。'], ['goal', '补射得手，把比分改写。'],
  ['big_chance', '单刀机会出现，防线被打穿。'], ['big_chance', '门前连续混战，差点形成进球。'], ['big_chance', '反击三打二，机会非常好。'],
  ['shot', '禁区外尝试一脚射门。'], ['shot', '小角度打门被封堵。'], ['shot', '接传中后头球攻门。'], ['shot', '内切后起脚，球被后卫挡出。'],
  ['save', '门将飞身把近角射门扑出。'], ['save', '门将出击化解单刀。'], ['save', '后卫在门线附近完成关键解围。'],
  ['miss', '门前包抄差一步。'], ['miss', '射门稍稍偏出立柱。'], ['miss', '击中门框弹出。'], ['miss', '面对门将处理得有些着急。'],
  ['foul', '中场战术犯规阻断反击。'], ['foul', '禁区前沿出现身体接触。'], ['foul', '边路对抗动作较大，裁判响哨。'],
  ['yellow_card', '因为战术犯规吃到黄牌。'], ['yellow_card', '动作过大，裁判出示黄牌。'],
  ['red_card', '最后一名防守球员犯规，被直接罚下。'], ['red_card', '裁判出示红牌，比赛形势发生重大变化。'],
  ['var_check', 'VAR 正在检查禁区内接触。'], ['var_check', '裁判等待视频助理裁判确认进球是否有效。'], ['var_check', '主裁去场边看回放。'],
  ['var_result', 'VAR 已经给出最终结果。'], ['goal_cancelled', 'VAR 判定越位，进球取消。'],
  ['penalty', '禁区内犯规，获得点球机会。'], ['penalty', '裁判判罚点球，比赛来到关键时刻。'],
  ['substitution', '准备换人，明显是在调整节奏。'], ['substitution', '换上进攻球员，阵型可能要前压。'], ['substitution', '用防守球员换下前场球员，开始守比分。'],
  ['tactical_shift', '阵型从四后卫切到三中卫。'], ['tactical_shift', '边路压得更靠前，开始加强进攻。'], ['tactical_shift', '中场站位回收，先稳住防线。'],
  ['pressure', '连续把球压在对方半场。'], ['pressure', '这一段攻势很密集，防线压力很大。'], ['pressure', '连续获得角球和二点球机会。'],
  ['injury', '队医进场，比赛暂时中断。'], ['operator_note', '场上节奏变慢，双方都在重新组织。'], ['operator_note', '这一段更像是在试探，不急着冒险。'],
  ['score_correction', '人工核对后更正当前比分。'],
].map(([type, text]) => ({ type, text }));

// 行为分组：事件定义按 definition.group 聚合（保持老页面的组序）。
export const behaviorGroups: Array<{ group: string; events: Array<{ type: string; definition: EventDefinition }> }> = (() => {
  const groups: Array<{ group: string; events: Array<{ type: string; definition: EventDefinition }> }> = [];
  for (const [type, definition] of Object.entries(eventDefinitions)) {
    let group = groups.find((candidate) => candidate.group === definition.group);
    if (!group) {
      group = { group: definition.group, events: [] };
      groups.push(group);
    }
    group.events.push({ type, definition });
  }
  return groups;
})();
