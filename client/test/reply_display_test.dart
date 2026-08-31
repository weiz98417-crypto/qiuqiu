import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/reply_display.dart';

void main() {
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
    expect(presentation!.expression, 'deflated');
    expect(presentation.motion, 'settle');
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

    expect(presentationReturnState(base), ('focus', 'focus'));
    expect(
      presentationReturnState(base.copyWith(returnMode: 'decay_to_listening')),
      ('listening', 'listen'),
    );
    expect(
      presentationReturnState(base.copyWith(returnMode: 'decay_to_idle')),
      ('idle', 'idle'),
    );
  });
}
