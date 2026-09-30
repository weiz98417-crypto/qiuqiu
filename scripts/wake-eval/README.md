# wake-eval：唤醒词误触/唤醒率评估 harness（wake-word-kws 10.1）

自包含 node 包（`npm install` 即可，sherpa-onnx-node 走 win-x64 预编译，
与 python sherpa-onnx 同一 C++ 核心/同模型/同 KWS 解码语义；本机无可用
python 故选此路）。迭代与结论见 `results/README.md`。

```bash
# 1) 模型就位（仓库外缓存，sha256 锁）
node scripts/wake-model/download.mjs

# 2) TTS 评估集（正样本=候选唤醒词×双音色×5 语气；负样本=足球语境+近失
#    混淆；MIMO_API_KEY 走环境变量或 backend/.env，绝不打印密钥；
#    dataset/*.wav 已生成的跳过重合成）
node scripts/wake-eval/generate-dataset.mjs

# 3) 单次评估（词表 × score × threshold × trailing-blanks）
node scripts/wake-eval/run-eval.mjs --keywords keywords/ni-hao-qiu-qiu.txt \
  --score 1.8 --threshold 0.3 --label my-run

# 4) 参数扫描
node scripts/wake-eval/sweep.mjs --keywords keywords/hei-qiu-qiu.txt
```

判据：安静唤醒率 >95% / 误触 <1 次/小时折算（负样本 53.1s，1 次命中
≈67.8/h，即整套负样本 0 命中才过）/ cooldown 后二次唤醒 ≥2。
`results/*.json` 入库（每档含逐 case 命中明细）；`dataset/*.wav` 不入库，
manifest（文本口径+每条 sha256）入库。
