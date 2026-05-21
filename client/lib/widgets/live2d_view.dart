import 'dart:async';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';

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
  String? _lastExpression;
  Timer? _readyPoll;
  int _pollCount = 0;

  @override
  void didUpdateWidget(Live2dView oldWidget) {
    super.didUpdateWidget(oldWidget);
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
    if (_modelReady || _controller == null) return;
    _pollCount++;
    // Fallback: hide spinner after ~9 seconds (30 attempts * 300ms)
    if (_pollCount > 30) {
      _readyPoll?.cancel();
      if (mounted) setState(() => _modelReady = true);
      return;
    }
    try {
      final result = await _controller!.evaluateJavascript(source: 'window.modelReady');
      if (result == 'true') {
        _readyPoll?.cancel();
        if (mounted) setState(() => _modelReady = true);
      }
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        InAppWebView(
          initialUrlRequest: kIsWeb
              ? URLRequest(url: WebUri('http://localhost:8080/live2d.html'))
              : null,
          initialData: kIsWeb
              ? null
              : InAppWebViewInitialData(
                  data: _htmlContent,
                  mimeType: 'text/html',
                  encoding: 'utf8',
                  baseUrl: WebUri('http://qiuqiu.local/'),
                ),
          initialSettings: InAppWebViewSettings(
            transparentBackground: true,
            javaScriptEnabled: true,
          ),
          onLoadStop: (controller, url) {
            if (kIsWeb && !_modelReady) {
              _readyPoll?.cancel();
              _readyPoll = Timer.periodic(const Duration(milliseconds: 300), (_) => _checkReady());
            }
          },
          onLoadResourceWithCustomScheme: kIsWeb
              ? null
              : (controller, url) async {
                  final urlStr = url.toString();
                  if (!urlStr.contains('qiuqiu.local/') || urlStr.contains('..')) return null;
                  final assetPath = urlStr.split('qiuqiu.local/').last;
                  final ext = assetPath.split('.').last.toLowerCase();
                  const mimeMap = {
                    'json': 'application/json',
                    'moc3': 'application/octet-stream',
                    'png': 'image/png',
                    'cdi3': 'application/json',
                    'exp3': 'application/json',
                    'js': 'application/javascript',
                  };
                  try {
                    final data = await rootBundle.load('assets/live2d/$assetPath');
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
                  if (mounted) setState(() => _modelReady = true);
                },
              );
            } else {
              // Web: all async callbacks are unreliable. Hard timeout.
              Timer(const Duration(seconds: 8), () {
                if (!_modelReady && mounted) {
                  _readyPoll?.cancel();
                  setState(() => _modelReady = true);
                }
              });
            }
          },
        ),
        if (!_modelReady)
          const Center(child: CircularProgressIndicator(color: Colors.orange)),
      ],
    );
  }

  @override
  void dispose() {
    _readyPoll?.cancel();
    super.dispose();
  }

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
<script src="http://qiuqiu.local/cubismcore/live2dcubismcore.min.js"></script>
<script src="http://qiuqiu.local/live2d.min.js"></script>
<script src="http://qiuqiu.local/pixi.min.js"></script>
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
var mouthValue = 0;
var exprMap = { idle:0, listening:0, confused:0, excited:1, chat:3, tease:3, happy:3, nervous:4, sad:4, surprised:5, angry:6 };

function resizeModel() {
    if (!model) return;
    model.anchor.set(0.5);
    model.x = app.screen.width / 2;
    model.y = app.screen.height * 0.38;
    var s = Math.min(app.screen.width / 700, app.screen.height / 1000);
    model.scale.set(s * 0.7);
}

async function loadModel() {
    try {
        var L2D = window.Live2DModel || Live2DModel;
        model = await L2D.from(
            'http://qiuqiu.local/models/qiuqiu/female_01Arkit_6.model3.json',
            { autoUpdate: true, autoInteract: false }
        );
        app.stage.addChild(model);
        resizeModel();
        window.addEventListener('resize', resizeModel);

        if (window.flutter_inappwebview) {
            window.flutter_inappwebview.callHandler('onModelReady');
        }
    } catch(e) {
        console.error('Live2D load error:', e);
        if (window.flutter_inappwebview) {
            window.flutter_inappwebview.callHandler('onModelReady');
        }
    }
}

function setExpression(name) {
    var idx = exprMap[name] || 0;
    try { if (model) model.expression(idx); } catch(e) {}
}

function playMotion(name) {
    if (!model) return;
    var map = {hello:['hello',0],cheer:['idle',1],idle:['idle',0],listen:['listen',0],speak:['speak',0],think:['think',0]};
    var m = map[name];
    if (m) try { model.motion(m[0], m[1], 3); } catch(e) {}
}

function setSpeaking(v) {
    speaking = v;
    if (!v) mouthValue = 0;
}

setInterval(function() {
    if (!model) return;
    if (speaking) {
        mouthValue = 0.25 + Math.random() * 0.75;
    } else {
        mouthValue += (0 - mouthValue) * 0.25;
    }
    try {
        model.internalModel.coreModel.setParameterValueById('ParamJawOpen', mouthValue);
    } catch(e) {}
}, 50);

loadModel();
</script>
</body>
</html>
''';
}
