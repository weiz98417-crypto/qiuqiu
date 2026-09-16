import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/reply_display.dart';

void main() {
  test('rejects unknown presentation commands', () {
    expect(
      CompanionPresentation.fromReplyData({
        'presentation': {
          'expression': 'javascript:alert',
          'motion': 'cheer',
        },
      }),
      isNull,
    );
  });

  test('legacy expressions use the same safety whitelist', () {
    expect(CompanionPresentation.normalizeExpression('excited'), 'excited');
    expect(CompanionPresentation.normalizeExpression('deflated'), 'sad');
    expect(
        CompanionPresentation.normalizeExpression('javascript:alert'), isNull);
  });

  test('maps supported backend presentation aliases safely', () {
    final presentation = CompanionPresentation.fromReplyData({
      'presentation': {
        'expression': 'deflated',
        'motion': 'settle',
        'voiceStyle': 'low_disappointed',
        'returnMode': 'decay_to_focus',
      },
    });
    expect(presentation?.expression, 'sad');
    expect(presentation?.motion, 'idle');
  });

  test('nod alias resolves into the agree motion', () {
    final presentation = CompanionPresentation.fromReplyData({
      'presentation': {
        'expression': 'happy',
        'motion': 'nod',
        'voiceStyle': 'warm',
        'returnMode': 'decay_to_focus',
      },
    });
    expect(presentation?.motion, 'agree');
  });

  test('accepts the full motion pack and still rejects unknowns', () {
    for (final motion in [
      'celebrate',
      'celebrate_02',
      'miss',
      'complain',
      'analysis',
      'tense',
      'agree',
      'wave',
      'idle_02',
      'speak_02',
    ]) {
      final presentation = CompanionPresentation.fromReplyData({
        'presentation': {
          'expression': 'chat',
          'motion': motion,
          'voiceStyle': 'natural',
          'returnMode': 'decay_to_focus',
        },
      });
      expect(presentation?.motion, motion, reason: motion);
    }
    expect(
      CompanionPresentation.fromReplyData({
        'presentation': {
          'expression': 'chat',
          'motion': 'dab',
          'voiceStyle': 'natural',
          'returnMode': 'decay_to_focus',
        },
      }),
      isNull,
    );
  });

  test('presentation parses the affect vector for idle tiers', () {
    final presentation = CompanionPresentation.fromReplyData({
      'presentation': {
        'expression': 'deflated',
        'motion': 'complain',
        'voiceStyle': 'low_disappointed',
        'returnMode': 'decay_to_idle',
        'affect': {'valence': -0.7, 'arousal': 0.3},
      },
    });
    expect(presentation?.valence, -0.7);
    expect(presentation?.arousal, 0.3);
  });

  test('single sentence reply does not invent companion filler', () {
    expect(splitReplyForDisplay('这脚真离谱。'), ('这脚真离谱。', ''));
  });

  test('multi sentence reply keeps the actual second layer', () {
    expect(
      splitReplyForDisplay('还是越了。白喊。'),
      ('还是越了。', '白喊。'),
    );
  });

  test('reply presentation parses the server embodiment contract', () {
    final presentation = CompanionPresentation.fromReplyData({
      'presentation': {
        'expression': 'deflated',
        'motion': 'settle',
        'voiceStyle': 'low_disappointed',
        'voiceEnergy': 0.35,
        'voiceSpeed': 0.92,
        'holdMs': 2800,
        'returnMode': 'decay_to_focus',
      },
    });

    expect(presentation, isNotNull);
    expect(presentation!.expression, 'sad');
    expect(presentation.motion, 'idle');
    expect(presentation.voiceStyle, 'low_disappointed');
    expect(presentation.voiceSpeed, 0.92);
    expect(presentation.hold, const Duration(milliseconds: 2800));
    expect(presentation.returnMode, 'decay_to_focus');
  });

  test('presentation return mode chooses the planned resting state', () {
    const base = CompanionPresentation(
      expression: 'deflated',
      motion: 'settle',
      voiceStyle: 'low_disappointed',
      voiceEnergy: 0.35,
      voiceSpeed: 0.92,
      hold: Duration(milliseconds: 2800),
      returnMode: 'decay_to_focus',
    );

    // All four ReturnMode values have real targets (ADR-0007): watching and
    // decay_to_focus keep the terminal watching focus, decay_to_listening is
    // the voice-session waiting pose, decay_to_idle defers to the tier picker.
    expect(presentationReturnState(base), ('focus', 'focus'));
    expect(
      presentationReturnState(base.copyWith(returnMode: 'watching')),
      ('focus', 'focus'),
    );
    expect(
      presentationReturnState(base.copyWith(returnMode: 'decay_to_listening')),
      ('listening', 'listen_01'),
    );
    // Neutral affect decays into the calm idle tier.
    expect(
      presentationReturnState(base.copyWith(returnMode: 'decay_to_idle')),
      ('idle', 'idle_02'),
    );
  });

  test('decay_to_idle picks the idle tier motion from the affect vector', () {
    const deflated = CompanionPresentation(
      expression: 'sad',
      motion: 'complain',
      voiceStyle: 'low_disappointed',
      voiceEnergy: 0.35,
      voiceSpeed: 0.92,
      hold: Duration(milliseconds: 2800),
      returnMode: 'decay_to_idle',
      valence: -0.8,
      arousal: 0.2,
    );
    expect(presentationReturnState(deflated), ('idle', 'idle_01'));

    const energetic = CompanionPresentation(
      expression: 'excited',
      motion: 'celebrate',
      voiceStyle: 'excited',
      voiceEnergy: 0.9,
      voiceSpeed: 1.05,
      hold: Duration(milliseconds: 2600),
      returnMode: 'decay_to_idle',
      valence: 0.6,
      arousal: 0.8,
    );
    expect(presentationReturnState(energetic), ('idle', 'idle_03'));
  });
}
