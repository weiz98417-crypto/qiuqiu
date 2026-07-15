import 'dart:convert';

import 'package:web/web.dart' as web;

void sendLive2dState({
  required String expression,
  required bool speaking,
  String? motion,
}) {
  final payload = jsonEncode({
    'type': 'qiuqiu-live2d-state',
    'expression': expression,
    'motion': motion,
    'speaking': speaking,
  });
  final script = web.HTMLScriptElement()
    ..text = '''
      (function() {
        var frames = document.querySelectorAll('iframe');
        for (var index = 0; index < frames.length; index++) {
          var frame = frames[index];
          if (frame.src && frame.src.indexOf('/live2d.html') !== -1 && frame.contentWindow) {
            frame.contentWindow.postMessage($payload, '*');
          }
        }
      })();
    ''';
  final head = web.document.head;
  if (head == null) return;
  head.appendChild(script);
  script.remove();
}
