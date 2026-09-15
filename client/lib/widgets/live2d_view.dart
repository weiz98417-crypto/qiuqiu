import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';

import 'live2d_bridge_stub.dart' if (dart.library.html) 'live2d_bridge_web.dart'
    as live2d_bridge;

class Live2dView extends StatefulWidget {
  final String expression;
  final bool isSpeaking;
  final String? motion;

  const Live2dView({
    super.key,
    required this.expression,
    required this.isSpeaking,
    this.motion,
  });

  @override
  State<Live2dView> createState() => Live2dViewState();
}

class Live2dViewState extends State<Live2dView> {
  InAppWebViewController? _controller;
  bool _modelReady = false;
  bool _loadFailed = false;

  void evaluateJS(String js) {
    _controller?.evaluateJavascript(source: js);
  }

  Future<String?> evaluateJSGet(String js) async {
    try {
      return await _controller?.evaluateJavascript(source: js);
    } catch (_) {
      return null;
    }
  }

  /// Hands the current TTS reply audio to the Live2D surface for lip-sync
  /// analysis (playback itself stays in the platform audio player). Safe to
  /// call before the model is ready.
  void queueLipSyncAudio(Uint8List audio, {required String mime}) {
    if (audio.isEmpty) return;
    final dataUrl = 'data:$mime;base64,${base64Encode(audio)}';
    if (kIsWeb) {
      live2d_bridge.sendLive2dAudio(dataUrl);
      return;
    }
    evaluateJS('window.qLipSync && qLipSync.queue(${jsonEncode(dataUrl)})');
  }

  /// Starts lip-sync analysis, aligned with platform audio playback start.
  void startLipSync() {
    if (kIsWeb) {
      live2d_bridge.sendLive2dLipSyncCommand('start');
      return;
    }
    evaluateJS('window.qLipSync && qLipSync.start()');
  }

  /// Stops lip-sync analysis (playback ended or was interrupted).
  void stopLipSync() {
    if (kIsWeb) {
      live2d_bridge.sendLive2dLipSyncCommand('stop');
      return;
    }
    evaluateJS('window.qLipSync && qLipSync.stop()');
  }

  String? _lastExpression;
  Timer? _readyPoll;
  Timer? _webReadyTimeout;
  int _pollCount = 0;

  WebUri get _webLive2dUrl {
    const configured = String.fromEnvironment('QIUQIU_LIVE2D_URL');
    if (configured.isNotEmpty) return WebUri(configured);
    return WebUri(
      Uri.base
          .replace(path: '/live2d.html', query: null, fragment: null)
          .toString(),
    );
  }

  @override
  void initState() {
    super.initState();
    if (kIsWeb) {
      live2d_bridge.configureLive2dSurface();
      _webReadyTimeout = Timer(const Duration(seconds: 8), () {
        if (mounted && !_modelReady && !_loadFailed) {
          _readyPoll?.cancel();
          setState(() => _modelReady = true);
        }
      });
    }
  }

  @override
  void didUpdateWidget(Live2dView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (kIsWeb &&
        (widget.expression != oldWidget.expression ||
            widget.isSpeaking != oldWidget.isSpeaking ||
            widget.motion != oldWidget.motion)) {
      _syncState();
      return;
    }
    if (_modelReady && _controller != null) {
      if (widget.expression != _lastExpression) {
        _lastExpression = widget.expression;
        _controller!.evaluateJavascript(
          source: "setExpression('${widget.expression}')",
        );
      }
      if (widget.isSpeaking != oldWidget.isSpeaking) {
        _controller!.evaluateJavascript(
          source: "setSpeaking(${widget.isSpeaking})",
        );
      }
      if (widget.motion != null && widget.motion != oldWidget.motion) {
        _controller!.evaluateJavascript(
          source: "playMotion('${widget.motion}')",
        );
      }
    }
  }

  void _checkReady() async {
    if (_modelReady || _loadFailed || _controller == null) return;
    _pollCount++;
    if (_pollCount > 30) {
      _readyPoll?.cancel();
      if (mounted) setState(() => _loadFailed = true);
      return;
    }
    try {
      final result = await _controller!.evaluateJavascript(
        source: 'window.modelReady',
      );
      if (result == true || result == 'true') {
        _readyPoll?.cancel();
        if (mounted) {
          setState(() => _modelReady = true);
          _syncState();
        }
      }
    } catch (_) {}
  }

  void _syncState() {
    if (kIsWeb) {
      live2d_bridge.sendLive2dState(
        expression: widget.expression,
        speaking: widget.isSpeaking,
        motion: widget.motion,
      );
      return;
    }
    final controller = _controller;
    if (!_modelReady || controller == null) return;
    controller.evaluateJavascript(
      source: "setExpression('${widget.expression}')",
    );
    controller.evaluateJavascript(source: 'setSpeaking(${widget.isSpeaking})');
    if (widget.motion != null) {
      controller.evaluateJavascript(source: "playMotion('${widget.motion}')");
    }
  }

  void _retry() {
    _readyPoll?.cancel();
    setState(() {
      _modelReady = false;
      _loadFailed = false;
      _pollCount = 0;
    });
    _controller?.reload();
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        IgnorePointer(
          child: InAppWebView(
            initialUrlRequest: kIsWeb ? URLRequest(url: _webLive2dUrl) : null,
            initialData: kIsWeb
                ? null
                : InAppWebViewInitialData(
                    data: _htmlContent,
                    mimeType: 'text/html',
                    encoding: 'utf8',
                    baseUrl: WebUri('qiuqiu://asset/'),
                  ),
            initialSettings: InAppWebViewSettings(
              transparentBackground: true,
              javaScriptEnabled: true,
              resourceCustomSchemes: const ['qiuqiu'],
              disableContextMenu: true,
              supportZoom: false,
            ),
            onLoadStop: (controller, url) {
              if (!_modelReady) {
                _readyPoll?.cancel();
                _readyPoll = Timer.periodic(
                  const Duration(milliseconds: 300),
                  (_) => _checkReady(),
                );
              }
            },
            onLoadResourceWithCustomScheme: kIsWeb
                ? null
                : (controller, url) async {
                    final urlStr = url.toString();
                    if (!urlStr.startsWith('qiuqiu://asset/') ||
                        urlStr.contains('..')) {
                      return null;
                    }
                    final assetPath = urlStr.split('qiuqiu://asset/').last;
                    final ext = assetPath.split('.').last.toLowerCase();
                    const mimeMap = {
                      'json': 'application/json',
                      'moc3': 'application/octet-stream',
                      'png': 'image/png',
                      'cdi3': 'application/json',
                      'exp3': 'application/json',
                      'js': 'application/javascript',
                      'mjs': 'application/javascript',
                      'bin': 'application/octet-stream',
                      'wasm': 'application/wasm',
                    };
                    try {
                      final data = await rootBundle.load(
                        'assets/live2d/$assetPath',
                      );
                      return CustomSchemeResponse(
                        data: data.buffer.asUint8List(),
                        contentType: mimeMap[ext] ?? 'application/octet-stream',
                      );
                    } catch (_) {
                      return null;
                    }
                  },
            onWebViewCreated: (controller) {
              _controller = controller;
              if (!kIsWeb) {
                controller.addJavaScriptHandler(
                  handlerName: 'onModelReady',
                  callback: (args) {
                    _readyPoll?.cancel();
                    if (mounted) {
                      setState(() {
                        _modelReady = true;
                        _loadFailed = false;
                      });
                    }
                  },
                );
                controller.addJavaScriptHandler(
                  handlerName: 'onModelError',
                  callback: (args) {
                    _readyPoll?.cancel();
                    if (mounted) setState(() => _loadFailed = true);
                  },
                );
              } else {
                _webReadyTimeout?.cancel();
                _webReadyTimeout = Timer(const Duration(seconds: 8), () {
                  if (mounted && !_modelReady && !_loadFailed) {
                    _readyPoll?.cancel();
                    setState(() => _modelReady = true);
                  }
                });
              }
            },
          ),
        ),
        if (!kIsWeb && !_modelReady && !_loadFailed)
          Center(
            child: Semantics(
              liveRegion: true,
              label: '球球正在入场',
              child: const Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    width: 120,
                    child: LinearProgressIndicator(
                      color: Color(0xFFFF6B35),
                      backgroundColor: Color(0xFF253142),
                    ),
                  ),
                  SizedBox(height: 12),
                  Text(
                    '球球正在入场…',
                    style: TextStyle(color: Color(0xFFAAB4C0), fontSize: 14),
                  ),
                ],
              ),
            ),
          ),
        if (!kIsWeb && _loadFailed)
          Center(
            child: Semantics(
              liveRegion: true,
              child: FilledButton.tonalIcon(
                onPressed: _retry,
                icon: const Icon(Icons.refresh_rounded),
                label: const Text('重新请球球入场'),
              ),
            ),
          ),
      ],
    );
  }

  @override
  void dispose() {
    _readyPoll?.cancel();
    _webReadyTimeout?.cancel();
    super.dispose();
  }

  static const String _lipSyncEngine = r'''
<script type="module">
// Real lip sync: wLipSync (WASM MFCC -> visemes, vendored under
// vendor/wlipsync/) analyzes the queued TTS audio. Playback stays in the
// platform player; this page only taps the bytes for mouth driving. When the
// analyser is unavailable the mouth falls back to the audio envelope, then to
// the legacy random jitter.
(function() {
    var state = { ctx: null, node: null, buffer: null, source: null, startedAt: 0, pendingStart: false };

    function ensureCtx() {
        if (!state.ctx) {
            var Ctx = window.AudioContext || window.webkitAudioContext;
            if (!Ctx) return null;
            state.ctx = new Ctx();
            document.addEventListener('pointerdown', function() {
                if (state.ctx && state.ctx.state === 'suspended') {
                    state.ctx.resume().catch(function() {});
                }
            }, true);
        }
        return state.ctx;
    }

    (async function() {
        try {
            var wlipsync = await import('qiuqiu://asset/vendor/wlipsync/wlipsync-single.js');
            var profileResp = await fetch('qiuqiu://asset/vendor/wlipsync/profile.bin');
            var profile = wlipsync.parseBinaryProfile(await profileResp.arrayBuffer());
            var ctx = ensureCtx();
            if (!ctx) return;
            state.node = await wlipsync.createWLipSyncNode(ctx, profile);
        } catch (e) {
            console.warn('wLipSync unavailable, using envelope fallback:', e);
        }
    })();

    window.qLipSync = {
        queue: function(dataUrl) {
            var ctx = ensureCtx();
            if (!ctx) return;
            state.buffer = null;
            state.pendingStart = false;
            fetch(dataUrl).then(function(resp) { return resp.arrayBuffer(); })
                .then(function(bytes) { return ctx.decodeAudioData(bytes); })
                .then(function(decoded) {
                    state.buffer = decoded;
                    if (state.pendingStart) {
                        state.pendingStart = false;
                        window.qLipSync.start();
                    }
                })
                .catch(function(e) { console.warn('lip sync decode failed:', e); });
        },
        start: function() {
            var ctx = ensureCtx();
            if (!ctx) return;
            if (!state.buffer) {
                // Decode still in flight; start once queue() completes.
                state.pendingStart = true;
                return;
            }
            this.stop();
            ctx.resume().catch(function() {});
            var source = ctx.createBufferSource();
            source.buffer = state.buffer;
            if (state.node) source.connect(state.node);
            state.source = source;
            state.startedAt = ctx.currentTime;
            source.onended = function() { if (state.source === source) state.source = null; };
            try { source.start(0); } catch (e) { state.source = null; }
        },
        stop: function() {
            state.pendingStart = false;
            if (!state.source) return;
            try { state.source.stop(); } catch (e) {}
            state.source = null;
        },
        sample: function() {
            if (!state.node || !state.source || !state.ctx || state.ctx.state !== 'running') return null;
            var w = state.node.weights;
            var volume = state.node.volume || 0;
            var a = (w.A || 0) * volume;
            var i = (w.I || 0) * volume;
            var u = (w.U || 0) * volume;
            var e = (w.E || 0) * volume;
            var o = (w.O || 0) * volume;
            if (a + i + u + e + o <= 0.01) return null;
            return {
                open: Math.min(1, a + 0.8 * o + 0.6 * e + 0.3 * i + 0.2 * u),
                form: Math.max(i, 0.4 * e) - Math.max(u, 0.7 * o),
                funnel: Math.max(u, 0.7 * o),
                stretch: Math.max(i, 0.4 * e)
            };
        },
        sampleEnvelope: function() {
            if (!state.buffer || !state.source || !state.ctx || state.ctx.state !== 'running') return null;
            var elapsed = state.ctx.currentTime - state.startedAt;
            if (elapsed < 0 || elapsed > state.buffer.duration) return null;
            var rate = state.buffer.sampleRate;
            var data = state.buffer.getChannelData(0);
            var start = Math.floor(elapsed * rate);
            var end = Math.min(start + Math.floor(rate * 0.05), data.length);
            var sum = 0;
            for (var i = start; i < end; i++) sum += data[i] * data[i];
            var rms = Math.sqrt(sum / Math.max(1, end - start));
            return { open: Math.min(1, rms * 6), form: 0, funnel: 0, stretch: 0 };
        }
    };
})();
</script>
''';

  static const String _htmlContent = r'''
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  html, body { width:100%; height:100%; overflow:hidden; background:transparent; }
  canvas { display:block; width:100%; height:100%; }
</style>
<script src="qiuqiu://asset/cubismcore/live2dcubismcore.min.js"></script>
<script src="qiuqiu://asset/live2d.min.js"></script>
<script src="qiuqiu://asset/pixi.min.js"></script>
</head>
<body>
<canvas id="live2d"></canvas>
<script>
var app = new PIXI.Application({
    view: document.getElementById('live2d'),
    autoStart: true,
    resizeTo: window,
    backgroundAlpha: 0,
});

var model = null;
var speaking = false;
window.modelReady = false;
var mouth = { open: 0, form: 0, funnel: 0, stretch: 0 };
var exprMap = {
    idle:0, listening:0, confused:0, think:0, thinking:0, focus:2,
    excited:1, cheer:1, chat:3, tease:3, happy:3,
    nervous:4, sad:4, complain:4, surprised:5, surprise:5, angry:6
};

function resizeModel() {
    if (!model) return;
    model.anchor.set(0.5);
    model.x = app.screen.width / 2;
    model.y = app.screen.height * 0.38;
    var s = Math.min(app.screen.width / 650, app.screen.height / 820);
    model.scale.set(s * 0.92);
}

async function loadModel() {
    try {
        var L2D = window.Live2DModel || Live2DModel;
        model = await L2D.from(
            'qiuqiu://asset/models/qiuqiu/female_01Arkit_6.model3.json',
            { autoUpdate: true, autoInteract: false }
        );
        app.stage.addChild(model);
        resizeModel();
        window.addEventListener('resize', resizeModel);
        window.modelReady = true;

        if (window.flutter_inappwebview) {
            window.flutter_inappwebview.callHandler('onModelReady');
        }
    } catch(e) {
        console.error('Live2D load error:', e);
        if (window.flutter_inappwebview) {
            window.flutter_inappwebview.callHandler('onModelError', String(e));
        }
    }
}

function setExpression(name) {
    var idx = exprMap[name] || 0;
    try { if (model) model.expression(idx); } catch(e) {}
}

function playMotion(name) {
    if (!model) return;
    var map = {
        hello:['hello',0],
        celebrate:['celebrate',0], celebrate_01:['celebrate',0], celebrate_02:['celebrate',1], cheer:['celebrate',0],
        idle:['idle',0], idle_01:['idle',0], idle_02:['idle',1], idle_03:['idle',2],
        listen:['listen',0], listen_01:['listen',0], listen_02:['listen',1], focus:['listen',1],
        speak:['speak',0], speak_01:['speak',0], speak_02:['speak',1],
        think:['think',0],
        miss:['miss',0], complain:['complain',0], analysis:['analysis',0],
        tense:['tense',0], agree:['agree',0], wave:['wave',0]
    };
    var m = map[name];
    if (m) try { model.motion(m[0], m[1], 3); } catch(e) {}
}

function setSpeaking(v) {
    speaking = v;
}

function applyMouth() {
    if (!model) return;
    try {
        var core = model.internalModel.coreModel;
        core.setParameterValueById('ParamMouthOpenY', mouth.open);
        core.setParameterValueById('ParamJawOpen', mouth.open);
        core.setParameterValueById('ParamMouthForm', mouth.form);
        core.setParameterValueById('ParamMouthFunnel', mouth.funnel);
        core.setParameterValueById('ParamMouthStretchLeft', mouth.stretch);
        core.setParameterValueById('ParamMouthStretchRight', mouth.stretch);
    } catch(e) {}
}

setInterval(function() {
    if (!model) return;
    var target = { open: 0, form: 0, funnel: 0, stretch: 0 };
    if (speaking) {
        var q = window.qLipSync;
        var viseme = q ? q.sample() : null;
        var envelope = viseme || (q ? q.sampleEnvelope() : null);
        if (viseme) {
            target = viseme;
        } else if (envelope) {
            target = envelope;
        } else {
            // Analyser not ready (vendor libs missing or audio not decoded):
            // legacy random jaw jitter keeps the mouth alive.
            target.open = 0.25 + Math.random() * 0.75;
        }
    }
    mouth.open += (target.open - mouth.open) * 0.5;
    mouth.form += (target.form - mouth.form) * 0.3;
    mouth.funnel += (target.funnel - mouth.funnel) * 0.4;
    mouth.stretch += (target.stretch - mouth.stretch) * 0.4;
    applyMouth();
}, 50);

loadModel();
</script>
''' + _lipSyncEngine;
}
