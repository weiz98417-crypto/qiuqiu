import 'package:web/web.dart' as web;

void replaceBrowserUrl(Uri uri) {
  web.window.history.replaceState(null, '', uri.toString());
}
