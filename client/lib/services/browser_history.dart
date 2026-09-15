import 'browser_history_stub.dart'
    if (dart.library.js_interop) 'browser_history_web.dart' as implementation;

void replaceBrowserUrl(Uri uri) => implementation.replaceBrowserUrl(uri);
