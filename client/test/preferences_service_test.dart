import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/preferences_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('anonymous user id is generated once and remains stable', () async {
    SharedPreferences.setMockInitialValues({});
    final preferences = PreferencesService();

    final first = await preferences.loadOrCreateAnonymousUserId();
    final second = await preferences.loadOrCreateAnonymousUserId();

    expect(first, isNotEmpty);
    expect(second, first);
    expect(
      first,
      matches(
        RegExp(
          r'^anon_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$',
        ),
      ),
    );
  });
}
