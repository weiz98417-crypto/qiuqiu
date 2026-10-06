# UTMOSv2 韵律盲测先筛工具（openspec/changes/eval-tooling 9.1）

自动 MOS(Mean Opinion Score)预测：UTMOSv2（MIT，VoiceMOS 2024 Track1
七项第一）对候选 TTS 音频出 1-5 分排序，人耳盲测只评头部——第二次韵律
盲测（TTS 换型/本地腿有卡后）的人工成本减半器。

**域差注记（ADR-0023）**：UTMOSv2 训练域以英语为主，对中文高表现力 TTS
**只粗筛不下结论**——排序用于选盲测头部样本，人耳结论必须真实采集。

## 安装（独立 venv，不进 Go/npm 依赖；python 本体 ≥3.10）

```bash
cd scripts/utmos
python -m venv .venv
source .venv/bin/activate        # Windows: .venv\Scripts\activate
pip install -r requirements.txt  # torch 按需换 cu121 源，见文件内注释
```

## 用法

```bash
# 批量预测：目录下的 wav/mp3 全部打分，按 MOS 降序输出
python mos_rank.py --input path/to/samples/ --output ranking.csv

# 与人耳盲测衔接（tts-supply-switch 韵律盲测门）：
#   1. mos_rank.py 出排序 → 头部（≥阈值，默认 3.5）进盲测集；
#   2. 人耳盲测排序 vs UTMOS 排序的相关性（Spearman）记进
#      docs/evals/ 对应报告——相关性数据决定下轮阈值校准。
```

## 输出格式（ranking.csv）

```
file,mos
sample-003.wav,4.12
sample-001.wav,3.87
...
```

## 状态

- [x] 工具脚本 + 依赖清单 + 衔接流程（本 change 9.1 交付）
- [ ] venv 建立与 ≥10 样本试跑：**待有卡机器/盲测开测时执行**（开发机
      python 为 Microsoft Store 桩、无卡跑 torch 推理无意义——与韵律
      盲测门同一触发条件，见 tts-supply-switch tasks 7.4 留尾）
- [ ] 与人耳盲测排序的相关性记录（盲测完成后回填此处）
