import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/presentation_state.dart';

/// Contract lock (backend side mirrored in
/// backend/internal/relationship/presentation_vocabulary.go): everything the
/// client whitelists must exist in the actual Live2D model asset, and the
/// model's full motion inventory (12 groups / 17 motions) must be reachable.
void main() {
  // `flutter test` runs with the package root as the working directory.
  final model = jsonDecode(
    File(
      'assets/live2d/models/qiuqiu/female_01Arkit_6.model3.json',
    ).readAsStringSync(),
  ) as Map<String, dynamic>;
  final fileReferences = model['FileReferences'] as Map<String, dynamic>;
  final motionGroups = fileReferences['Motions'] as Map<String, dynamic>;
  final expressions = fileReferences['Expressions'] as List<dynamic>;

  test('model ships the full 12-group / 17-motion inventory', () {
    expect(motionGroups.keys, hasLength(12));
    final motionCount = motionGroups.values.fold<int>(
      0,
      (sum, group) => sum + (group as List).length,
    );
    expect(motionCount, 17);
    expect(expressions, hasLength(7));
  });

  test('every whitelisted motion resolves into the model3.json', () {
    expect(
      CompanionPresentation.motionVariants.keys.toSet(),
      CompanionPresentation.allowedMotions,
      reason: 'whitelist and motionVariants must stay in lockstep',
    );
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

  test('every motion of the model is reachable from the whitelist', () {
    final covered = <String>{
      for (final variant in CompanionPresentation.motionVariants.values)
        '${variant.$1}:${variant.$2}',
    };
    motionGroups.forEach((group, motions) {
      for (var index = 0; index < (motions as List).length; index++) {
        expect(
          covered,
          contains('$group:$index'),
          reason: 'model motion $group[$index] is not whitelisted',
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

  test('expression aliases resolve into the whitelist', () {
    for (final target in CompanionPresentation.expressionAliases.values) {
      expect(
        CompanionPresentation.allowedExpressions,
        contains(target),
        reason: 'alias target "$target" is not whitelisted',
      );
    }
  });
}
