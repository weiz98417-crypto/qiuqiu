import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/turn_detector.dart';
import 'package:qiuqiu/services/vad_service.dart';

import '../tool/turn_cases.dart';

TurnDetectionParams tuned(int ms) => TurnDetectionParams(
      strategy: TurnSilenceStrategy.tuned,
      tunedThreshold: Duration(milliseconds: ms),
    );

void main() {
  // 默认帧 50ms：1400ms = 28 个静默帧；800ms = 16 个静默帧。
  group('能量阈值单一来源', () {
    test('turn_detector 阈值与 VoiceActivityGate 同值，不漂移', () {
      expect(turnStartRmsThreshold,
          VoiceActivityGate.defaultStartThreshold);
      expect(turnContinueRmsThreshold,
          VoiceActivityGate.defaultContinueThreshold);
      expect(
        const TurnDetectionParams().fallbackThreshold,
        const Duration(milliseconds: 1400),
      );
    });
  });

  group('静默档判定', () {
    test('默认固定档与历史行为等价：连续静默 27 帧不判，第 28 帧判完', () {
      final chain = TurnDetectionChain();
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03); // 语音确认
      }
      for (var i = 0; i < 27; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      expect(chain.observe(0.001).decided, isTrue);
    });

    test('调参档阈值参数化：800ms 档 16 帧判完', () {
      final chain = TurnDetectionChain(params: tuned(800));
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      for (var i = 0; i < 15; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      final observation = chain.observe(0.001);
      expect(observation.decided, isTrue);
      expect(observation.stage, TurnDetectionStage.silence);
    });

    test('高于 continue 阈值的帧重置静默累计（拖尾/噪声不计静默）', () {
      final chain = TurnDetectionChain(params: tuned(800));
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      for (var i = 0; i < 15; i++) {
        chain.observe(0.001);
      }
      chain.observe(0.009); // 歧义带帧：静默清零
      for (var i = 0; i < 15; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      expect(chain.observe(0.001).decided, isTrue);
    });

    test('语速自适应：被吸收的犹豫小停顿抬高生效阈值（800→1000）', () {
      final chain = TurnDetectionChain(
        params: const TurnDetectionParams(
          strategy: TurnSilenceStrategy.tuned,
          tunedThreshold: Duration(milliseconds: 800),
          adaptive: true,
        ),
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      // 600ms 犹豫停顿：未达 800ms 被吸收。
      for (var i = 0; i < 12; i++) {
        chain.observe(0.002);
      }
      chain.observe(0.03); // 语音恢复，犹豫计数 +1 → 生效阈值 1000ms
      expect(chain.effectiveSilenceThreshold,
          const Duration(milliseconds: 1000));
      // 末尾静默 19 帧不判，第 20 帧（1000ms）判完。
      for (var i = 0; i < 19; i++) {
        expect(chain.observe(0.002).decided, isFalse);
      }
      expect(chain.observe(0.002).decided, isTrue);
    });

    test('语速自适应封顶：犹豫再多不超过 adaptiveCap', () {
      final chain = TurnDetectionChain(
        params: const TurnDetectionParams(
          strategy: TurnSilenceStrategy.tuned,
          tunedThreshold: Duration(milliseconds: 800),
          adaptive: true,
          adaptiveCap: Duration(milliseconds: 2000),
        ),
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      // 8 次犹豫：800×(1+0.25×8)=2400 → 封顶 2000。
      for (var h = 0; h < 8; h++) {
        for (var i = 0; i < 4; i++) {
          chain.observe(0.002); // 200ms 犹豫
        }
        chain.observe(0.03);
      }
      expect(chain.effectiveSilenceThreshold,
          const Duration(milliseconds: 2000));
    });
  });

  group('降级链插槽', () {
    test('模型阶段给出结论即采纳，阶段标记为 model', () {
      final observations = evaluateTurnDetection(
        [...List.filled(2, 0.03), ...List.filled(28, 0.001)],
        params: tuned(1400),
        modelStage: (context) => true,
      );
      expect(observations.last.stage, TurnDetectionStage.model);
      expect(observations.last.decided, isTrue);
    });

    test('模型否决（false）后不判，再累计满一段生效阈值后二次询问', () {
      var calls = 0;
      final chain = TurnDetectionChain(
        params: tuned(800),
        modelStage: (context) {
          calls++;
          return calls == 1 ? false : null;
        },
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      // 第 16 帧静默：模型首次被问，否决 → 不判。
      for (var i = 0; i < 16; i++) {
        chain.observe(0.001);
      }
      expect(calls, 1);
      expect(chain.decided, isFalse);
      // 再累计 16 帧（又一段 800ms）：二次询问返回 null → 静默档兜底判完。
      for (var i = 0; i < 15; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      final observation = chain.observe(0.001);
      expect(observation.decided, isTrue);
      expect(observation.stage, TurnDetectionStage.silence);
      expect(calls, 2);
    });

    test('模型未决下探规则阶段，规则结论采纳并标记 completenessRule', () {
      final observations = evaluateTurnDetection(
        [...List.filled(2, 0.03), ...List.filled(28, 0.001)],
        params: tuned(1400),
        modelStage: (context) => null,
        ruleStage: (context) => true,
      );
      expect(observations.last.decided, isTrue);
      expect(observations.last.stage, TurnDetectionStage.completenessRule);
    });

    test('全部插槽未接入（null）时静默档兜底，行为与默认一致', () {
      final observations = evaluateTurnDetection(
        [...List.filled(2, 0.03), ...List.filled(28, 0.001)],
      );
      expect(observations.last.decided, isTrue);
      expect(observations.last.stage, TurnDetectionStage.silence);
    });

    test('tuned 关回退：插槽未决时链停在未判，不兜底 fire', () {
      final chain = TurnDetectionChain(
        params: const TurnDetectionParams(
          strategy: TurnSilenceStrategy.tuned,
          tunedThreshold: Duration(milliseconds: 800),
          fallbackEnabled: false,
        ),
        modelStage: (context) => null,
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03); // 语音确认
      }
      // 远超 800ms 生效阈值仍未判：关回退后交给上层超时，不再下探静默档。
      for (var i = 0; i < 40; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      expect(chain.decided, isFalse);
    });

    test('fixed 照常判完：关回退对固定档无影响', () {
      final chain = TurnDetectionChain(
        params: const TurnDetectionParams(fallbackEnabled: false),
        modelStage: (context) => null,
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03); // 语音确认
      }
      for (var i = 0; i < 27; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      expect(chain.observe(0.001).decided, isTrue);
      expect(chain.decided, isTrue);
    });
  });

  group('标注用例锁（与评估 harness 同源）', () {
    test('A3 句中停顿 900ms 在 1400ms 档被吸收，不提前截断', () {
      final a3 = turnEvalCases.firstWhere((c) => c.id == 'A3');
      final observations = evaluateTurnDetection(
        a3.synthRmsFrames(),
        params: tuned(1400),
      );
      final decidedIndex =
          observations.indexWhere((observation) => observation.decided);
      expect(decidedIndex, greaterThanOrEqualTo(0));
      expect(
        (decidedIndex + 1) * turnFrameMs,
        greaterThanOrEqualTo(a3.expectedEndMs),
      );
    });

    test('A3 同一停顿在 800ms 档被提前截断（评估结论复现）', () {
      final a3 = turnEvalCases.firstWhere((c) => c.id == 'A3');
      final observations = evaluateTurnDetection(
        a3.synthRmsFrames(),
        params: tuned(800),
      );
      final decidedIndex =
          observations.indexWhere((observation) => observation.decided);
      expect(
        (decidedIndex + 1) * turnFrameMs,
        lessThan(a3.expectedEndMs),
      );
    });

    test('C3 长尾噪声在 1800ms 档直到录音结束都无法判完', () {
      final c3 = turnEvalCases.firstWhere((c) => c.id == 'C3');
      final observations = evaluateTurnDetection(
        c3.synthRmsFrames(),
        params: tuned(1800),
      );
      expect(
        observations.indexWhere((observation) => observation.decided),
        -1,
      );
    });
  });

  group('决策 c 接线：model 插槽提前问', () {
    test('默认链不受影响：model 只在静默阈值帧被咨询', () {
      var calls = 0;
      final chain = TurnDetectionChain(
        params: tuned(1400),
        modelStage: (context) {
          calls++;
          return null;
        },
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      // 前 27 帧（1350ms < 1400ms）：不咨询。
      for (var i = 0; i < 27; i++) {
        chain.observe(0.001);
      }
      expect(calls, 0);
      // 第 28 帧（阈值）：首次咨询。
      chain.observe(0.001);
      expect(calls, 1);
    });

    test('提前问 true 提前判完：赶在静默阈值之前', () {
      // 在途（null）→ 结果回填（true）→ 下一帧采纳，650ms 判完 < 1400ms。
      bool? verdict;
      final chain = TurnDetectionChain(
        params: tuned(1400),
        modelStage: (context) => verdict,
        modelEarlyQueryAfter: const Duration(
          milliseconds: turnModelEarlyQueryMs,
        ),
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      for (var i = 0; i < 11; i++) {
        expect(chain.observe(0.001).decided, isFalse); // 550ms
      }
      expect(chain.observe(0.001).decided, isFalse); // 600ms 首询，在途
      verdict = true; // turn_result 回填
      final observation = chain.observe(0.001); // 650ms 消费结论
      expect(observation.decided, isTrue);
      expect(observation.stage, TurnDetectionStage.model);
    });

    test('提前问 false 否决：压制静默档至否决过期，过期帧按未决下探', () {
      bool? verdict;
      final chain = TurnDetectionChain(
        params: tuned(800),
        modelStage: (context) => verdict,
        modelEarlyQueryAfter: const Duration(milliseconds: 600),
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      verdict = null;
      for (var i = 0; i < 11; i++) {
        expect(chain.observe(0.001).decided, isFalse); // 550ms
      }
      verdict = false;
      expect(chain.observe(0.001).decided, isFalse); // 600ms 首询否决
      // 否决压制期（到 600+800=1400ms）静默档不得兜底。
      verdict = null;
      for (var i = 0; i < 15; i++) {
        expect(chain.observe(0.001).decided, isFalse); // 650→1350ms
      }
      // 1400ms：否决恰好过期，二次咨询返回 null → 下探静默档判完。
      final observation = chain.observe(0.001);
      expect(observation.decided, isTrue);
      expect(observation.stage, TurnDetectionStage.silence);
    });

    test('模型持续否决（文本不变）链不判完：等待用户继续或上层超时', () {
      // 远程模型对同文本重复返回 false（RemoteTurnModel 结论在文本不变时
      // 持续有效）：链每个否决周期持续等待，静默档不得兜底抢判；最终由
      // 用户继续说话（静默清零）或 maxDuration 兜底收口。
      final chain = TurnDetectionChain(
        params: tuned(800),
        modelStage: (context) => false,
        modelEarlyQueryAfter: const Duration(milliseconds: 600),
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      for (var i = 0; i < 40; i++) {
        expect(chain.observe(0.001).decided, isFalse); // 2000ms 仍未判
      }
      expect(chain.decided, isFalse);
      // 用户继续说话：静默清零，否决解除。
      chain.observe(0.03);
      expect(chain.vetoActive, isFalse);
    });

    test('提前问 null 下探：静默档兜底判完（降级链闭环）', () {
      final chain = TurnDetectionChain(
        params: tuned(1400),
        modelStage: (context) => null,
        modelEarlyQueryAfter: const Duration(milliseconds: 600),
      );
      for (var i = 0; i < 2; i++) {
        chain.observe(0.03);
      }
      for (var i = 0; i < 27; i++) {
        expect(chain.observe(0.001).decided, isFalse);
      }
      final observation = chain.observe(0.001);
      expect(observation.decided, isTrue);
      expect(observation.stage, TurnDetectionStage.silence);
    });
  });

  group('RemoteTurnModel', () {
    TurnDecisionContext ctx(int silenceMs) => TurnDecisionContext(
          utteranceDuration: const Duration(seconds: 2),
          trailingSilence: Duration(milliseconds: silenceMs),
        );

    test('静默 600ms 起查：低于门槛不查，达标即查且单飞', () {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(send: (message) {
        sent.add(message);
        return true;
      });
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '我觉得裁判这次吹得');
      expect(model.call(ctx(550)), isNull);
      expect(sent, isEmpty);
      expect(model.call(ctx(600)), isNull);
      expect(sent.length, 1);
      expect(sent.first['type'], 'turn_query');
      expect(sent.first['utteranceId'], 'utt-1');
      expect(sent.first['text'], '我觉得裁判这次吹得');
      // 在途单飞：不重发。
      expect(model.call(ctx(700)), isNull);
      expect(sent.length, 1);
    });

    test('无转写文本不查询（null 下探静默档）', () {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(send: (message) {
        sent.add(message);
        return true;
      });
      model.beginUtterance('utt-1');
      expect(model.call(ctx(900)), isNull);
      expect(sent, isEmpty);
    });

    test('结论回填：文本不变期间持续有效；错话轮的迟到结果丢弃', () {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(send: (message) {
        sent.add(message);
        return true;
      });
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull);
      // 错话轮结果：不回填。
      model.acceptResult(utteranceId: 'utt-2', isComplete: true);
      expect(model.call(ctx(700)), isNull);
      // 本话轮结果：回填后文本不变期间重复咨询取同一结论，不重发请求。
      model.acceptResult(utteranceId: 'utt-1', isComplete: false);
      expect(model.call(ctx(700)), isFalse);
      expect(model.call(ctx(1200)), isFalse);
      expect(sent.length, 1);
    });

    test('null 结论（服务端未配置/失败）话轮内粘滞不再询', () {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(send: (message) {
        sent.add(message);
        return true;
      });
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull);
      model.acceptResult(utteranceId: 'utt-1', isComplete: null);
      // 粘滞：越过节流窗也只回 null，不重发。
      expect(model.call(ctx(1200)), isNull);
      expect(sent.length, 1);
    });

    test('发送失败（WS 不可达）话轮内粘滞', () {
      var sendCalls = 0;
      final model = RemoteTurnModel(
        send: (message) {
          sendCalls++;
          return false;
        },
      );
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull);
      expect(model.call(ctx(1200)), isNull);
      expect(sendCalls, 1);
    });

    test('在途超时按未决处理：越过节流窗后可再询', () async {
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
      expect(model.call(ctx(600)), isNull);
      await Future<void>.delayed(const Duration(milliseconds: 30));
      // 超时未回：pending 清空，重发（结果从未到达，不粘滞）。
      expect(model.call(ctx(700)), isNull);
      expect(sent.length, 2);
    });

    test('连续否决超限后放弃远程判定（防误判把话轮无限挂起）', () {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(send: (message) {
        sent.add(message);
        return true;
      });
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull); // 查询 1
      model.acceptResult(utteranceId: 'utt-1', isComplete: false);
      expect(model.call(ctx(700)), isFalse); // 否决 1
      expect(model.call(ctx(2100)), isFalse); // 否决 2
      // 第 3 次咨询：连续否决超限，null 下探静默档，且不重发请求。
      expect(model.call(ctx(3500)), isNull);
      expect(sent.length, 1);
    });

    test('partial 文本更新作废未消费结论，重询带新文本', () async {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(
        send: (message) {
          sent.add(message);
          return true;
        },
        resendThrottle: const Duration(milliseconds: 5),
      );
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull);
      model.acceptResult(utteranceId: 'utt-1', isComplete: true);
      // 文本未变：结论仍有效，直接消费。
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isTrue);
      // 文本变了：旧结论作废，越过节流窗后带新文本重询。
      model.noteText(utteranceId: 'utt-1', text: '好进了这次进攻');
      await Future<void>.delayed(const Duration(milliseconds: 10));
      expect(model.call(ctx(800)), isNull);
      expect(sent.length, 2);
      expect(sent.last['text'], '好进了这次进攻');
    });

    test('beginUtterance 绑定新话轮即清在途状态', () {
      final sent = <Map<String, dynamic>>[];
      final model = RemoteTurnModel(send: (message) {
        sent.add(message);
        return true;
      });
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull);
      model.beginUtterance('utt-2');
      // 旧话轮文本与在途查询全部清零：新话轮无文本不查。
      expect(model.call(ctx(900)), isNull);
      expect(sent.length, 1);
      model.noteText(utteranceId: 'utt-2', text: '下半场刚开始');
      expect(model.call(ctx(600)), isNull);
      expect(sent.last['utteranceId'], 'utt-2');
    });

    test('dispose 取消在途超时定时器不抛异常', () {
      final model = RemoteTurnModel(send: (message) => true);
      model.beginUtterance('utt-1');
      model.noteText(utteranceId: 'utt-1', text: '好进了');
      expect(model.call(ctx(600)), isNull);
      expect(model.dispose, returnsNormally);
    });
  });
}
