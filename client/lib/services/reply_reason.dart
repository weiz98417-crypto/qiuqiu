/// 主动回合理由的客户端文案映射(memory-surfacing 1.6):服务端 reason.codes
/// → 用户可读的一句话。未认识的码原样忽略——宁可少说,不说机器话。
List<String> replyReasonPhrases(List<dynamic>? codes) {
  if (codes == null) return const [];
  final phrases = <String>[];
  for (final code in codes) {
    final value = code?.toString() ?? '';
    if (value.startsWith('proactive_citation:reminder:')) {
      phrases.add('这是你之前订的开球提醒');
    } else if (value.startsWith('proactive_citation:subscription:')) {
      phrases.add('这是你订阅的球队赛程提醒');
    } else if (value.startsWith('proactive_citation:pivotal')) {
      phrases.add('这是关键时刻，我想跟你一起看');
    } else if (value == 'policy_memory:favorite_team_goal') {
      phrases.add('你的主队进球了，我第一时间想到你');
    } else if (value == 'open_thread_recovery') {
      phrases.add('接着上次没聊完的话题');
    } else if (value == 'favorite_team_goal') {
      phrases.add('你的主队进球了');
    }
  }
  return phrases;
}

/// 把短语列表折成展示行(取前两条,顿号连接);空返回 null。
String? replyReasonLine(List<dynamic>? codes) {
  final phrases = replyReasonPhrases(codes);
  if (phrases.isEmpty) return null;
  return phrases.take(2).join('；');
}
