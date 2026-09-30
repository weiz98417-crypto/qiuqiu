import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart' show AssetManifest, rootBundle;
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';
import 'package:sherpa_onnx/sherpa_onnx.dart' as sherpa_onnx;

import 'wake_service.dart';

/// sherpa_onnx KeywordSpotter 引擎实现（wake-word-kws 10.2）。
///
/// 平台门：Android 先行（proposal 平台口径）；windows 作桌面开发验证通道
/// （sherpa_onnx 出 win-x64 预编译）；iOS 留配置位（proposal 出范围）；
/// web 不编译本文件（wake_engine.dart conditional import）。
///
/// 模型落盘约定（模型不入 git）：`assets/wake/model/` 里的 int8 模型 +
/// tokens.txt 由 `node scripts/wake-model/download.mjs`（WAKE_ASSETS=1）放置；
/// 首次构造时拷贝到应用支持目录（sherpa 需要真实文件路径），keywords.txt
/// 同步落盘后以 keywordsFile 传入（换词只改 asset，不改代码）。词表/参数
/// 取评估门定稿（scripts/wake-eval/results/FINAL-nihao-s1.8-t0.3.json）。
bool get isWakePlatformSupported =>
    !kIsWeb && (Platform.isAndroid || Platform.isWindows);

Future<KwsEngine?> createDefaultKwsEngine(WakeConfig config) async =>
    SherpaKwsEngine(config: config);

const _wakeAssetRoot = 'assets/wake';
const _modelAssetDir = '$_wakeAssetRoot/model';
const _keywordsAssetPath = '$_wakeAssetRoot/keywords.txt';

/// 模型资产 → 支持目录的落盘缓存（进程内只拷一次；文件已存在且等长则跳过）。
Future<Directory>? _modelDirFuture;

class SherpaKwsEngine implements KwsEngine {
  SherpaKwsEngine({required this.config});

  final WakeConfig config;

  sherpa_onnx.KeywordSpotter? _spotter;
  AudioRecorder? _recorder;
  StreamSubscription<Uint8List>? _micSub;
  sherpa_onnx.OnlineStream? _stream;
  final _hits = StreamController<KwsHit>.broadcast();
  bool _bindingsReady = false;

  @override
  Stream<KwsHit> get hits => _hits.stream;

  @override
  Future<void> start() async {
    if (_micSub != null) return; // 已在收音
    final spotter = await _ensureSpotter();
    // 让路刚停麦的 VAD：Android 同一时刻只允许一路 AudioRecord，VAD 的
    // recorder 释放异步尾随（VADState.idle 先于 _stopCapture 落地），缓一
    // 个短拍再起 KWS 收音，避免双持麦克风互踩。
    await Future<void>.delayed(const Duration(milliseconds: 250));
    final recorder = _recorder ??= AudioRecorder();
    final permitted = await recorder.hasPermission();
    if (!permitted) {
      throw StateError('microphone permission denied for wake listening');
    }
    final stream = spotter.createStream();
    _stream = stream;
    final micStream = await recorder.startStream(const RecordConfig(
      encoder: AudioEncoder.pcm16bits,
      sampleRate: 16000,
      numChannels: 1,
      echoCancel: true,
      noiseSuppress: true,
    ));
    _micSub = micStream.listen(
      (pcm) => _processPcm(pcm, stream, spotter),
      onError: (Object error) {
        unawaited(stop());
      },
    );
  }

  /// 100ms 级 pcm16 块 → Float32 → acceptWaveform + 解码排水。
  /// 解码在收音回调里同步做：int8 zipformer 3.3M 实时因子远小于 1，
  /// 无需 isolate（与 VAD 同帧率量级）。
  void _processPcm(
    Uint8List pcm,
    sherpa_onnx.OnlineStream stream,
    sherpa_onnx.KeywordSpotter spotter,
  ) {
    if (pcm.isEmpty) return;
    final samples = pcm16ToFloat32(pcm);
    stream.acceptWaveform(samples: samples, sampleRate: 16000);
    while (spotter.isReady(stream)) {
      spotter.decode(stream);
      final result = spotter.getResult(stream);
      if (result.keyword != '') {
        // sherpa KWS 契约：检出即 reset，才能连检下一次；冷却由服务层管。
        spotter.reset(stream);
        if (!_hits.isClosed) {
          _hits.add(KwsHit(keyword: result.keyword, at: DateTime.now()));
        }
      }
    }
  }

  @override
  Future<void> stop() async {
    _micSub?.cancel();
    _micSub = null;
    _stream?.free();
    _stream = null;
    final recorder = _recorder;
    if (recorder != null && await recorder.isRecording()) {
      await recorder.stop();
    }
  }

  @override
  Future<void> dispose() async {
    await stop();
    _recorder?.dispose();
    _recorder = null;
    _spotter?.free();
    _spotter = null;
    await _hits.close();
  }

  Future<sherpa_onnx.KeywordSpotter> _ensureSpotter() async {
    final existing = _spotter;
    if (existing != null) return existing;
    if (!_bindingsReady) {
      await sherpa_onnx.initBindingsAsync();
      _bindingsReady = true;
    }
    final modelDirFuture = _modelDirFuture ??= _copyModelAssets();
    final dir = await modelDirFuture;
    final spotter = sherpa_onnx.KeywordSpotter(
      sherpa_onnx.KeywordSpotterConfig(
        model: sherpa_onnx.OnlineModelConfig(
          transducer: sherpa_onnx.OnlineTransducerModelConfig(
            encoder: '${dir.path}/encoder.int8.onnx',
            decoder: '${dir.path}/decoder.int8.onnx',
            joiner: '${dir.path}/joiner.int8.onnx',
          ),
          tokens: '${dir.path}/tokens.txt',
          numThreads: 1,
        ),
        keywordsFile: '${dir.path}/keywords.txt',
        keywordsScore: config.keywordsScore,
        keywordsThreshold: config.keywordsThreshold,
      ),
    );
    return _spotter = spotter;
  }
}

/// 拷贝 assets/wake/{keywords.txt,model/*} 到支持目录；返回模型目录。
/// 资产缺失（模型没放置）时抛错 → WakeService 归入 failure 态并在
/// failureReason 里带原因。
Future<Directory> _copyModelAssets() async {
  final support = await getApplicationSupportDirectory();
  final wakeDir = Directory('${support.path}/wake');
  final modelDir = Directory('${wakeDir.path}/model');
  await modelDir.create(recursive: true);

  await _copyAsset(_keywordsAssetPath, '${wakeDir.path}/keywords.txt');

  final manifest = await AssetManifest.loadFromAssetBundle(rootBundle);
  final modelAssets =
      manifest.listAssets().where((path) => path.startsWith('$_modelAssetDir/'));
  var copied = 0;
  for (final asset in modelAssets) {
    final basename = asset.split('/').last;
    if (basename.isEmpty || basename.startsWith('.')) continue;
    await _copyAsset(asset, '${modelDir.path}/$basename');
    copied += 1;
  }
  if (copied == 0) {
    throw StateError(
      'wake model assets missing under $_modelAssetDir — '
      'run: node scripts/wake-model/download.mjs (WAKE_ASSETS=1)',
    );
  }
  // 归一化文件名：eval/端上模型同名约定（encoder/decoder/joiner.int8.onnx）。
  await _renameIfPresent(modelDir,
      'encoder-epoch-12-avg-2-chunk-16-left-64.int8.onnx', 'encoder.int8.onnx');
  await _renameIfPresent(modelDir,
      'decoder-epoch-12-avg-2-chunk-16-left-64.int8.onnx', 'decoder.int8.onnx');
  await _renameIfPresent(modelDir,
      'joiner-epoch-12-avg-2-chunk-16-left-64.int8.onnx', 'joiner.int8.onnx');
  return modelDir;
}

Future<void> _copyAsset(String assetPath, String targetPath) async {
  final data = await rootBundle.load(assetPath);
  final bytes = data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes);
  final existing = File(targetPath);
  if (await existing.exists() && await existing.length() == bytes.length) {
    return;
  }
  await existing.writeAsBytes(bytes, flush: true);
}

Future<void> _renameIfPresent(Directory dir, String from, String to) async {
  final source = File('${dir.path}/$from');
  if (await source.exists()) {
    await source.rename('${dir.path}/$to');
  }
}

/// pcm16LE 字节 → [-1, 1) Float32（sherpa 特征入口口径）。
Float32List pcm16ToFloat32(Uint8List pcm) {
  final aligned = ByteData.sublistView(
    pcm,
    0,
    pcm.length - (pcm.length % 2),
  );
  final count = aligned.lengthInBytes ~/ 2;
  final out = Float32List(count);
  for (var i = 0; i < count; i++) {
    out[i] = aligned.getInt16(i * 2, Endian.little) / 32768.0;
  }
  return out;
}
