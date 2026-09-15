# 球球 Live2D 角色制作流程

> 从 AI 生图到 Live2D 模型，全 AI 工具链。无需手绘、无需手动绑骨。

---
## 流程总览

```
Step 1                Step 2              Step 3                Step 4
GPT Image 2    →    拆图层 + 绑骨   →    导出模型     →     集成到 App
生成角色立绘         Cubism Editor        .model3.json          client/assets/
                    (Auto Deformer)       .moc3                替换 Canvas 占位
```

**捷径**：Step 1-3 可以用开源项目 [Textoon](https://github.com/Human3DAIGC/Textoon) 一步完成（文字→Live2D模型）。

---

## Step 1: GPT Image 2 生成角色立绘

### 球球人设概要

| 维度 | 设定 |
|------|------|
| 年龄感 | 20-22 岁女生 |
| 风格 | 日系二次元，现代简约 |
| 配色 | 暖色调（橙色/米色为主） |
| 发型 | 中长发，活泼但不夸张 |
| 服装 | 休闲风（连帽衫/T恤），可选足球元素小配饰 |
| 气质 | 开朗、亲切、有一点点俏皮 |
| 表情 | 丰富但不夸张，适合小窗口展示 |

### Prompt（英文版，GPT Image 2 用）

```
A full-body character turnaround reference sheet of a cute anime girl.

Character details:
- Name: QiuQiu (球球)
- Age: ~20 years old
- Hair: medium-length warm brown hair, slightly wavy, with a small side ponytail
- Eyes: large bright amber eyes, friendly expression
- Outfit: oversized orange hoodie with a small soccer ball badge on the chest, white t-shirt underneath, casual denim shorts
- Accessories: a thin scarf with subtle football pattern, small round earrings
- Build: slim, ~160cm, youthful appearance
- Vibe: cheerful, sporty, approachable, slightly playful

Layout:
- Front view (A-pose, full body, arms slightly away from body)
- 3/4 view
- Expression sheet: happy, excited, surprised, teasing, regretful, neutral
- Color palette swatch on the side

Style: clean anime art style, cel-shaded, clean lineart
Background: pure white
Format: professional character reference sheet, organized layout, high resolution, 2048x2048
```

### Prompt（中文版，给 GPT Image 2 的翻译参考）

```
一张可爱的动漫女孩全身角色三视图设定稿。

角色设定：
- 名字：球球
- 年龄：20岁左右
- 发型：暖棕色中长发，微卷，侧边扎一个小马尾
- 眼睛：明亮的琥珀色大眼睛，友好的表情
- 服装：宽松的橙色连帽衫，胸前有小足球徽章，内搭白色T恤，休闲牛仔短裤
- 配饰：足球图案的薄围巾，小圆耳环
- 身材：苗条，约160cm，青春感
- 气质：开朗、运动风、亲切、一点点俏皮

布局：
- 正面全身A字站立（手臂微微离开身体，方便后期拆图）
- 3/4侧面
- 表情集：开心、兴奋、惊讶、吐槽、遗憾、平静
- 旁边放色板

风格：干净的日系动漫风，赛璐璐上色，清晰线稿
背景：纯白色
格式：专业角色设定稿，2048x2048 高分辨率
```

### 输出后检查

- [ ] 四肢是否和身体分开（方便后期绑骨）
- [ ] 正面是否对称（头发、眼睛左右对齐）
- [ ] 表情面板是否有 6 种表情
- [ ] 背景是否纯白（方便抠图拆层）

---

## Step 2: 拆图层 + AI 自动绑骨

### 工具：Live2D Cubism Editor（免费版）

**下载**：https://www.live2d.com/en/download/cubism/

免费版功能完全够用，含 Auto Deformer 自动绑骨。

### 2.1 拆图层（PSD 分层）

在 Photoshop / GIMP / Photopea 中把 Step 1 生成的图拆成独立图层：

```
图层结构（从上到下）：
├── 前发（刘海+侧发）
├── 脸部（含五官：眉毛、眼睛、鼻子、嘴巴分到子图层）
│   ├── 眉毛左 / 眉毛右
│   ├── 眼睛左 / 眼睛右（含眼白+瞳孔）
│   ├── 鼻子
│   └── 嘴巴
├── 后发
├── 身体（躯干）
├── 左手臂
├── 右手臂
├── 左手
├── 右手
├── 左腿
├── 右腿
└── 配饰（围巾、耳环等）
```

> **关键原则**：需要独立运动的部件分成独立图层。每个图层在 Cubism 里会变成一个 ArtMesh。

### 2.2 导入 Cubism Editor

1. 打开 Cubism Editor → `文件 → 打开` → 选择分层 PSD
2. 检查 Parts 面板确认图层结构正确
3. 调整 Draw Order（前发 > 脸 > 后发 > 身体...）

### 2.3 Auto Deformer（AI 自动绑骨）

1. 选中所有 ArtMesh
2. ` Modeling → Auto Mesh Generation`（自动生成网格）
3. `Deformer → Auto Generation of Deformer`（AI 自动生成骨骼变形器）
4. `Parameter → Auto Generation of Facial Motion`（自动生成面部表情参数）

### 2.4 手动微调（可选）

- 给嘴巴加 ParamMouthOpenY（嘴张合，0~1，Lip Sync 用）
- 给眼睛加 ParamEyeLOpen / ParamEyeROpen（眨眼）
- 给头部加 ParamAngleX/Y/Z（头部转动）
- 身体加 ParamBodyAngleX（身体前倾，进球兴奋用）

---

## Step 3: 导出 Live2D 模型

1. `File → Export for Runtime → Export as model3.json`
2. 选择导出路径，生成以下文件：

```
qiuqiu_model/
├── qiuqiu.model3.json    # 模型配置文件
├── qiuqiu.moc3            # 模型数据
├── textures/              # 纹理贴图
│   └── qiuqiu.2048.png
└── expressions/           # 表情预设（可选）
    ├── excited.exp3.json
    ├── nervous.exp3.json
    └── ...
```

---

## Step 4: 集成到 App

### 4.1 放置模型文件

将导出的文件夹复制到项目中：

```
client/assets/live2d/models/qiuqiu/
├── qiuqiu.model3.json
├── qiuqiu.moc3
└── textures/
    └── qiuqiu.2048.png
```

### 4.2 下载 Cubism 5 Web SDK

从 https://www.live2d.com/en/download/cubism-sdk/ 下载 Cubism 5 SDK for Web。

将 `live2dcubismcore.min.js` 放入 `client/assets/live2d/cubismcore/`。

### 4.3 更新 pubspec.yaml 确保资产被打包

```yaml
flutter:
  assets:
    - assets/live2d/
    - assets/live2d/models/qiuqiu/
    - assets/live2d/models/qiuqiu/textures/
    - assets/live2d/cubismcore/
```

### 4.4 更新 index.html

将 `client/assets/live2d/index.html` 中的 Canvas 占位渲染替换为 Cubism SDK 渲染。

完成后我会帮你更新 `index.html` 加载真实模型。

---

## 捷径：Textoon（文字直接生成 Live2D）

如果不想手动拆层绑骨，可以使用开源项目 [Textoon](https://github.com/Human3DAIGC/Textoon)：

- 输入：文字描述（类似上方的 Prompt）
- 输出：完整的 Live2D 模型
- 时间：< 1 分钟

**GitHub**: https://github.com/Human3DAIGC/Textoon

> 需要 ComfyUI + SDXL 环境。适合有 GPU 的机器。

---

## 文件清单

完成后你桌面上应该有：

| 文件 | 用途 |
|------|------|
| `球球-Live2D角色制作流程.md` | 本文档 |
| Step 1 生成的 PNG | GPT Image 2 输出的角色立绘 |
| Step 2 的 PSD | 分层原画 |
| Step 3 导出的文件夹 | .model3.json + .moc3 + textures |

---
