import 'dart:math';

import 'package:shared_preferences/shared_preferences.dart';

import 'session_service.dart';

/// Local user preferences (no server-side account).
class PreferencesService {
  static const _keyNickname = 'nickname';
  static const _keyFavoriteTeam = 'favorite_team';
  static const _keyTalkativeness = 'talkativeness';
  static const _keyContinuousConversation = 'continuous_conversation';
  static const _keyDuplexPlaybackCapture = 'duplex_playback_capture';
  static const _keySubtitles = 'subtitles';
  static const _keySound = 'sound';
  static const _keyFirstMeetingCompleted = 'first_meeting_completed';
  static const _keyAnonymousUserId = 'anonymous_user_id';

  final SessionSecretStore _secretStorage;

  PreferencesService({SessionSecretStore? secretStorage})
      : _secretStorage = secretStorage ?? FlutterSessionSecretStore();

  /// 匿名身份的唯一凭据（CONTEXT.md「匿名身份」），读写收敛在这对函数里：
  /// 读 = secure storage 优先 → SharedPreferences 回退；首次读取时把既有
  /// SharedPreferences 值迁移进 secure storage。迁移语义：绝不能因换存储
  /// 生成新 UUID——迁移永远先于生成。
  Future<String> loadOrCreateAnonymousUserId() async {
    final prefs = await SharedPreferences.getInstance();
    String? secureValue;
    try {
      secureValue = await _secretStorage.read(_keyAnonymousUserId);
    } catch (_) {
      secureValue = null; // secure 完全不可用：保持 SharedPreferences 语义
    }
    final secure = secureValue?.trim() ?? '';
    if (secure.isNotEmpty) {
      // 双写维护：把 prefs 兜底副本对齐到 secure 权威值，避免 secure 日后
      // 不可用时回退到一份陈旧身份。
      if (prefs.getString(_keyAnonymousUserId) != secure) {
        await prefs.setString(_keyAnonymousUserId, secure);
      }
      return secure;
    }

    final legacy = prefs.getString(_keyAnonymousUserId)?.trim() ?? '';
    if (legacy.isNotEmpty) {
      await _storeAnonymousUserId(legacy, prefs);
      return legacy;
    }
    final generated = _newAnonymousUserId();
    await _storeAnonymousUserId(generated, prefs);
    return generated;
  }

  /// 双写：secure 为主、SharedPreferences 保留一份兜底；secure 不可用时
  /// prefs 的这份副本仍是唯一凭据，行为与旧实现一致。
  Future<void> _storeAnonymousUserId(
    String value,
    SharedPreferences prefs,
  ) async {
    try {
      await _secretStorage.write(_keyAnonymousUserId, value);
    } catch (_) {
      // secure 不可用：prefs 兜底写入照常进行。
    }
    await prefs.setString(_keyAnonymousUserId, value);
  }

  /// 后端 409 ErrIdentityUnavailable（隐私删除/映射过期）时如实清除本地
  /// 身份：两份存储都清空，之后 loadOrCreateAnonymousUserId 生成全新
  /// UUID——删除就是删除，不假装找回旧身份。
  Future<void> resetAnonymousIdentity() async {
    final prefs = await SharedPreferences.getInstance();
    try {
      await _secretStorage.delete(_keyAnonymousUserId);
    } catch (_) {
      // secure 不可用时也要保证 prefs 一侧被清空。
    }
    await prefs.remove(_keyAnonymousUserId);
  }

  Future<UserProfile> load() async {
    final prefs = await SharedPreferences.getInstance();
    return UserProfile(
      nickname: prefs.getString(_keyNickname) ?? '',
      favoriteTeam: prefs.getString(_keyFavoriteTeam) ?? '',
      talkativeness: prefs.getString(_keyTalkativeness) ?? 'normal',
      continuousConversation: prefs.getBool(_keyContinuousConversation) ?? true,
      duplexPlaybackCapture: prefs.getBool(_keyDuplexPlaybackCapture) ?? true,
      subtitlesEnabled: prefs.getBool(_keySubtitles) ?? true,
      soundEnabled: prefs.getBool(_keySound) ?? true,
    ).ensureOutputAvailable();
  }

  Future<void> save(UserProfile profile) async {
    final prefs = await SharedPreferences.getInstance();
    profile = profile.ensureOutputAvailable();
    await prefs.setString(_keyNickname, profile.nickname);
    if (profile.favoriteTeam.isNotEmpty) {
      await prefs.setString(_keyFavoriteTeam, profile.favoriteTeam);
    } else {
      await prefs.remove(_keyFavoriteTeam);
    }
    await prefs.setString(_keyTalkativeness, profile.talkativeness);
    await prefs.setBool(
      _keyContinuousConversation,
      profile.continuousConversation,
    );
    await prefs.setBool(
      _keyDuplexPlaybackCapture,
      profile.duplexPlaybackCapture,
    );
    await prefs.setBool(_keySubtitles, profile.subtitlesEnabled);
    await prefs.setBool(_keySound, profile.soundEnabled);
  }

  Future<bool> hasCompletedFirstMeeting() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_keyFirstMeetingCompleted) ?? false;
  }

  Future<void> markFirstMeetingCompleted() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_keyFirstMeetingCompleted, true);
  }
}

String _newAnonymousUserId() {
  final random = Random.secure();
  final bytes = List<int>.generate(16, (_) => random.nextInt(256));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  final hex =
      bytes.map((value) => value.toRadixString(16).padLeft(2, '0')).join();
  return 'anon_${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
}

class UserProfile {
  final String nickname;
  final String favoriteTeam;
  final String talkativeness;
  final bool continuousConversation;

  /// duplex_playback_capture（voice-duplex 1.1）：球球说话时允许抢话
  /// 打断。关闭即半双工——播放期不自动打断，收音照常。
  final bool duplexPlaybackCapture;
  final bool subtitlesEnabled;
  final bool soundEnabled;

  const UserProfile({
    required this.nickname,
    required this.favoriteTeam,
    required this.talkativeness,
    this.continuousConversation = true,
    this.duplexPlaybackCapture = true,
    this.subtitlesEnabled = true,
    this.soundEnabled = true,
  });

  bool get hasProfile => nickname.isNotEmpty;

  UserProfile withSubtitlesEnabled(bool enabled) {
    return copyWith(
      subtitlesEnabled: enabled,
      soundEnabled: enabled ? soundEnabled : true,
    );
  }

  UserProfile withSoundEnabled(bool enabled) {
    return copyWith(
      soundEnabled: enabled,
      subtitlesEnabled: enabled ? subtitlesEnabled : true,
    );
  }

  UserProfile ensureOutputAvailable() {
    if (subtitlesEnabled || soundEnabled) return this;
    return copyWith(subtitlesEnabled: true);
  }

  UserProfile copyWith({
    String? nickname,
    String? favoriteTeam,
    String? talkativeness,
    bool? continuousConversation,
    bool? duplexPlaybackCapture,
    bool? subtitlesEnabled,
    bool? soundEnabled,
  }) {
    return UserProfile(
      nickname: nickname ?? this.nickname,
      favoriteTeam: favoriteTeam ?? this.favoriteTeam,
      talkativeness: talkativeness ?? this.talkativeness,
      continuousConversation:
          continuousConversation ?? this.continuousConversation,
      duplexPlaybackCapture:
          duplexPlaybackCapture ?? this.duplexPlaybackCapture,
      subtitlesEnabled: subtitlesEnabled ?? this.subtitlesEnabled,
      soundEnabled: soundEnabled ?? this.soundEnabled,
    );
  }
}
