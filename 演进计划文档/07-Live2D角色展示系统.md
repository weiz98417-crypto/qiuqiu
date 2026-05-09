# 07 — Live2D角色展示系统

> 所属阶段：Phase 1 · 依赖：无 · 优先级：P0

---

## 1. 概述

Live2D角色系统是球球的"身体"，通过轻量2D角色动画为用户提供视觉陪伴感。

核心要求：
- 嘴型与语音同步（Lip Sync）
- 表情根据事件实时切换
- 基础动作（点头、轻微晃动）
- 性能轻量，不影响主线程

---

## 2. 技术方案

### 2.1 SDK选型

| 方案 | 平台 | 性能 | 成本 | 推荐 |
|------|------|------|------|------|
| Live2D Cubism SDK for Native | iOS/Android | 高 | 免费 | 首选 |
| Live2D Cubism SDK for Web | Web | 中 | 免费 | Web端 |
| PixiJS + Live2D | Web | 中 | 免费 | 备选 |
| Spine 2D | 全平台 | 高 | 付费 | 备选 |

**MVP选择**：Live2D Cubism SDK for Native（原生性能最好）。

### 2.2 模型规格

| 参数 | 值 |
|------|-----|
| 角色名 | 球球 |
| 风格 | 日系二次元（轻量） |
| 分辨率 | 1024×1024 |
| 模型大小 | < 5MB（压缩后） |
| 帧率 | 30fps（最低） / 60fps（推荐） |
| 部件数 | ≤ 20 个（保证性能） |

---

## 3. 角色设计规范

### 3.1 外观方向

- 风格：现代简约二次元
- 年龄感：20岁左右女生
- 配色：暖色调为主（橙色/米色）
- 服装：休闲风（连帽衫/T恤）
- 配饰：可选足球元素小配件（围巾、徽章）

### 3.2 设计原则

- 不要过度性感化
- 表情要丰富但不夸张
- 适合小窗口展示（手机屏幕 1/4 大小）
- 嘴型清晰可辨（Lip Sync用）

---

## 4. 状态与动画

### 4.1 状态定义

| 状态 | 动画表现 | 触发条件 |
|------|---------|---------|
| idle | 轻微呼吸、偶尔眨眼 | 无事件时 |
| listening | 头微倾、眼睛稍大 | 用户说话时 |
| speaking | 嘴型同步、头部微动 | AI说话时 |
| excited | 张嘴笑、身体前倾 | 进球事件 |
| nervous | 眼睛放大、身体微紧 | 射门/紧张时刻 |
| tease | 一侧嘴角上扬、挑眉 | 吐槽/失误 |
| regret | 叹气动作、头微低 | 遗憾时刻 |
| greeting | 挥手 | 用户进入 |

### 4.2 动画参数

| 参数 | 范围 | 说明 |
|------|------|------|
| ParamMouthOpenY | 0–1 | 嘴张开程度（Lip Sync） |
| ParamEyeOpen | 0–1 | 眼睛张开程度 |
| ParamBrowY | -1–1 | 眉毛上下 |
| ParamAngleX/Y/Z | -30–30 | 头部旋转 |
| ParamBodyAngleX | -10–10 | 身体前倾 |
| ParamBreath | 0–1 | 呼吸周期 |

---

## 5. Lip Sync（嘴型同步）

### 5.1 实现方案

```
TTS音频流 → 实时音量分析 → 嘴型参数映射 → Live2D渲染

音量 → 嘴型映射：
- 音量 < 阈值：ParamMouthOpenY = 0（闭嘴）
- 音量 中：ParamMouthOpenY = 0.3–0.6
- 音量 高：ParamMouthOpenY = 0.6–1.0
```

### 5.2 音频分析

- 采样窗口：20ms
- 分析频率：每 20ms 更新一次嘴型
- 平滑过渡：参数变化做低通滤波，避免嘴部抖动

```python
# 伪代码
def audio_to_mouth_open(audio_frame):
    rms = sqrt(mean(audio_frame ** 2))
    normalized = min(rms / threshold, 1.0)
    # 低通滤波平滑
    smoothed = prev_value * 0.7 + normalized * 0.3
    return smoothed
```

---

## 6. 表情切换

### 6.1 切换规则

| 事件 | 目标表情 | 过渡时间 |
|------|---------|---------|
| 进球 | excited | 200ms |
| 射门 | nervous | 150ms |
| 失误/乌龙 | tease/regret | 200ms |
| 回复结束 | idle | 500ms |
| 用户说话 | listening | 100ms |
| 黄牌 | tease | 200ms |
| 红牌 | nervous→regret | 200ms |

### 6.2 过渡动画

- 使用 Live2D Cubism 的 Blend 功能
- 表情之间平滑过渡，不允许硬切
- 过渡曲线：ease-out

---

## 7. 渲染性能

| 指标 | 目标 |
|------|------|
| 模型纹理 | ≤ 2048×2048 |
| Draw Call | ≤ 5 |
| 帧率 | ≥ 30fps（低端机）/ ≥ 60fps（高端机） |
| 内存占用 | ≤ 30MB |
| 首帧加载 | ≤ 500ms |

---

## 8. 客户端集成（Flutter）

```dart
// Flutter 侧 Live2D 集成示意
class QiuQiuLive2D extends StatefulWidget {
  final String expression;  // 当前表情
  final Float32List? audioFrame;  // 当前音频帧（Lip Sync用）

  @override
  Widget build(BuildContext context) {
    return PlatformView(
      viewType: 'live2d_view',
      creationParams: {
        'model': 'qiuqiu_v1',
        'expression': expression,
        'mouthOpen': _calculateMouthOpen(audioFrame),
      },
    );
  }
}
```

---

## 9. 验证标准

- [ ] 角色在待机状态有呼吸动画
- [ ] 嘴型与语音同步，无明显延迟（< 50ms）
- [ ] 表情切换流畅无卡顿
- [ ] 30fps 稳定运行在 iPhone 8 / Android 中端机
- [ ] 模型加载时间 < 500ms
