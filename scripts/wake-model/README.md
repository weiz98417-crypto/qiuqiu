# wake-model：唤醒词 KWS 模型下载与落盘约定（wake-word-kws）

模型：`sherpa-onnx-kws-zipformer-wenetspeech-3.3M-2024-01-01` 的 **int8** 推理
三件套（encoder/decoder/joiner chunk-16-left-64）+ tokens.txt，Apache-2.0。
源头锁定 ModelScope 官方镜像 `pkufool/sherpa-onnx-kws-zipformer-wenetspeech-
3.3M-2024-01-01`（pkufool = k2-fsa 核心维护者；k2-fsa 官方发布物是 GitHub
release tar.bz2：`https://github.com/k2-fsa/sherpa-onnx/releases/download/
kws-models/<同名>.tar.bz2`，直连不稳时用它作备源）。每个文件下载后与
ModelScope files API 给的内容 sha256 逐一比对，不匹配即退出。

## 落盘约定（模型不入 git）

- **仓库外缓存（评估用）**：`E:\tools\wake-model\sherpa-onnx-kws-zipformer-
  wenetspeech-3.3M-2024-01-01\`（`WAKE_MODEL_ROOT` 可覆盖）。
- **Flutter 资产（端上用）**：`client/assets/wake/`
  - `keywords.txt` —— **入库**。词表配置化（换词不改代码），
    当前行 `n ǐ h ǎo q iú q iú @你好球球`（wake-word-kws 评估门定稿，
    见 `scripts/wake-eval/results/FINAL-nihao-s1.8-t0.3.json`）。
  - `model/*.onnx + tokens.txt` —— **不入 git**（.gitignore），由下面命令
    放置；pubspec 以目录声明打包，首次构造引擎时拷贝到应用支持目录再喂给
    sherpa（需要真实文件路径）。

## 用法

```bash
# 仓库外缓存（评估 runner 用）
node scripts/wake-model/download.mjs

# 同时同步进 Flutter 资产（端上跑唤醒）
WAKE_ASSETS=1 node scripts/wake-model/download.mjs
```

模型缺失时 WakeService 会进 failure 态并在 `failureReason` 里提示本命令；
web/不受支持平台不加载（conditional import 走 stub）。

## 词表格式（照 k2-fsa KWS keywords.txt）

每行 = 声母/韵母拼音 token（声调标在韵母上）+ `@词形`：
`n ǐ h ǎo q iú q iú @你好球球`。换词 = 改这一行 + 重跑评估门
（`scripts/wake-eval/run-eval.mjs`），参数起步 score=1.8 / threshold=0.3 /
cooldown=1.5s。
