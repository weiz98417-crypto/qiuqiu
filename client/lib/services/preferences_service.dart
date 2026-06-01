import 'package:shared_preferences/shared_preferences.dart';

/// Local user preferences (no server-side account).
class PreferencesService {
  static const _keyNickname = 'nickname';
  static const _keyFavoriteTeam = 'favorite_team';
  static const _keyTalkativeness = 'talkativeness';

  Future<UserProfile> load() async {
    final prefs = await SharedPreferences.getInstance();
    return UserProfile(
      nickname: prefs.getString(_keyNickname) ?? '',
      favoriteTeam: prefs.getString(_keyFavoriteTeam) ?? '',
      talkativeness: prefs.getString(_keyTalkativeness) ?? 'normal',
    );
  }

  Future<void> save(UserProfile profile) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_keyNickname, profile.nickname);
    if (profile.favoriteTeam.isNotEmpty) {
      await prefs.setString(_keyFavoriteTeam, profile.favoriteTeam);
    }
    await prefs.setString(_keyTalkativeness, profile.talkativeness);
  }
}

class UserProfile {
  final String nickname;
  final String favoriteTeam;
  final String talkativeness;

  const UserProfile({
    required this.nickname,
    required this.favoriteTeam,
    required this.talkativeness,
  });

  bool get hasProfile => nickname.isNotEmpty;
}
