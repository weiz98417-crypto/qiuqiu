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
}
