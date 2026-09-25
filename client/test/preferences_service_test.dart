import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/preferences_service.dart';
import 'package:qiuqiu/services/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  const anonKey = 'anonymous_user_id';
  final generatedPattern = RegExp(
    r'^anon_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
  );

  test('anonymous user id is generated once and remains stable', () async {
    SharedPreferences.setMockInitialValues({});
    final preferences = PreferencesService(
      secretStorage: MemoryIdentitySecretStore(),
    );

    final first = await preferences.loadOrCreateAnonymousUserId();
    final second = await preferences.loadOrCreateAnonymousUserId();

    expect(first, isNotEmpty);
    expect(second, first);
    expect(first, matches(generatedPattern));
  });

  test(
      'deviceId resolves through every storage state without inventing a new id',
      () async {
    final scenarios = <_DeviceIdScenario>[
      // 迁移：prefs 有值 + secure 空 → 原值迁入 secure，绝不因换存储换值。
      const _DeviceIdScenario(
        name: 'prefs value migrates into secure storage unchanged',
        prefsSeed: {anonKey: 'anon_legacy'},
      ),
      // secure 为主：prefs 里的陈旧副本不覆盖权威值。
      const _DeviceIdScenario(
        name: 'secure value wins over a stale prefs copy',
        secureSeed: {anonKey: 'anon_secure'},
        prefsSeed: {anonKey: 'anon_prefs'},
      ),
      // 全新设备：生成一次，双写两处。
      const _DeviceIdScenario(name: 'a fresh device dual-writes one new id'),
      // secure 完全不可用：回退 prefs 语义，仍不换值。
      const _DeviceIdScenario(
        name: 'secure storage outage falls back to prefs without regenerating',
        prefsSeed: {anonKey: 'anon_legacy'},
        secureUnavailable: true,
      ),
      const _DeviceIdScenario(
        name: 'secure storage outage on a fresh device keeps the prefs copy',
        secureUnavailable: true,
      ),
    ];

    for (final scenario in scenarios) {
      SharedPreferences.setMockInitialValues(scenario.prefsSeed);
      final store = scenario.secureUnavailable
          ? ThrowingIdentitySecretStore()
          : MemoryIdentitySecretStore(scenario.secureSeed);
      final preferences = PreferencesService(secretStorage: store);

      final deviceId = await preferences.loadOrCreateAnonymousUserId();
      final prefs = await SharedPreferences.getInstance();

      // 迁移语义：只要旧存储里有值，就绝不能生成第二个 UUID。
      final expectedId =
          scenario.secureSeed[anonKey] ?? scenario.prefsSeed[anonKey];
      if (expectedId != null) {
        expect(deviceId, expectedId, reason: scenario.name);
      } else {
        expect(deviceId, matches(generatedPattern), reason: scenario.name);
      }
      // 双写：prefs 始终保留一份与已解析身份一致的兜底副本。
      expect(prefs.getString(anonKey), deviceId, reason: scenario.name);
      // secure 为主：可用时保存同一值；完全不可用时回退且不崩溃。
      if (scenario.secureUnavailable) {
        expect(store, isA<ThrowingIdentitySecretStore>(),
            reason: scenario.name);
      } else {
        expect(
          (store as MemoryIdentitySecretStore).values[anonKey],
          deviceId,
          reason: scenario.name,
        );
      }
    }
  });

  test(
      'privacy-deleted identity is reset honestly and the next id is brand new',
      () async {
    SharedPreferences.setMockInitialValues({anonKey: 'anon_old'});
    final store = MemoryIdentitySecretStore({anonKey: 'anon_old'});
    final preferences = PreferencesService(secretStorage: store);

    expect(await preferences.loadOrCreateAnonymousUserId(), 'anon_old');

    await preferences.resetAnonymousIdentity();

    expect(store.values[anonKey], isNull);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString(anonKey), isNull);

    final fresh = await preferences.loadOrCreateAnonymousUserId();
    expect(fresh, isNot('anon_old'));
    expect(fresh, matches(generatedPattern));
    expect(store.values[anonKey], fresh);
    expect(prefs.getString(anonKey), fresh);
  });

  test('reset still clears the prefs fallback when secure storage is down',
      () async {
    SharedPreferences.setMockInitialValues({anonKey: 'anon_old'});
    final preferences =
        PreferencesService(secretStorage: ThrowingIdentitySecretStore());

    await preferences.resetAnonymousIdentity();

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString(anonKey), isNull);

    final fresh = await preferences.loadOrCreateAnonymousUserId();
    expect(fresh, matches(generatedPattern));
    expect(prefs.getString(anonKey), fresh);
  });

  test('legacy preferences cannot disable both sound and subtitles', () async {
    SharedPreferences.setMockInitialValues({
      'sound': false,
      'subtitles': false,
    });

    final profile = await PreferencesService().load();

    expect(profile.soundEnabled, isFalse);
    expect(profile.subtitlesEnabled, isTrue);
  });

  test('duplex_playback_capture 默认开启，关闭后可持久化恢复', () async {
    SharedPreferences.setMockInitialValues({});

    final fresh = await PreferencesService().load();
    expect(fresh.duplexPlaybackCapture, isTrue);

    final preferences = PreferencesService();
    await preferences
        .save(fresh.copyWith(duplexPlaybackCapture: false, soundEnabled: true));

    final reloaded = await PreferencesService().load();
    expect(reloaded.duplexPlaybackCapture, isFalse);
  });

  test('turning off one output keeps the other available', () {
    const silent = UserProfile(
      nickname: '',
      favoriteTeam: '',
      talkativeness: 'normal',
      soundEnabled: false,
    );
    const hidden = UserProfile(
      nickname: '',
      favoriteTeam: '',
      talkativeness: 'normal',
      subtitlesEnabled: false,
    );

    expect(silent.withSubtitlesEnabled(false).soundEnabled, isTrue);
    expect(hidden.withSoundEnabled(false).subtitlesEnabled, isTrue);
  });
}

class _DeviceIdScenario {
  final String name;
  final Map<String, String> secureSeed;
  final Map<String, String> prefsSeed;
  final bool secureUnavailable;

  const _DeviceIdScenario({
    required this.name,
    this.secureSeed = const {},
    this.prefsSeed = const {},
    this.secureUnavailable = false,
  });
}

class MemoryIdentitySecretStore implements SessionSecretStore {
  final Map<String, String> values;

  MemoryIdentitySecretStore([Map<String, String>? values])
      : values = values == null
            ? <String, String>{}
            : Map<String, String>.of(values);

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String value) async {
    values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    values.remove(key);
  }
}

class ThrowingIdentitySecretStore implements SessionSecretStore {
  @override
  Future<String?> read(String key) => throw StateError('storage unavailable');

  @override
  Future<void> write(String key, String value) =>
      throw StateError('storage unavailable');

  @override
  Future<void> delete(String key) => throw StateError('storage unavailable');
}
