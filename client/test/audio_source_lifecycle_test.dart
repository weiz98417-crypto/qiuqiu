import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/audio_player_native.dart';

void main() {
  test('lifecycle releases the previous source on replace and is idempotent',
      () async {
    final released = <String>[];
    final lifecycle = AudioSourceLifecycle<String>(
      release: released.add,
    );

    // 新建：attach 只记录，不释放。
    lifecycle.attach('first');
    expect(lifecycle.current, 'first');
    expect(released, isEmpty);

    // 新播放：attach 换源时释放上一来源。
    lifecycle.attach('second');
    expect(lifecycle.current, 'second');
    expect(released, ['first']);

    // pause/dispose：释放当前来源。
    lifecycle.dispose();
    expect(lifecycle.current, isNull);
    expect(released, ['first', 'second']);

    // 幂等：重复 dispose 不重复释放。
    lifecycle.dispose();
    expect(released, ['first', 'second']);

    // 释放 → 再新建 → 再释放 全程可重复。
    lifecycle.attach('third');
    expect(lifecycle.current, 'third');
    lifecycle.dispose();
    expect(released, ['first', 'second', 'third']);
    lifecycle.dispose();
    expect(released, ['first', 'second', 'third']);
  });

  test('a failing release does not leave the lifecycle tracking a source', () {
    final lifecycle = AudioSourceLifecycle<Object>(release: (_) {
      throw StateError('release failed');
    });

    lifecycle.attach('source');
    lifecycle.dispose();
    expect(lifecycle.current, isNull);
    lifecycle.dispose();
  });
}
