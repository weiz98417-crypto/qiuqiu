import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/presentation_state.dart';

/// Contract lock (ADR-0007 single source, three-way): the Dart constants in
/// services/presentation_state.dart, the
/// assets/live2d/models/qiuqiu/presentation-map.json tables, and the
/// female_01Arkit_6.model3.json model asset must agree exactly. Backend side
/// mirrored in backend/internal/relationship/presentation_vocabulary.go; the
/// JS surfaces fetch the same JSON at runtime (checked against it by
/// scripts/check-presentation-map.mjs in the offline tier).
void main() {
  // `flutter test` runs with the package root as the working directory.
  const mapDir = 'assets/live2d/models/qiuqiu';
  final model = jsonDecode(
    File(
      '$mapDir/female_01Arkit_6.model3.json',
    ).readAsStringSync(),
  ) as Map<String, dynamic>;
  final presentationMap = jsonDecode(
    File('$mapDir/presentation-map.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  final fileReferences = model['FileReferences'] as Map<String, dynamic>;
  final motionGroups = fileReferences['Motions'] as Map<String, dynamic>;
  final expressions = fileReferences['Expressions'] as List<dynamic>;
  final jsonExpressions =
      (presentationMap['expressions'] as Map).cast<String, dynamic>();
  final jsonMotions =
      (presentationMap['motions'] as Map).cast<String, dynamic>();
  final jsonPhases =
      (presentationMap['phases'] as Map).cast<String, dynamic>();
  // 模型 motion 组名（resolveMotionName 的合法落点之一——组名由渲染面挑
  // 组内变体；events/phases 行允许指到组名，如 var_overturn → "confused"
  // 经 motionAliases 落到 idle 组）。
  modelMotionGroups.addAll(motionGroups.keys.cast<String>());

  test('model ships the original 5-group / 9-motion inventory (live2d-motion-revert)', () {
    expect(motionGroups.keys, hasLength(5));
    final motionCount = motionGroups.values.fold<int>(
      0,
      (sum, group) => sum + (group as List).length,
    );
    expect(motionCount, 9);
    expect(expressions, hasLength(7));
  });

  test('Dart constants equal presentation-map.json exactly', () {
    // expressionIndices / motionVariants are the synchronous fallback mirror
    // of the JSON (see loadPresentationMap); any JSON edit must update them
    // in the same commit or this three-way lock fails.
    expect(
      CompanionPresentation.expressionIndices,
      jsonExpressions.map((key, value) => MapEntry(key, value as int)),
      reason: 'expressionIndices drifted from presentation-map.json',
    );
    expect(
      CompanionPresentation.motionVariants,
      jsonMotions.map(
        (key, value) => MapEntry(
          key,
          (
            (value as Map)['group'] as String,
            value['variant'] as int,
          ),
        ),
      ),
      reason: 'motionVariants drifted from presentation-map.json',
    );
  });

  test('runtime load derives the same tables as the asset JSON', () async {
    final loaded = await loadPresentationMap();
    expect(loaded.expressions, CompanionPresentation.expressionIndices);
    expect(loaded.motions, CompanionPresentation.motionVariants);
    expect(loaded.actsHoldLastFrame, CompanionPresentation.actsHoldLastFrame);
  });

  test('acts holdLastFrame slots lock three-way (live2d-engine-swap 6.3)', () {
    final jsonActs = (presentationMap['acts'] as Map).cast<String, dynamic>();
    final jsonHold = <String, List<String>>{
      for (final entry in jsonActs.entries)
        if ((entry.value as Map)['holdLastFrame'] is List &&
            ((entry.value as Map)['holdLastFrame'] as List).isNotEmpty)
          entry.key: [
            for (final quadrant
                in (entry.value as Map)['holdLastFrame'] as List<dynamic>)
              quadrant as String,
          ],
    };
    // Dart mirror == JSON acts section == PresentationMap.fallback.
    expect(
      CompanionPresentation.actsHoldLastFrame,
      jsonHold,
      reason: 'actsHoldLastFrame drifted from presentation-map.json',
    );
    expect(PresentationMap.fallback.actsHoldLastFrame, jsonHold);
    // Every named quadrant is a real key of its acts row, and the Go table
    // rows are locked to the same slots by
    // backend/internal/relationship/presentation_table_test.go
    // (TestHoldLastFrameRidesOnActReactRows).
    jsonHold.forEach((act, quadrants) {
      final row = jsonActs[act] as Map;
      expect(row.keys, containsAll(quadrants),
          reason: 'act "$act" names unknown quadrants $quadrants');
      expect(quadrants, isNotEmpty);
    });
  });

  test('CompanionPresentation parses the holdLastFrame wire flag', () {
    final holding = CompanionPresentation.fromReplyData(const {
      'presentation': {
        'expression': 'excited',
        'motion': 'celebrate',
        'voiceStyle': 'excited',
        'voiceEnergy': 0.9,
        'voiceSpeed': 1.05,
        'holdMs': 2600,
        'returnMode': 'watching',
        'holdLastFrame': true,
      },
    });
    expect(holding, isNotNull);
    expect(holding!.holdLastFrame, isTrue);

    // Legacy senders (and the ReturnMode decay apply, which re-enters
    // through the same parser without the flag) default to not holding: the
    // decay motion preempts and clears a held frame.
    final plain = CompanionPresentation.fromReplyData(const {
      'presentation': {
        'expression': 'focus',
        'motion': 'focus',
        'voiceStyle': 'natural',
        'voiceEnergy': 0.5,
        'voiceSpeed': 1,
        'holdMs': 1800,
        'returnMode': 'decay_to_focus',
      },
    });
    expect(plain, isNotNull);
    expect(plain!.holdLastFrame, isFalse);
  });

  test('every whitelisted motion resolves into presentation-map.json', () {
    expect(jsonMotions, isNotEmpty);
    expect(jsonExpressions, isNotEmpty);
    bool resolves(String name) {
      if (CompanionPresentation.motionVariants.containsKey(name)) return true;
      // Model group names: the surfaces pick among the group's variants.
      if (motionGroups.containsKey(name)) return true;
      final legacy = CompanionPresentation.legacyMotionNames[name];
      if (legacy != null) {
        return CompanionPresentation.motionVariants.containsKey(legacy);
      }
      final alias = CompanionPresentation.motionAliases[name];
      return alias != null && resolves(alias);
    }

    for (final name in CompanionPresentation.allowedMotions) {
      expect(
        resolves(name),
        isTrue,
        reason: 'whitelisted motion "$name" has no route into the JSON',
      );
    }
  });

  test('every JSON motion resolves into the model3.json', () {
    for (final entry in CompanionPresentation.motionVariants.entries) {
      final group = motionGroups[entry.value.$1];
      expect(
        group,
        isA<List<dynamic>>(),
        reason: 'motion "${entry.key}" targets unknown group "${entry.value.$1}"',
      );
      expect(
        entry.value.$2,
        lessThan((group as List).length),
        reason: 'motion "${entry.key}" variant index out of range',
      );
    }
  });

  test('every motion of the model is reachable from the JSON tables', () {
    final covered = <String>{
      for (final variant in CompanionPresentation.motionVariants.values)
        '${variant.$1}:${variant.$2}',
    };
    motionGroups.forEach((group, motions) {
      for (var index = 0; index < (motions as List).length; index++) {
        expect(
          covered,
          contains('$group:$index'),
          reason: 'model motion $group[$index] is not routed by the JSON',
        );
      }
    });
  });

  test('motion aliases resolve into the whitelist', () {
    for (final target in CompanionPresentation.motionAliases.values) {
      expect(
        CompanionPresentation.allowedMotions,
        contains(target),
        reason: 'alias target "$target" is not whitelisted',
      );
    }
  });

  test('legacy synthetic motions resolve into the JSON tables', () {
    for (final target in CompanionPresentation.legacyMotionNames.values) {
      expect(
        CompanionPresentation.motionVariants.keys,
        contains(target),
        reason: 'legacy motion target "$target" is not in the JSON motions',
      );
    }
  });

  test('every whitelisted expression maps into the model expressions', () {
    for (final name in CompanionPresentation.allowedExpressions) {
      final index = CompanionPresentation.expressionIndices[name];
      expect(
        index,
        isNotNull,
        reason: 'expression "$name" has no expressionIndices entry',
      );
      expect(
        index!,
        lessThan(expressions.length),
        reason: 'expression "$name" index out of range',
      );
    }
  });

  test('non-neutral expressions never bind the empty expression file', () {
    // Expression file 0 renders no face (design contract test 3).
    const neutral = {'focus', 'idle', 'listening'};
    CompanionPresentation.expressionIndices.forEach((name, index) {
      if (index == 0) {
        expect(
          neutral,
          contains(name),
          reason: 'expression "$name" binds the empty expression file',
        );
      }
    });
    expect(
      CompanionPresentation.expressionIndices['thinking'],
      3,
      reason: 'thinking must not bind expression file 0 (the empty expression)',
    );
  });

  test('expression aliases resolve into the whitelist', () {
    for (final target in CompanionPresentation.expressionAliases.values) {
      expect(
        CompanionPresentation.allowedExpressions,
        contains(target),
        reason: 'alias target "$target" is not whitelisted',
      );
    }
  });

  test('phase rows resolve through the JSON into whitelisted bodies', () {
    final map = PresentationMap.fromJson(presentationMap);
    expect(map.phases, jsonPhases);
    const phaseRows = {
      'user_speaking': ('listening', 'listen_01'),
      'understanding': ('thinking', 'think'),
      'qiuqiu_speaking': ('chat', 'speak_01'),
      'session_open': ('happy', 'hello'),
      'match_end': ('happy', 'wave'),
    };
    phaseRows.forEach((phase, expected) {
      final performance = map.performanceFor(phase);
      expect(
        performance,
        expected,
        reason: 'phase "$phase" drifted from presentation-map.json',
      );
      expect(
        CompanionPresentation.allowedExpressions,
        contains(performance!.$1),
      );
      expect(resolvesMotionName(performance.$2), isTrue);
    });
    // phases.idle stays owned by the C4 idle tier picker.
    expect(
      map.performanceFor('idle'),
      isNull,
      reason: 'phases.idle is the affect-idle-tier marker, not a performance',
    );
  });

  test('events rows lock three-way and resolve into whitelisted bodies', () {
    final map = PresentationMap.fromJson(presentationMap);
    final jsonEvents =
        (presentationMap['events'] as Map).cast<String, dynamic>();
    // Dart fallback mirror == JSON events == (Go rows locked on the backend
    // side by presentation_table_test.go
    // TestPresentationTableMirrorsJSONActsAndEvents).
    expect(
      PresentationMap.fallback.events,
      jsonEvents,
      reason: 'PresentationMap.fallback.events drifted from presentation-map.json',
    );
    expect(
      map.events,
      jsonEvents,
      reason: 'PresentationMap.events parse drifted from presentation-map.json',
    );
    // wake-word-kws 10.3: the client-origin wake row must exist so the idle
    // wake-up render has a single-source body.
    expect(
      jsonEvents,
      contains(PresentationMap.wakeEventClass),
      reason: 'events.wake (client-origin wake row) is missing',
    );
    jsonEvents.forEach((name, raw) {
      final performance = map.eventFor(name);
      expect(performance, isNotNull,
          reason: 'event "$name" is not an "expression/motion" row');
      // Expression may be a legacy alias (events.var_check "tense" →
      // nervous); it must at least normalize into the whitelist.
      expect(
        CompanionPresentation.normalizeExpression(performance!.$1),
        isNotNull,
        reason: 'event "$name" targets unknown expression "${performance.$1}"',
      );
      expect(
        resolvesMotionName(performance.$2),
        isTrue,
        reason: 'event "$name" targets unknown motion "${performance.$2}"',
      );
    });
    // The wake row renders 球球抬头看你: happy face, leaning-in listen pose.
    expect(map.eventFor(PresentationMap.wakeEventClass), ('happy', 'listen_01'));
  });
}

bool resolvesMotionName(String name) {
  if (CompanionPresentation.motionVariants.containsKey(name)) return true;
  // Model group names: the surfaces pick among the group's variants.
  if (modelMotionGroups.contains(name)) return true;
  final legacy = CompanionPresentation.legacyMotionNames[name];
  if (legacy != null) {
    return CompanionPresentation.motionVariants.containsKey(legacy);
  }
  final alias = CompanionPresentation.motionAliases[name];
  if (alias != null) return resolvesMotionName(alias);
  return false;
}

/// Filled at main() start from the model3.json motion groups.
final Set<String> modelMotionGroups = {};
