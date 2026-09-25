// 说完判定离线评估 runner（voice-turn-detection 1.2）。
//
// 对标注用例集（client/tool/turn_cases.dart）做参数扫描：静默阈值
// 800/1000/1400/1800ms 四档 + 语速自适应变体，输出各档提前截断率（该等
// 没等）/拖尾率（该断没断）与轮次延迟（说完点→判完点）统计表。
//
// 运行：cd client && dart run tool/turn_detection_eval.dart
// 结果供 openspec/changes/voice-turn-detection/design.md 引用。

import 'package:qiuqiu/services/turn_detector.dart';

import 'turn_cases.dart';

/// 判完余量：latency 超过「静默阈值+600ms」视为拖尾（600ms 覆盖帧量化与
/// 收尾抖动）。
const int tailGraceMs = 600;

class _CaseResult {
  final TurnCase testCase;
  final bool decided;
  final int latencyMs; // 说完点→判完点；未决为 -1

  /// 该档静默基础阈值（拖尾界 = 它 + 余量）。
  final int configBaseMs;

  const _CaseResult(
    this.testCase,
    this.decided,
    this.latencyMs,
    this.configBaseMs,
  );

  bool get early => decided && latencyMs < 0;
  bool get tail => !decided || latencyMs > configBaseMs + tailGraceMs;
  bool get correct => !early && !tail;
}

class _ConfigStats {
  String label = '';
  final List<_CaseResult> results = [];

  int earlyOf(String category) => results.where((r) => r.testCase.category.startsWith(category) && r.early).length;
  int tailOf(String category) => results.where((r) => r.testCase.category.startsWith(category) && r.tail).length;
  int get misjudged => results.where((r) => r.early || r.tail).length;
  double get misjudgeRate => misjudged / results.length;
  List<int> get correctLatencies =>
      results.where((r) => r.correct).map((r) => r.latencyMs).toList()..sort();
  List<int> get earlyLeads =>
      results.where((r) => r.early).map((r) => -r.latencyMs).toList()..sort();
}

int _percentile(List<int> sorted, double p) {
  if (sorted.isEmpty) return -1;
  final index = (p * sorted.length).ceil() - 1;
  return sorted[index.clamp(0, sorted.length - 1)];
}

void main() {
  final cases = turnEvalCases;
  final configs = <(String, TurnDetectionParams)>[
    (
      '800ms 固定',
      const TurnDetectionParams(
        strategy: TurnSilenceStrategy.tuned,
        tunedThreshold: Duration(milliseconds: 800),
      ),
    ),
    (
      '1000ms 固定',
      const TurnDetectionParams(
        strategy: TurnSilenceStrategy.tuned,
        tunedThreshold: Duration(milliseconds: 1000),
      ),
    ),
    (
      '1400ms 固定（基线）',
      const TurnDetectionParams(
        strategy: TurnSilenceStrategy.tuned,
        tunedThreshold: Duration(milliseconds: 1400),
      ),
    ),
    (
      '1800ms 固定',
      const TurnDetectionParams(
        strategy: TurnSilenceStrategy.tuned,
        tunedThreshold: Duration(milliseconds: 1800),
      ),
    ),
    (
      '语速自适应（基 800ms）',
      const TurnDetectionParams(
        strategy: TurnSilenceStrategy.tuned,
        tunedThreshold: Duration(milliseconds: 800),
        adaptive: true,
      ),
    ),
  ];

  final stats = <_ConfigStats>[];
  for (final (label, params) in configs) {
    final stat = _ConfigStats()..label = label;
    for (final testCase in cases) {
      final frames = testCase.synthRmsFrames();
      final observations =
          evaluateTurnDetection(frames, params: params);
      final decidedIndex =
          observations.indexWhere((observation) => observation.decided);
      final baseMs = params.baseSilenceThreshold.inMilliseconds;
      final result = _CaseResult(
        testCase,
        decidedIndex >= 0,
        decidedIndex >= 0
            ? (decidedIndex + 1) * turnFrameMs - testCase.expectedEndMs
            : -1,
        baseMs,
      );
      stat.results.add(result);
    }
    stats.add(stat);
  }

  _printOverallTable(stats, cases);
  _printCaseMatrix(stats, cases);
}

void _printOverallTable(List<_ConfigStats> stats, List<TurnCase> cases) {
  print('### 各档总体表（用例数 ${cases.length}，三类各 5 例）');
  print('');
  print('| 静默档 | A截断/拖尾 | B截断/拖尾 | C截断/拖尾 | 误判率 | 延迟p50 | 延迟p90 | 延迟均值 | 截断平均提前量 |');
  print('| --- | --- | --- | --- | --- | --- | --- | --- | --- |');
  for (final stat in stats) {
    final latencies = stat.correctLatencies;
    final mean = latencies.isEmpty
        ? -1
        : (latencies.reduce((a, b) => a + b) / latencies.length).round();
    final leads = stat.earlyLeads;
    final leadMean = leads.isEmpty
        ? -1
        : (leads.reduce((a, b) => a + b) / leads.length).round();
    print(
      '| ${stat.label} '
      '| ${stat.earlyOf('A')}/${stat.tailOf('A')} '
      '| ${stat.earlyOf('B')}/${stat.tailOf('B')} '
      '| ${stat.earlyOf('C')}/${stat.tailOf('C')} '
      '| ${(stat.misjudgeRate * 100).toStringAsFixed(1)}% '
      '| ${_percentile(latencies, 0.5)}ms '
      '| ${_percentile(latencies, 0.9)}ms '
      '| ${mean < 0 ? '—' : '${mean}ms'} '
      '| ${leadMean < 0 ? '—' : '${leadMean}ms'} |',
    );
  }
  print('');
}

void _printCaseMatrix(List<_ConfigStats> stats, List<TurnCase> cases) {
  print('### 逐例判完延迟矩阵（说完点→判完点，ms；负值=提前截断量；未决=—）');
  print('');
  final header = stats.map((stat) => stat.label).join(' | ');
  print('| 用例 | $header |');
  print('| --- | ${stats.map((_) => '---').join(' | ')} |');
  for (final testCase in cases) {
    final cells = <String>[];
    for (final stat in stats) {
      final result =
          stat.results.firstWhere((r) => r.testCase.id == testCase.id);
      if (!result.decided) {
        cells.add('—');
      } else {
        final mark = result.early ? '（截）' : (result.tail ? '（拖）' : '');
        cells.add('${result.latencyMs}$mark');
      }
    }
    print('| ${testCase.id} ${testCase.note} | ${cells.join(' | ')} |');
  }
  print('');
}
