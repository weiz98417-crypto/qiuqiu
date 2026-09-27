import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/turn_detector.dart';

/// 红测（bug 猎手）：RemoteTurnModel 的结论「绑定其判定时的文本」契约
/// （类注释：partial 文本更新即作废，重询带新文本）没有覆盖在途竞态——
/// 查询发出后 partial 文本更新，旧文本的 turn_result 迟到抵达时仍被
/// acceptResult 存为有效结论，按旧文本的判定提前判完新文本的话轮。
void main() {
  TurnDecisionContext ctx(int silenceMs) => TurnDecisionContext(
        utteranceDuration: const Duration(seconds: 2),
        trailingSilence: Duration(milliseconds: silenceMs),
      );

  test('在途结论迟到于 partial 文本更新：不得按旧文本结论提前判完', () {
    final sent = <Map<String, dynamic>>[];
    final model = RemoteTurnModel(send: (message) {
      sent.add(message);
      return true;
    });
    model.beginUtterance('utt-1');
    model.noteText(utteranceId: 'utt-1', text: '好进了');
    expect(model.call(ctx(600)), isNull); // 查询 1（文本「好进了」）在途
    // partial 更新：用户还在说话，文本变为「好进了这次进攻」。
    model.noteText(utteranceId: 'utt-1', text: '好进了这次进攻');
    // 查询 1 的结论此刻才回来——它判定的是旧文本「好进了」。
    model.acceptResult(utteranceId: 'utt-1', isComplete: true);
    // 旧文本的结论不得对已更新的文本生效：应按未决处理（null），带新
    // 文本重询。当前实现返回 true（按旧文本结论提前判完）→ 红。
    expect(model.call(ctx(700)), isNull,
        reason: '基于旧文本的在途结论不得污染更新后的文本');
  });

  test('丢弃迟到旧文本结论后按新文本重询', () async {
    final sent = <Map<String, dynamic>>[];
    final model = RemoteTurnModel(
      send: (message) {
        sent.add(message);
        return true;
      },
      resendThrottle: const Duration(milliseconds: 5),
      queryTimeout: const Duration(milliseconds: 10),
    );
    model.beginUtterance('utt-1');
    model.noteText(utteranceId: 'utt-1', text: '好进了');
    expect(model.call(ctx(600)), isNull); // 查询 1（旧文本）在途
    model.noteText(utteranceId: 'utt-1', text: '好进了这次进攻');
    model.acceptResult(utteranceId: 'utt-1', isComplete: true); // 旧文本结论
    // 越过超时与节流窗：结论应按未决处理，重询必须带新文本。
    await Future<void>.delayed(const Duration(milliseconds: 20));
    final verdict = model.call(ctx(700));
    expect(verdict, isNot(true),
        reason: '迟到于文本更新的旧结论不得作为判完依据');
    expect(sent.length, 2, reason: '旧结论作废后应带新文本重询');
    expect(sent.last['text'], '好进了这次进攻');
  });
}
