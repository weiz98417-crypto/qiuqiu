# 球球 MiMo 专属音色生产与上线流程

更新日期：2026-08-31

## 1. 结论先行

球球的专属音色采用以下生产链路：

```text
VoiceDesign 文字设计音色
  → 多次生成候选并盲听筛选
  → 保存唯一、合规、无后期处理的参考 WAV
  → VoiceClone 每次复用同一参考 WAV 固定音色
  → 小流量 A/B
  → 达标后放量；不达标一键回滚到 mimo-v2.5-tts + 冰糖
```

需要特别澄清：小米当前官方字段定义没有“把 VoiceDesign 结果注册成永久 `voice_id`”的接口。响应中的 `message.audio.id` 只是本次响应音频的 ID，不是可复用音色 ID。`mimo-v2.5-tts-voiceclone` 要求在每次请求的 `audio.voice` 中传入 MP3/WAV 的 Base64 Data URL。因此，“固定音色”不是保存服务端 ID，而是**版本化保存同一份参考样音，后端每次复用它**。[1][2]

生产建议分两步：

1. 近期继续以 `mimo-v2.5-tts + 冰糖` 作为实时主链路和回滚基线。
2. 完成专属音色生产后，以 `mimo-v2.5-tts-voiceclone` 灰度；若延迟不满足实时陪看要求，则采用“紧急短句走冰糖流式、普通对话走专属克隆”的混合方案。

原因是官方已为 `mimo-v2.5-tts` 提供真正的低延迟流式输出，但 VoiceDesign 和 VoiceClone 的流式接口目前只是兼容模式，要等完整推理结束后才一次返回，不能降低首音频等待时间。[1]

## 2. 官方能力与硬约束

| 项目 | `mimo-v2.5-tts-voicedesign` | `mimo-v2.5-tts-voiceclone` |
| --- | --- | --- |
| 用途 | 用文字描述从零设计音色 | 用参考音频复刻并保持音色 |
| 音色输入 | `messages` 中必需的 `user` 消息 | `audio.voice` 中必需的音频 Data URL |
| 朗读文本 | `assistant` 消息 | `assistant` 消息 |
| `audio.voice` | 不支持 | 必需 |
| 参考音频格式 | 不需要参考音频 | 仅 MP3/WAV |
| 参考音频上限 | 不适用 | Base64 编码后的字符串不超过 10 MB |
| 风格控制 | `user` 自然语言；不要依赖音频标签 | `user` 自然语言 + `assistant` 音频标签 |
| 低延迟流式 | 尚未提供 | 尚未提供 |
| 推荐输出 | 非流式 WAV | 非流式 WAV |

目标朗读文本必须放在 `role: assistant` 的消息中；`role: user` 用来提供音色描述、语气风格或上下文，不会被朗读。VoiceDesign 的 `user` 消息是必需字段。[1][2]

官方 API 使用：

- Endpoint：`POST https://api.xiaomimimo.com/v1/chat/completions`
- 鉴权头：`api-key: $MIMO_API_KEY`
- 非流式响应音频：`choices[0].message.audio.data`，内容为 Base64 编码音频字节
- 非流式输出格式：`wav`、`mp3`、`pcm`、`pcm16`
- 流式拼接应使用 `pcm16`；官方示例说明为 24 kHz、PCM16LE、单声道[1][2]

## 3. 阶段 0：准备材料与目录

### 3.1 人员与权限

至少指定三类责任人：

- 音色负责人：维护角色定义、提示词和候选编号。
- 评审组：5–7 人，盲听打分，不看候选生成参数。
- 技术负责人：管理 API Key、私有样音、灰度开关和回滚。

### 3.2 环境准备

```powershell
$env:MIMO_API_KEY="<从 MiMo 开放平台获取，禁止写入仓库>"
python -m pip install openai
```

API Key 只放在后端进程环境或密钥管理系统，不写入 Markdown、请求样例、日志、客户端包和 Git 历史。

### 3.3 私有资产目录

参考样音可能包含可识别的声音特征，不应放入 Flutter/Web 公共资源，也不应提交 Git。推荐使用仓库外的受控目录：

```text
<SECURE_VOICE_ROOT>/qiuqiu/
  v1/
    source/
      qiuqiu-v1-reference.raw.wav
    eval/
      qiuqiu-v1-neutral.wav
      qiuqiu-v1-excited.wav
      qiuqiu-v1-comfort.wav
    manifest.json
    prompt.txt
    consent-and-provenance.md
    sha256.txt
```

其中：

- `source/*.raw.wav`：VoiceDesign 原始返回，禁止覆盖或做 EQ、混响、压缩、变调、变速。
- `eval/*.wav`：用同一参考音频经 VoiceClone 合成的验收样音。
- `manifest.json`：记录模型、提示词版本、生成时间、候选 ID、朗读文本、人工评分和入选理由。
- `consent-and-provenance.md`：记录音频由 VoiceDesign 生成、批准用途、责任人和发布日期。
- `sha256.txt`：参考 WAV 的 SHA-256；部署前后核对，防止样音被静默替换。

访问控制采用最小权限：仅 TTS 后端运行账号和音色负责人可读取 `source/`；评审人员只读 `eval/`；客户端永远拿不到参考样音和 Base64。

## 4. 阶段 1：用 VoiceDesign 设计球球音色

### 4.1 冻结角色声音定义

官方建议 VoiceDesign 描述覆盖性别与年龄、音色质感、情绪底色、语速与节奏；1–4 句即可，避免互相冲突、模糊词和 EQ/混响/压缩等后期术语。[1] 官方 MiMo-Skills 进一步建议用 1–2 句白描，不写具体场景、动作、真人演员或 IP 角色名。[3]

球球主提示词 `QD-A`：

```text
20 至 24 岁的中文女性，普通话，声线清亮甜润但不幼态、不夹，气息自然，吐字清楚而口语化。节奏灵活、自然略快，默认松弛亲近，情绪升高时能短促提亮并有爆发，句尾自然回落，偶尔带很轻的笑意。
```

对照提示词 `QD-B`（更活泼）：

```text
20 至 24 岁的中文女性，普通话，明亮有活力的少女感声线，但不过度尖细，发声松弛并带少量自然气息。语速偏快而不赶，重音灵活，兴奋时上扬有力，普通聊天时温暖自然、不使用播音腔。
```

对照提示词 `QD-C`（更陪伴）：

```text
22 至 26 岁的中文女性，普通话，甜润清晰、温暖耐听的年轻声线，轻微笑意，咬字自然，不端着说话。语速中等略快，句内有自然快慢和停顿，安慰时柔和，遇到精彩瞬间能迅速切换为明亮兴奋但不尖叫。
```

禁止把“像某明星、某主播、某动漫角色”写入提示词。若候选音色经多人判断明显接近可识别真人，也直接淘汰，不进入 VoiceClone。

### 4.2 固定 VoiceDesign 朗读文本

第一轮所有候选使用完全相同的文本，避免把“台词差异”误判为“音色差异”。VoiceDesign 阶段不加入括号音频标签，所有设计要求均放在 `user` 消息中。[3]

```text
嗨，我是球球，今晚这场我陪你一起看。刚才这次推进其实很聪明，边路先把空间拉开，中路才有机会。球进了！这脚传中真的太漂亮了，我一下就精神了。别急，还有时间，我们再看看下一波进攻。
```

这段文本同时覆盖：日常招呼、足球分析、短促兴奋和安慰回落。官方建议合成文本应与音色描述匹配；用于候选比较时不要开启会自动改写文本的能力。[1]

### 4.3 生成数量

TTS 生成存在随机性，官方 MiMo-Skills 建议在需要时多次生成供挑选。[3] 本项目采用：

- 3 个提示词版本 × 每版 6 次 = 18 个第一轮候选。
- 候选命名：`QD-A-01` 至 `QD-A-06`、`QD-B-01` 至 `QD-C-06`。
- 每次保存原始 WAV、完整请求 JSON、生成时间和 SHA-256。
- 不通过变速、变调或后期处理“修好”候选；需要修正时修改提示词并重新生成。

### 4.4 VoiceDesign curl 请求

为了保证候选朗读文本完全一致，显式设置 `optimize_text_preview: false`：

```bash
curl --location --request POST 'https://api.xiaomimimo.com/v1/chat/completions' \
  --header "api-key: $MIMO_API_KEY" \
  --header 'Content-Type: application/json' \
  --data-raw '{
    "model": "mimo-v2.5-tts-voicedesign",
    "messages": [
      {
        "role": "user",
        "content": "20 至 24 岁的中文女性，普通话，声线清亮甜润但不幼态、不夹，气息自然，吐字清楚而口语化。节奏灵活、自然略快，默认松弛亲近，情绪升高时能短促提亮并有爆发，句尾自然回落，偶尔带很轻的笑意。"
      },
      {
        "role": "assistant",
        "content": "嗨，我是球球，今晚这场我陪你一起看。刚才这次推进其实很聪明，边路先把空间拉开，中路才有机会。球进了！这脚传中真的太漂亮了，我一下就精神了。别急，还有时间，我们再看看下一波进攻。"
      }
    ],
    "audio": {
      "format": "wav",
      "optimize_text_preview": false
    },
    "stream": false
  }' > qiuqiu-voicedesign-response.json
```

curl 返回的是 JSON，不是裸 WAV；需要从 `choices[0].message.audio.data` 取 Base64 并解码。批量生产推荐直接使用 Python，避免在命令行处理长 Base64。

### 4.5 VoiceDesign Python 请求

```python
import base64
import hashlib
import json
import os
from pathlib import Path

from openai import OpenAI

client = OpenAI(
    api_key=os.environ["MIMO_API_KEY"],
    base_url="https://api.xiaomimimo.com/v1",
)

candidate_id = "QD-A-01"
prompt = (
    "20 至 24 岁的中文女性，普通话，声线清亮甜润但不幼态、不夹，气息自然，"
    "吐字清楚而口语化。节奏灵活、自然略快，默认松弛亲近，情绪升高时能短促"
    "提亮并有爆发，句尾自然回落，偶尔带很轻的笑意。"
)
preview = (
    "嗨，我是球球，今晚这场我陪你一起看。刚才这次推进其实很聪明，边路先把"
    "空间拉开，中路才有机会。球进了！这脚传中真的太漂亮了，我一下就精神了。"
    "别急，还有时间，我们再看看下一波进攻。"
)

completion = client.chat.completions.create(
    model="mimo-v2.5-tts-voicedesign",
    messages=[
        {"role": "user", "content": prompt},
        {"role": "assistant", "content": preview},
    ],
    audio={"format": "wav", "optimize_text_preview": False},
    stream=False,
)

audio_bytes = base64.b64decode(completion.choices[0].message.audio.data)
output = Path(f"{candidate_id}.raw.wav")
output.write_bytes(audio_bytes)
Path(f"{candidate_id}.sha256").write_text(
    hashlib.sha256(audio_bytes).hexdigest() + "\n", encoding="utf-8"
)
Path(f"{candidate_id}.request.json").write_text(
    json.dumps(
        {
            "model": "mimo-v2.5-tts-voicedesign",
            "prompt": prompt,
            "preview": preview,
            "optimize_text_preview": False,
        },
        ensure_ascii=False,
        indent=2,
    ),
    encoding="utf-8",
)
```

## 5. 阶段 2：多轮试听与筛选

### 5.1 第一轮硬性淘汰

任意一项命中即淘汰：

- 出现爆音、削波、持续底噪、数字毛刺、双声、断词或异常长静音。
- 普通话声调明显错误，数字、足球术语或标点停顿严重失真。
- 声音过度幼态、夹、尖锐，或长听有明显疲劳感。
- 平静句也始终兴奋，或者进球句无法产生清晰情绪抬升。
- 句尾持续上扬、逐字念稿、播音腔明显。
- 与可识别真人或受保护角色高度相似。

第一轮保留 5–6 个候选。

### 5.2 第一轮盲听评分

每位评审使用同一副耳机、相近音量；播放器倍速固定 `1.0`。随机打乱候选，只显示候选编号。

| 指标 | 权重 | 评分问题 |
| --- | ---: | --- |
| 真人感 | 25% | 是否有自然气息、连读、停顿和句尾收束，而非机器朗读？ |
| 球球人设 | 25% | 是否甜美、亲近、有活力，但不幼态、不夹？ |
| 长听舒适度 | 20% | 连听 2 分钟是否刺耳、疲劳或过度兴奋？ |
| 情绪跨度 | 15% | 平静、分析、兴奋、安慰之间是否能自然切换？ |
| 清晰度 | 10% | 中文、数字、足球词汇是否清楚？ |
| 辨识度 | 5% | 是否有记忆点，又不像已知真人？ |

采用 1–5 分制。综合分低于 4.0，或“真人感 / 球球人设 / 长听舒适度”任一项低于 3.8，不进入第二轮。

### 5.3 第二轮跨文本复测

对第一轮前 3 名分别使用其原始 WAV 作为 VoiceClone 参考音频，合成第 9 节的完整测试语料。第二轮重点判断：同一参考音频在不同文本和情绪下，音色身份是否稳定。

每个候选至少生成：

- 平静、分析、兴奋、安慰四类各 3 条。
- 同一条兴奋文本重复生成 3 次，检查随机性导致的音色漂移。
- 一条 80–120 字长分析，检查长句节奏和后半段稳定性。

最终只选 1 个主参考音频，并保留 1 个未上线的备选版本。不要在生产中随机轮换多个参考音频，否则用户会感到“球球每天换了一个人”。

## 6. 阶段 3：保存合规参考样音

### 6.1 样音质量标准

官方硬约束只有：VoiceClone 参考音频支持 MP3/WAV，Base64 编码字符串不超过 10 MB。[1][2] 本项目额外采用以下质量门槛：

- 优先保存 VoiceDesign 原始 WAV，不转 MP3，不做二次编码。
- 参考音频有效人声建议 20–45 秒，包含完整自然句，不只有一个口头禅。
- 单人、无背景音乐、无环境声、无他人插话、无音效。
- 不做降噪、EQ、混响、压缩、变速、变调；避免把处理痕迹克隆进音色。
- 峰值不削波，开头结尾无截断，长静音手工剔除但不改变人声本体。
- 台词包含普通语气、一次自然提亮和一次自然回落，不包含尖叫、哭腔或极端表演。

注意：10 MB 限制针对**Base64 编码后的字符串**，不是原始文件大小。Base64 通常会膨胀约三分之一，因此本项目把原始参考文件控制在 7 MiB 以内，并在部署前实际编码检查长度；不能只看 WAV 文件大小。

### 6.2 合规清单

在 `consent-and-provenance.md` 中逐项确认：

- 参考音频由 `mimo-v2.5-tts-voicedesign` 生成，不是从真人录音、影视、直播或社交平台截取。
- 提示词未使用真实演员、主播、公众人物或 IP 角色名；这是官方 MiMo-Skills 明确要求的写法边界。[3]
- 评审组未发现其明显模仿某个可识别真人；有疑问即淘汰并重做。
- 样音只用于球球产品，禁止被其他项目、员工或第三方下载后复用。
- 外部展示时明确其为 AI 数字人合成语音，不冒充真人。
- 资产责任人、批准人、批准日期和允许使用环境已经记录。
- 删除或换版时，可定位所有部署副本、缓存和备份。

如果未来改为克隆真人录音，必须在采集前获得可审计的书面授权，明确用途、期限、地域、撤回方式和再许可边界；没有授权不进入生产。该条是球球项目的上线门禁，不因 API 技术上可调用而豁免。

### 6.3 版本与不可变性

参考文件命名：

```text
qiuqiu-v1-reference.raw.wav
qiuqiu-v1-reference.raw.wav.sha256
```

任何音频内容变化都必须升版本，如 `v2`；严禁用新内容覆盖 `v1` 文件名。运行时日志只记录 `voice_asset_version=qiuqiu-v1` 和 SHA-256 前 12 位，不记录 Base64、绝对文件路径或原始音频内容。

## 7. 阶段 4：用 VoiceClone 固定音色

### 7.1 请求结构

VoiceClone 每次请求都要：

1. 读取同一份参考 WAV。
2. Base64 编码。
3. 加前缀 `data:audio/wav;base64,`。
4. 将完整 Data URL 放入 `audio.voice`。
5. 将表演指导放入 `user` 消息。
6. 将真正朗读文本放入 `assistant` 消息。

VoiceClone 支持通过 `user` 自然语言控制风格，也支持在 `assistant` 文本中加入情绪、停顿、呼吸等音频标签。[1][3] 标签要少而精，同一句最多一个；不能写“转身、挥手”一类不会发声的动作。[3]

### 7.2 VoiceClone curl 请求

Windows PowerShell 下可以直接构造请求文件，再交给 `curl.exe`。这样避免把超长 Base64 拼进命令行参数：

```powershell
$bytes = [System.IO.File]::ReadAllBytes($env:MIMO_TTS_REFERENCE_AUDIO)
$base64Audio = [Convert]::ToBase64String($bytes)
if ($base64Audio.Length -ge 10MB) { throw "Base64 reference audio must be under 10 MB" }

$payload = @{
  model = "mimo-v2.5-tts-voiceclone"
  messages = @(
    @{
      role = "user"
      content = "像熟悉的朋友陪着看球，甜美清亮但不夹，口语自然，语速自然略快，句尾自然回落；此刻看到精彩进球，兴奋有爆发但不要尖叫。"
    },
    @{
      role = "assistant"
      content = "（兴奋，笑）球进了！这脚传中也太漂亮了！"
    }
  )
  audio = @{
    format = "wav"
    voice = "data:audio/wav;base64,$base64Audio"
  }
  stream = $false
} | ConvertTo-Json -Depth 8 -Compress

[System.IO.File]::WriteAllText(
  "$PWD\qiuqiu-voiceclone-request.json",
  $payload,
  [System.Text.UTF8Encoding]::new($false)
)

curl.exe --location --request POST `
  "https://api.xiaomimimo.com/v1/chat/completions" `
  --header "api-key: $env:MIMO_API_KEY" `
  --header "Content-Type: application/json" `
  --data-binary "@qiuqiu-voiceclone-request.json" `
  --output "qiuqiu-voiceclone-response.json"
```

生产调用不要把 `$base64Audio`、请求文件内容或 Data URL 打印到终端和日志；示例请求文件使用完后应按团队安全流程删除。

### 7.3 VoiceClone Python 请求

```python
import base64
import os
from pathlib import Path

from openai import OpenAI

client = OpenAI(
    api_key=os.environ["MIMO_API_KEY"],
    base_url="https://api.xiaomimimo.com/v1",
)

reference_path = Path(os.environ["MIMO_TTS_REFERENCE_AUDIO"])
reference_b64 = base64.b64encode(reference_path.read_bytes()).decode("ascii")
if len(reference_b64.encode("ascii")) >= 10 * 1024 * 1024:
    raise ValueError("Base64 reference audio must be under 10 MB")

completion = client.chat.completions.create(
    model="mimo-v2.5-tts-voiceclone",
    messages=[
        {
            "role": "user",
            "content": (
                "像熟悉的朋友陪着看球，甜美清亮但不夹，口语自然，语速自然略快，"
                "句尾自然回落；此刻看到精彩进球，兴奋有爆发但不要尖叫。"
            ),
        },
        {
            "role": "assistant",
            "content": "（兴奋，笑）球进了！这脚传中也太漂亮了！",
        },
    ],
    audio={
        "format": "wav",
        "voice": f"data:audio/wav;base64,{reference_b64}",
    },
    stream=False,
)

audio_bytes = base64.b64decode(completion.choices[0].message.audio.data)
Path("qiuqiu-clone-output.wav").write_bytes(audio_bytes)
```

### 7.4 球球场景指导模板

基础身份段每次保持一致，只追加当前场景，减少音色与人格漂移：

```text
基础身份：年轻中文女性足球搭档，甜美清亮但不夹，口语自然，不要播音腔。语速自然略快但不赶，句内有快慢，句尾自然回落，保留轻微真实气息。
```

| 场景 | 追加指导 | 允许的少量标签 |
| --- | --- | --- |
| 日常聊天 | 松弛、亲近，像朋友坐在旁边说话，带一点笑意 | `（轻声）`、`（笑）` |
| 战术分析 | 清楚、有判断，重音落在关键结论，语速稳定 | `（认真）`、`（强调）` |
| 进球庆祝 | 开头短促提亮，兴奋有爆发，第二句及时收住，不尖叫 | `（兴奋）`、`（吸气）` |
| 错失机会 | 先短促惋惜，再恢复分析，不拖成长叹 | `（惋惜）`、`（叹气）` |
| 安慰用户 | 音量感稍低、语速略慢，真诚但不说教 | `（温柔）`、`（轻声）` |
| VAR 等待 | 紧张克制，适当停顿，不提前宣布结果 | `（紧张）`、`（停顿）` |

音准、音高走势和语速通过自然语言及标签控制；官方字段定义没有独立的数值型 `speed`、`pitch` 或半音参数。[1][2] 客户端播放器保持 `1.0`，不要再用播放器变速制造情绪。

## 8. 阶段 5：接入球球项目

### 8.1 当前接入点

当前项目的 TTS 请求位于：

- `backend/internal/tts/client.go`
- `backend/internal/config/config.go`
- `backend/cmd/server/main.go`

现有实现只接受预置音色字符串，服务端启动时把 TTS 模型固定为 `mimo-v2.5-tts`。接入 VoiceClone 时不能只把 `MIMO_VOICE` 改成文件路径；需要扩展后端，让它读取参考音频并构造 Data URL。

### 8.2 推荐环境变量

```dotenv
# 通用
MIMO_API_KEY=<secret>
MIMO_BASE_URL=https://api.xiaomimimo.com/v1

# 主链路开关
MIMO_TTS_MODE=clone
MIMO_TTS_MODEL=mimo-v2.5-tts-voiceclone
MIMO_TTS_REFERENCE_AUDIO=C:\secure\voices\qiuqiu\v1\qiuqiu-v1-reference.raw.wav
MIMO_TTS_VOICE_ASSET_VERSION=qiuqiu-v1

# 回滚链路
MIMO_TTS_FALLBACK_MODEL=mimo-v2.5-tts
MIMO_TTS_FALLBACK_VOICE=冰糖

# 灰度
MIMO_TTS_CLONE_PERCENT=5
```

不要把 Base64 本体放入环境变量；它体积大、容易被进程诊断或日志泄漏。只配置私有文件路径，后端启动时读取一次、检查 Base64 小于 10 MB、计算 SHA-256 并缓存 Data URL。

### 8.3 后端请求策略

- `MIMO_TTS_MODE=preset`：`model=mimo-v2.5-tts`，`audio.voice=冰糖`。
- `MIMO_TTS_MODE=clone`：`model=mimo-v2.5-tts-voiceclone`，`audio.voice=<缓存的 Data URL>`。
- 每次请求都发送基础身份指导 + 场景指导；台词只放 `assistant`。
- 播放器倍速固定 `1.0`；原 `voiceSpeed` 只映射为“语速稍慢 / 自然 / 稍快”的模型指导，不再映射成 `playbackRate`。
- 克隆请求超时、空音频、解码失败或延迟熔断时，同一文本立即回退到 `mimo-v2.5-tts + 冰糖`。
- 日志只记录模型、资产版本、生成耗时、音频字节数和回退原因，不记录 API Key、Data URL 或完整参考路径。

### 8.4 延迟权衡

VoiceClone 当前没有真正的低延迟流式输出；设置 `stream=true` 仍需等待完整推理，不能改善实时体验。[1] 因此：

1. 生产默认用非流式 `wav`，实现最简单、最容易监控。
2. 回复尽量 1–3 个短句；先让 LLM 输出口语化短句，不用播放器加速长句。
3. 对“进球了”“太可惜了”等固定短反应，可提前生成专属音色音频并缓存；涉及实时事实的完整句仍动态生成。
4. 若 VoiceClone p95 超过第 10 节阈值，采用混合路由：高时效事件走 `mimo-v2.5-tts + 冰糖` 的真正流式输出，普通问答和主动陪伴走 VoiceClone。
5. 若未来官方为 VoiceClone 上线真正低延迟流式，再切换为 `stream=true + format=pcm16`，按 24 kHz PCM16LE 单声道拼接；在此之前不为兼容流式增加客户端复杂度。[1]

## 9. A/B 测试语料

所有版本使用完全相同的文本与场景指导；播放器倍速固定 `1.0`。

| 类别 | 测试语料 |
| --- | --- |
| 初次见面 | 嗨，我是球球。今晚这场我陪你一起看，你更看好哪一边？ |
| 平静事实 | 比赛来到第六十二分钟，比分还是一比一。 |
| 战术分析 | 他们现在改成四二三一了，两个边路站得更高，中场出球会舒服很多。 |
| 术语 | 这次机会的预期进球值不高，但二点球处理得很聪明。 |
| 进球 | 球进了！这脚传中又快又准，中路包抄完全没有浪费机会！ |
| VAR | 等等，VAR 还在确认。先别急着庆祝，我们看最后结果。 |
| 错失机会 | 哎呀，就差一点。角度已经打出来了，可惜最后偏了一点点。 |
| 安慰 | 别急，还有时间。只要阵型别散，下一次机会很快会来的。 |
| 数字 | 补时还有六分钟，这是他们本场第十二次射门、第五次射正。 |
| 中英混读 | 这次 high press 很坚决，但回防时肋部空间也暴露出来了。 |
| 短反馈 | 嗯，我懂。你是觉得他们中场拿不住球，对吧？ |
| 长分析 | 上半场他们的问题不是没有控球，而是拿球以后推进太慢。边后卫没有及时套上，中场又缺少向前的接应，所以看起来控球不少，真正有威胁的机会却不多。 |

每条语料至少生成 3 次，用于区分“音色本身问题”和“单次随机波动”。

## 10. 验收指标与放量门槛

### 10.1 盲听 A/B

对照组 A：`mimo-v2.5-tts + 冰糖 + 相同风格指导`。

实验组 B：`mimo-v2.5-tts-voiceclone + qiuqiu-v1 + 相同风格指导`。

每位评审随机听取配对样音，不显示模型和版本。上线门槛是球球项目的产品标准，不是小米官方承诺：

| 指标 | 门槛 |
| --- | ---: |
| 实验组总体偏好胜率 | ≥ 65% |
| 真人感平均分 | ≥ 4.2 / 5 |
| 球球人设匹配 | ≥ 4.2 / 5 |
| 情绪匹配 | ≥ 4.0 / 5 |
| 长听舒适度 | ≥ 4.0 / 5 |
| 严重错读、漏读、幻读 | 0 |
| 一般读音问题 | 每 100 条 ≤ 2 条 |
| 明显音色漂移 | 每 100 条 ≤ 2 条 |
| 削波、毛刺、双声等音频事故 | 0 |

### 10.2 在线指标

先用当前冰糖链路测出基线，再比较克隆链路：

- `tts_request_to_audio_ms`：请求开始到完整 WAV 可播放。
- `reply_text_ready_to_play_ms`：文本就绪到用户听见声音。
- `tts_fallback_rate`：克隆失败转冰糖的比例。
- `playback_failure_rate`：音频解码、自动播放和中断失败。
- `user_replay_rate`、`voice_mute_rate`、语音回合继续率。

建议灰度门槛：

- 30 字以内短句，VoiceClone 的 `p95 tts_request_to_audio_ms` 不得比冰糖非流式基线高超过 800 ms，且绝对值不超过 2.5 s。
- `tts_fallback_rate < 1%`。
- `playback_failure_rate` 相对基线上升不超过 0.5 个百分点。
- 用户语音关闭率不得显著恶化；若恶化，先回滚再分析，不用播放器加速掩盖。

如果绝对延迟门槛不现实，以“混合路由”上线，不降低音频质量门槛。

## 11. 灰度、回滚与故障处理

### 11.1 放量顺序

```text
内部测试账号
  → 5% 用户 / 1 天
  → 20% 用户 / 2 天
  → 50% 用户 / 3 天
  → 100%
```

每一阶段都同时查看质量、延迟和失败率。用户分桶按稳定哈希，避免同一用户在冰糖和专属音色之间频繁跳变。

### 11.2 一键回滚

只改变后端配置，不发布客户端：

```dotenv
MIMO_TTS_MODE=preset
MIMO_TTS_MODEL=mimo-v2.5-tts
MIMO_TTS_FALLBACK_VOICE=冰糖
MIMO_TTS_CLONE_PERCENT=0
```

触发任一条件立即回滚：

- 连续 5 分钟 `tts_fallback_rate ≥ 5%`。
- p95 延迟连续 10 分钟超过门槛。
- 出现参考音频泄漏、错误样音版本或权限异常。
- 出现明显冒充真人、侵权投诉或未授权使用疑虑。
- 严重错读、双声、刺耳爆音在生产复现。

回滚后保留问题请求的匿名元数据和输出音频事故样本，但不得保留用户敏感文本或泄漏参考 Base64。

## 12. 可执行检查表

### 设计前

- [ ] 已获取 `MIMO_API_KEY`，仅保存在后端安全环境。
- [ ] 已冻结 `QD-A/B/C` 和统一朗读文本。
- [ ] 提示词没有真人、主播、演员或 IP 角色名。
- [ ] 已准备私有目录和评审表。

### VoiceDesign

- [ ] 生成 18 个原始 WAV，不做后期处理。
- [ ] 每个候选保存请求、候选 ID、时间和 SHA-256。
- [ ] 完成硬性淘汰和第一轮盲听。
- [ ] 前 3 名完成跨文本 VoiceClone 复测。

### 样音入库

- [ ] 只选一个生产参考 WAV。
- [ ] Base64 字符串小于 10 MB，格式为 WAV 或 MP3。
- [ ] 已完成合规、来源、权限和用途记录。
- [ ] 已锁定版本号与 SHA-256，禁止同名覆盖。

### 接入与验收

- [ ] 后端能在 `preset / clone` 间切换。
- [ ] 参考音频不会进入客户端、Git 和日志。
- [ ] 播放器倍速固定 `1.0`。
- [ ] 语速、情绪、音高走势由 `user` 指导和少量音频标签控制。
- [ ] 冰糖回滚路径通过自动化和人工演练。
- [ ] 盲听、错读、稳定性、延迟、失败率全部达标。
- [ ] 按 5% → 20% → 50% → 100% 灰度。

## 13. 官方来源

本文的模型能力、请求字段、格式、流式状态和提示词方法只引用以下小米官方资料；样音时长、评分、灰度和延迟阈值属于球球项目的工程验收标准。

1. [MiMo-V2.5-TTS 官方使用指南](https://mimo.mi.com/docs/zh-CN/quick-start/usage-guide/audio/speech-synthesis-v2.5)；[官方 Markdown](https://mimo.mi.com/static/docs/quick-start/usage-guide/audio/speech-synthesis-v2.5.md)
2. [MiMo 语音合成 API 字段定义](https://mimo.mi.com/static/docs/api/audio/tts.md)
3. [XiaomiMiMo/MiMo-Skills：mimo-v2-5-tts](https://github.com/XiaomiMiMo/MiMo-Skills/tree/main/skills/mimo-v2-5-tts)，以及官方的 [VoiceDesign 脚本](https://github.com/XiaomiMiMo/MiMo-Skills/blob/main/skills/mimo-v2-5-tts/scripts/mimo_tts_voicedesign.py) 和 [VoiceClone 脚本](https://github.com/XiaomiMiMo/MiMo-Skills/blob/main/skills/mimo-v2-5-tts/scripts/mimo_tts_voiceclone.py)
