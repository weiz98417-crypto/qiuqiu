import 'package:flutter/material.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';

class Live2dView extends StatefulWidget {
  final String expression;
  final bool isSpeaking;

  const Live2dView({
    super.key,
    required this.expression,
    required this.isSpeaking,
  });

  @override
  State<Live2dView> createState() => _Live2dViewState();
}

class _Live2dViewState extends State<Live2dView> {
  InAppWebViewController? _controller;
  bool _modelReady = false;
  String? _lastExpression;

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
    }
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        InAppWebView(
          initialData: InAppWebViewInitialData(
            data: _htmlContent,
            mimeType: 'text/html',
            encoding: 'utf8',
            baseUrl: WebUri('about:blank'),
          ),
          onWebViewCreated: (controller) {
            _controller = controller;
            controller.addJavaScriptHandler(
              handlerName: 'onModelReady',
              callback: (args) {
                setState(() => _modelReady = true);
              },
            );
          },
          initialSettings: InAppWebViewSettings(
            transparentBackground: false,
            javaScriptEnabled: true,
          ),
        ),
        if (!_modelReady)
          const Center(child: CircularProgressIndicator(color: Colors.orange)),
      ],
    );
  }

  static const String _htmlContent = '''
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  html, body { width:100%; height:100%; overflow:hidden; background:#1A1A2E; }
  canvas { display:block; width:100%; height:100%; }
</style>
</head>
<body>
<canvas id="live2d"></canvas>
<script>
var mouthOpenY = 0;
var targetMouth = 0;
var currentExpr = 'idle';
var speaking = false;

function setExpression(name) { currentExpr = name; }
function setSpeaking(v) { speaking = v; if(!v) targetMouth = 0; }

// Lip sync simulation (every 100ms)
setInterval(function() {
  if(speaking) {
    targetMouth = 0.3 + Math.random() * 0.7;
  } else {
    targetMouth = 0;
  }
  mouthOpenY += (targetMouth - mouthOpenY) * 0.4;
}, 100);

// Canvas rendering
var canvas = document.getElementById('live2d');
var ctx = canvas.getContext('2d');

function resize() {
  canvas.width = canvas.clientWidth * (window.devicePixelRatio || 1);
  canvas.height = canvas.clientHeight * (window.devicePixelRatio || 1);
  ctx.setTransform(window.devicePixelRatio || 1, 0, 0, window.devicePixelRatio || 1, 0, 0);
}
resize();
window.addEventListener('resize', resize);

var frame = 0;
function draw() {
  frame++;
  var w = canvas.clientWidth;
  var h = canvas.clientHeight;
  ctx.clearRect(0, 0, w, h);

  var cx = w / 2;
  var cy = h * 0.4;
  var r = Math.min(w, h) * 0.2;

  // Body
  ctx.fillStyle = '#FF6B35';
  ctx.beginPath();
  ctx.ellipse(cx, cy + r * 1.3, r * 0.7, r * 0.9, 0, 0, Math.PI * 2);
  ctx.fill();

  // Head
  ctx.fillStyle = '#FFE0C0';
  ctx.beginPath();
  ctx.arc(cx, cy + Math.sin(frame * 0.03) * 2, r, 0, Math.PI * 2);
  ctx.fill();

  // Eyes
  var eyeY = cy + Math.sin(frame * 0.03) * 2 - r * 0.1;
  ctx.fillStyle = '#333';
  ctx.beginPath(); ctx.arc(cx - r * 0.3, eyeY, r * 0.08, 0, Math.PI * 2); ctx.fill();
  ctx.beginPath(); ctx.arc(cx + r * 0.3, eyeY, r * 0.08, 0, Math.PI * 2); ctx.fill();

  // Mouth (lip sync)
  var mw = r * 0.25;
  var mh = r * 0.05 + mouthOpenY * r * 0.12;
  ctx.fillStyle = '#C44';
  ctx.beginPath();
  ctx.ellipse(cx, cy + Math.sin(frame * 0.03) * 2 + r * 0.2, mw, mh, 0, 0, Math.PI * 2);
  ctx.fill();

  requestAnimationFrame(draw);
}
draw();

// Notify Flutter
setTimeout(function() {
  window.flutter_inappwebview.callHandler('onModelReady');
}, 800);
</script>
</body>
</html>
''';
}
