// 说完判定评估用例集（voice-turn-detection 1.2 离线评估 harness）。
//
// 三类观赛真实场景的确定性合成：给定「分段 RMS 序列 + 期望说完点」的标注
// 用例，供参数扫描对比不同静默档的提前截断率/拖尾率与轮次延迟。帧长 50ms
// （与采集帧节奏一致），帧 RMS 取帧中点所在分段的标注电平。纯 Dart，无
// flutter 依赖，可被 flutter 测试与 `dart run` 评估脚本共同引用。

import 'package:qiuqiu/services/turn_detector.dart';

/// 分段类型：speech=标注用户语音（期望说完点按最后一段 speech 收口）；
/// silence=真静默（低于 continue 阈值）；noise=环境噪声/拖音（处于
/// continue 与 start 之间的歧义带，现行为按「语音继续」处理）；burst=
/// 高于 start 阈值的脉冲（人群欢呼/背景人声串入）。
enum TurnSegmentKind { speech, silence, noise, burst }

class TurnCaseSegment {
  final TurnSegmentKind kind;
  final int durationMs;
  final double rms;

  const TurnCaseSegment(this.kind, this.durationMs, this.rms);
}

class TurnCase {
  final String id;

  /// 类别：A=句中停顿，B=正常句尾收束，C=环境噪声/长尾拖音。
  final String category;

  /// 台词/场景说明（标注依据）。
  final String note;
  final List<TurnCaseSegment> segments;

  const TurnCase(this.id, this.category, this.note, this.segments);

  /// 期望说完点（毫秒）：最后一段标注语音的收口时刻。
  int get expectedEndMs {
    var end = 0;
    var latestSpeechEnd = -1;
    for (final segment in segments) {
      end += segment.durationMs;
      if (segment.kind == TurnSegmentKind.speech) {
        latestSpeechEnd = end;
      }
    }
    return latestSpeechEnd;
  }

  /// 合成逐帧 RMS 序列（帧中点采样）。
  List<double> synthRmsFrames() {
    final frames = <double>[];
    var elapsed = 0;
    for (final segment in segments) {
      final frameCount = segment.durationMs ~/ turnFrameMs;
      for (var i = 0; i < frameCount; i++) {
        frames.add(segment.rms);
      }
      elapsed += segment.durationMs;
    }
    assert(
      elapsed ~/ turnFrameMs == frames.length,
      '分段时长必须是 50ms 整数倍',
    );
    return frames;
  }
}

const _speechLoud = 0.04;
const _speechMid = 0.03;
const _speechSoft = 0.022;
const _quiet = 0.002;
const _tailNoise = 0.009;
const _humNoise = 0.007;
const _tvNoise = 0.012;

/// 句中停顿：用户停顿思考后继续（「那个进球……嗯怎么说呢」形态）。
/// 期望说完点在第二段语音收口——停顿期间判完即提前截断（球球抢答）。
const List<TurnCase> kSentencePauseCases = [
  TurnCase('A1', 'A句中停顿', '那个进球，[600ms]回放里看得很清楚。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1200, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 600, _quiet),
    TurnCaseSegment(TurnSegmentKind.speech, 1500, _speechMid),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('A2', 'A句中停顿', '这球……[800ms]门将应该要负责的。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1000, _speechMid),
    TurnCaseSegment(TurnSegmentKind.silence, 800, _quiet),
    TurnCaseSegment(TurnSegmentKind.speech, 1200, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('A3', 'A句中停顿', '那个进球……[900ms]嗯怎么说呢，越位在先。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1500, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 900, _quiet),
    TurnCaseSegment(TurnSegmentKind.speech, 1000, _speechSoft),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('A4', 'A句中停顿', '我觉得……[1000ms]裁判这次吹得没问题。', [
    TurnCaseSegment(TurnSegmentKind.speech, 900, _speechMid),
    TurnCaseSegment(TurnSegmentKind.silence, 1000, _quiet),
    TurnCaseSegment(TurnSegmentKind.speech, 1600, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('A5', 'A句中停顿', '下半场刚开始……[1200ms]对，就是那次反击。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1200, _speechSoft),
    TurnCaseSegment(TurnSegmentKind.silence, 1200, _quiet),
    TurnCaseSegment(TurnSegmentKind.speech, 900, _speechMid),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
];

/// 正常句尾收束：一次成句自然收尾，判完点越贴近说完点越好。
const List<TurnCase> kSentenceEndCases = [
  TurnCase('B1', 'B正常句尾', '这波进攻配合打得真漂亮。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1800, _speechMid),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('B2', 'B正常句尾', '主教练下半场的换人调整直接改变了比赛节奏。', [
    TurnCaseSegment(TurnSegmentKind.speech, 2400, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('B3', 'B正常句尾', '好，进了！', [
    TurnCaseSegment(TurnSegmentKind.speech, 900, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('B4', 'B正常句尾', '这脚……这脚射门太可惜了。（短促重复不算犹豫长停顿）', [
    TurnCaseSegment(TurnSegmentKind.speech, 600, _speechMid),
    TurnCaseSegment(TurnSegmentKind.silence, 300, _quiet),
    TurnCaseSegment(TurnSegmentKind.speech, 900, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('B5', 'B正常句尾', '角球开出来，前点一蹭，后点包抄推射入网！（能量起伏）', [
    TurnCaseSegment(TurnSegmentKind.speech, 700, _speechMid),
    TurnCaseSegment(TurnSegmentKind.speech, 700, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.speech, 700, _speechSoft),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
];

/// 环境噪声/长尾拖音：说完点之后是歧义带能量（噪声/拖音/串入声），
/// 能量线在这些帧上分不清「还没说完」——拖尾率由此而来。
const List<TurnCase> kNoiseTailCases = [
  TurnCase('C1', 'C噪声拖尾', '说完后 800ms 麦克风拖音尾。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1500, _speechMid),
    TurnCaseSegment(TurnSegmentKind.noise, 800, _tailNoise),
    TurnCaseSegment(TurnSegmentKind.silence, 2500, _quiet),
  ]),
  TurnCase('C2', 'C噪声拖尾', '说完后 1.5s 电视伴音串入。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1200, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.noise, 1500, _tvNoise),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('C3', 'C噪声拖尾', '说完后 2.5s 观赛环境低鸣长尾。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1800, _speechLoud),
    TurnCaseSegment(TurnSegmentKind.noise, 2500, _humNoise),
    TurnCaseSegment(TurnSegmentKind.silence, 1500, _quiet),
  ]),
  TurnCase('C4', 'C噪声拖尾', '说完后人群欢呼：短脉冲高于 start 阈值+歧义带拖尾。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1000, _speechMid),
    TurnCaseSegment(TurnSegmentKind.burst, 300, 0.02),
    TurnCaseSegment(TurnSegmentKind.noise, 1200, 0.01),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
  TurnCase('C5', 'C噪声拖尾', '说完后背景人声串入：歧义带+短促人声+歧义带。', [
    TurnCaseSegment(TurnSegmentKind.speech, 1400, _speechMid),
    TurnCaseSegment(TurnSegmentKind.noise, 1000, 0.008),
    TurnCaseSegment(TurnSegmentKind.burst, 300, 0.025),
    TurnCaseSegment(TurnSegmentKind.noise, 600, 0.008),
    TurnCaseSegment(TurnSegmentKind.silence, 2000, _quiet),
  ]),
];

/// 全部标注用例：三类各 5 例，共 15 例。
List<TurnCase> get turnEvalCases => [
      ...kSentencePauseCases,
      ...kSentenceEndCases,
      ...kNoiseTailCases,
    ];
