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
}

bool resolvesMotionName(String name) {
  if (CompanionPresentation.motionVariants.containsKey(name)) return true;
  final legacy = CompanionPresentation.legacyMotionNames[name];
  if (legacy != null) {
    return CompanionPresentation.motionVariants.containsKey(legacy);
  }
  final alias = CompanionPresentation.motionAliases[name];
  if (alias != null) return resolvesMotionName(alias);
  return false;
}
