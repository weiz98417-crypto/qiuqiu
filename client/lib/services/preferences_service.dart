import 'dart:math';

import 'package:shared_preferences/shared_preferences.dart';

/// Local user preferences (no server-side account).
class PreferencesService {
  static const _keyNickname = 'nickname';
  static const _keyFavoriteTeam = 'favorite_team';
  static const _keyTalkativeness = 'talkativeness';
  static const _keyContinuousConversation = 'continuous_conversation';
  static const _keySubtitles = 'subtitles';
  static const _keySound = 'sound';
  static const _keyFirstMeetingCompleted = 'first_meeting_completed';
  static const _keyAnonymousUserId = 'anonymous_user_id';

  Future<String> loadOrCreateAnonymousUserId() async {
    final prefs = await SharedPreferences.getInstance();
    final existing = prefs.getString(_keyAnonymousUserId)?.trim() ?? '';
    if (existing.isNotEmpty) return existing;
    final generated = _newAnonymousUserId();
    await prefs.setString(_keyAnonymousUserId, generated);
    return generated;
  }

  Future<UserProfile> load() async {
    final prefs = await SharedPreferences.getInstance();
    return UserProfile(
      nickname: prefs.getString(_keyNickname) ?? '',
      favoriteTeam: prefs.getString(_keyFavoriteTeam) ?? '',
      talkativeness: prefs.getString(_keyTalkativeness) ?? 'normal',
      continuousConversation: prefs.getBool(_keyContinuousConversation) ?? true,
      subtitlesEnabled: prefs.getBool(_keySubtitles) ?? true,
      soundEnabled: prefs.getBool(_keySound) ?? true,
    );
  }

  Future<void> save(UserProfile profile) async {
    final prefs = await SharedPreferences.getInstance();
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
  final bool subtitlesEnabled;
  final bool soundEnabled;

  const UserProfile({
    required this.nickname,
    required this.favoriteTeam,
    required this.talkativeness,
    this.continuousConversation = true,
    this.subtitlesEnabled = true,
    this.soundEnabled = true,
  });

  bool get hasProfile => nickname.isNotEmpty;

  UserProfile copyWith({
    String? nickname,
    String? favoriteTeam,
    String? talkativeness,
    bool? continuousConversation,
    bool? subtitlesEnabled,
    bool? soundEnabled,
  }) {
    return UserProfile(
      nickname: nickname ?? this.nickname,
      favoriteTeam: favoriteTeam ?? this.favoriteTeam,
      talkativeness: talkativeness ?? this.talkativeness,
      continuousConversation:
          continuousConversation ?? this.continuousConversation,
      subtitlesEnabled: subtitlesEnabled ?? this.subtitlesEnabled,
      soundEnabled: soundEnabled ?? this.soundEnabled,
    );
  }
}
