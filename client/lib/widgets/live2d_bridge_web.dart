import 'dart:convert';

import 'package:web/web.dart' as web;

void configureLive2dSurface() {
  final script = web.HTMLScriptElement()
    ..text = '''
      (function() {
        function disablePointerCapture(root) {
          var frames = (root || document).querySelectorAll('iframe');
          for (var index = 0; index < frames.length; index++) {
            var frame = frames[index];
            if (frame.src && frame.src.indexOf('/live2d.html') !== -1) {
              frame.style.pointerEvents = 'none';
            }
          }
        }
        disablePointerCapture(document);
        if (!window.__qiuqiuLive2dPointerObserver) {
          window.__qiuqiuLive2dPointerObserver = new MutationObserver(function() {
            disablePointerCapture(document);
          });
          window.__qiuqiuLive2dPointerObserver.observe(document.body, {
            childList: true,
            subtree: true
          });
        }
      })();
    ''';
  final head = web.document.head;
  if (head == null) return;
  head.appendChild(script);
  script.remove();
}

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
  _postToLive2dFrame(payload);
}

/// Hands the current TTS reply audio (as a data URL) to the Live2D iframe for
/// lip-sync analysis; playback itself stays in the top-window player.
void sendLive2dAudio(String dataUrl) {
  final payload = jsonEncode({
    'type': 'qiuqiu-live2d-audio',
    'audio': dataUrl,
  });
  _postToLive2dFrame(payload);
}

/// Starts/stops lip-sync analysis in the Live2D iframe.
void sendLive2dLipSyncCommand(String command) {
  final payload = jsonEncode({
    'type': 'qiuqiu-live2d-lipsync',
    'command': command,
  });
  _postToLive2dFrame(payload);
}

void _postToLive2dFrame(String payload) {
  final script = web.HTMLScriptElement()
    ..text = '''
      (function() {
        var frames = document.querySelectorAll('iframe');
        for (var index = 0; index < frames.length; index++) {
          var frame = frames[index];
          if (frame.src && frame.src.indexOf('/live2d.html') !== -1 && frame.contentWindow) {
            // Live2D is a visual background. Let Flutter's controls layered
            // above the stage receive pointer events instead of the iframe.
            frame.style.pointerEvents = 'none';
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
