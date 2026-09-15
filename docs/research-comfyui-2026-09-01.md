# ComfyUI + Live2D 数字人工作流核验（截至 2026-09-01）

> 调研日期：2026-09-01（Asia/Shanghai）。本文件只记录可追溯的一手资料；“已核验”表示页面/仓库在调研日可访问并明确写出，“不确定/需现场验证”表示官方未承诺或版本会随更新变化。

## 结论先行

- **ComfyUI 不是 Live2D 编辑器。** ComfyUI 负责文本/图像生成、修复和工作流编排；`.cmo3/.can3` 到 `.moc3/.model3.json` 的建模、参数和运行时导出仍由 Live2D Cubism Editor 完成。
- **“一键生成 Live2D/Auto Deformer”表述不准确。** Cubism 5 的 *Auto Generation of Deformer* 会依据 ArtMesh 自动估计并生成一套固定的人体变形器层级，但仍要求 PSD 分层、检查归类、点击确认并手调；*Auto generation of facial motion* 也要求先准备面部分层/变形器及参数。它不是 ComfyUI 节点，也不是无需人工的完整绑定。
- **截至调研日可见版本：** ComfyUI Core `v0.34.0`（2026-08-26）；Comfy Desktop `v1.0.46`（2026-08-28）；Live2D Cubism Editor Windows 稳定版 `5.3.03`（下载脚本实时值），beta `5.3.04 beta1`。ComfyUI 与自定义节点均采用滚动发布，生产应锁定版本并保存工作流/依赖快照。

## 1. Windows 安装与运行环境（已核验）

### 1.1 选择安装形态

| 形态 | 官方信息（2026-09-01 页面） | 落地建议 |
|---|---|---|
| Comfy Desktop | ComfyUI README 将桌面应用列为 Windows/macOS 新用户最容易的方式；桌面仓库最新 release `v1.0.46`。 | 首选。通过 [comfy.org/download](https://www.comfy.org/download) 安装；模型目录和更新由桌面应用管理，首次启动后在界面确认实际 ComfyUI 根目录。 |
| Windows Portable | 官方提供 `ComfyUI_windows_portable_nvidia.7z`、AMD、Intel；NVIDIA 另有 `..._cu126.7z`（给 10 系及更旧 GPU）。便携包当前自带 Python 3.13、PyTorch CUDA 13.0；20 系及以上需更新 NVIDIA 驱动。 | 需要可复制/离线演示时使用。用 7-Zip 解压到短路径（如 `D:\ComfyUI`），运行包内启动脚本；不要把 Manager 解压成多层目录。 |
| 手动安装 | 官方支持 Windows/Linux/macOS 及 NVIDIA/AMD/Intel/Apple；建议 Python 3.13（3.12 作为兼容回退），Python 3.14 可能导致自定义节点问题；Torch 2.7 为最低支持，新版更佳。 | 仅在需要严格锁定依赖或开发节点时使用；用独立 venv，按硬件安装 PyTorch，再 `pip install -r requirements.txt`。 |

**手动 NVIDIA 示例（官方 README，版本会滚动）：**

```powershell
git clone https://github.com/Comfy-Org/ComfyUI.git
cd ComfyUI
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install --upgrade pip
pip install torch torchvision torchaudio --extra-index-url https://download.pytorch.org/whl/cu130
pip install -r requirements.txt
python main.py --listen 127.0.0.1 --port 8188
```

> 说明：`cu130` 是官方当前示例；应以 [ComfyUI README](https://github.com/Comfy-Org/ComfyUI#manual-install-windows-linux) 当日命令和 GPU 驱动兼容性为准。便携包不要重复执行上述 pip 安装。

### 1.2 运行前检查

```powershell
python --version                 # 手动安装建议 3.13
python -c "import torch; print(torch.__version__, torch.cuda.is_available())"
python main.py --listen 127.0.0.1 --port 8188
```

浏览器打开 `http://127.0.0.1:8188`。若仅本机调用，使用回环地址；不要直接暴露到公网。ComfyUI 核心默认不会下载模型，模型需由用户放入对应目录或通过 Manager 明确下载。

## 2. ComfyUI-Manager 安装与缺失节点（已核验）

官方 Manager README（仓库当前 README 标注 V3.38 安全迁移；可见最新 tag `4.2.2`，2026-06-14）给出以下方式：

### 2.1 已有 Git/手动安装

```powershell
cd D:\ComfyUI\custom_nodes
git clone https://github.com/ltdrdata/ComfyUI-Manager comfyui-manager
# 重启 ComfyUI
```

目录必须正好是 `ComfyUI/custom_nodes/comfyui-manager`；不要解压为 `ComfyUI-Manager-main` 或双层 `ComfyUI-Manager/ComfyUI-Manager`，否则无法正常识别/更新。

### 2.2 Portable

1. 安装 [Git for Windows](https://git-scm.com/download/win)（安装器选择 Windows 默认控制台）。
2. 下载 [install-manager-for-portable-version.bat](https://github.com/ltdrdata/ComfyUI-Manager/raw/main/scripts/install-manager-for-portable-version.bat) 到 `ComfyUI_windows_portable` 根目录（右键“另存为”）。
3. 双击批处理，完成后重启 ComfyUI。

### 2.3 在界面安装工作流所需节点

1. 主菜单点击 **Manager → Install Missing Custom Nodes**；该功能会扫描当前工作流中缺失的节点并列出扩展。
2. 对已登记扩展点击 **Install**；未确认来源的项目显示 **Try Install**，安装前审核仓库与许可证。
3. 重启 ComfyUI，观察启动日志是否有 `import failed`；在 Manager 的 **Fetch Updates/Update** 前先 **Save snapshot**。
4. Manager 的 `security_level`、`allow_git_url_install`、`allow_pip_install` 控制高风险安装；仅在回环监听且明确需要时临时开启，完成后恢复默认拒绝。

## 3. 模型目录与工作流导入（已核验）

### 3.1 通用目录

```
ComfyUI/
├─ models/checkpoints/       # 单文件 checkpoint（SDXL 或 Flux FP8）
├─ models/diffusion_models/  # Flux Dev 等拆分扩散模型
├─ models/unet/              # 部分旧示例（如 Flux Schnell）
├─ models/text_encoders/     # t5xxl、clip_l 等
├─ models/vae/               # VAE（例如 Flux AE）
├─ models/controlnet/        # ControlNet 权重
└─ input/                    # Load Image 节点读取的输入图
```

可用 `extra_model_paths.yaml` 共享其他 UI 的模型目录；修改后重启并在节点下拉框确认文件名。

### 3.2 SDXL（官方示例）

[ComfyUI_examples/sdxl](https://github.com/comfyanonymous/ComfyUI_examples/tree/master/sdxl) 说明 SDXL base 可像普通 checkpoint 使用，最佳分辨率约 1024×1024（或同像素数比例）。将 SDXL `.safetensors` 放入 `models/checkpoints`，把示例 PNG 拖入 ComfyUI 可恢复工作流；若使用 refiner，需同时准备 refiner 权重。

### 3.3 Flux（官方示例）

[Flux examples](https://github.com/comfyanonymous/ComfyUI_examples/tree/master/flux) 明确区分：

- 易用 FP8 单文件（`flux1-dev-fp8.safetensors`、`flux1-schnell-fp8.safetensors`）→ `models/checkpoints`，用 **Load Checkpoint**；Dev 示例要求 CFG=1.0。
- 常规拆分 Dev → `models/diffusion_models/flux1-dev.safetensors`，并将 `t5xxl_fp16`/`clip_l` 放 `models/text_encoders`、AE VAE 放 `models/vae`。
- Schnell 旧示例使用 `models/unet`；以当前工作流节点的模型类型为准，不要仅按文件名猜目录。

下载模型后，将官方示例 PNG/JSON 拖入画布；如出现红色缺失节点，先按上节 Manager 流程安装，再检查每个 Loader 的下拉文件名。工作流 JSON 只保存节点参数，不会替用户下载受许可限制的模型。

## 4. Textoon 项目现实边界（已核验 + 兼容性风险）

[Human3DAIGC/Textoon](https://github.com/Human3DAIGC/Textoon) 主分支 HEAD `7fec72d`（2025-07-02）。README 自身要求：

- ComfyUI 固定到旧 commit `82c530856...`，并用 `conda create -n comfyui python=3.10`；需要 SDXL checkpoint、ControlNet 和 8 个自定义节点（Advanced-ControlNet、tooling-nodes、CXH joy caption、Essentials、art-venture、AlekPet、controlnet_aux、mixlab-nodes）。
- Textoon 独立环境示例为 Python 3.11、Torch 2.5.0（CUDA 12.1；CPU 另装 CPU wheel），还需下载 `TextoonPromptParsing` 和受许可约束的 `assets/haimeng` 数据。
- 运行 `python main.py --text_prompt "..."` 或 `python app/gradio_demo.py`；生成目录包含 Live2D 运行时文件。README 的 ComfyUI API 工作流 JSON（`workflow/*.json`）依赖上述旧节点版本。

**不确定/必须现场验证：** Textoon README 没有声明兼容 ComfyUI `v0.34.0`、Python 3.13 或 Torch CUDA 13.0；不能把“Textoon 一键生成”写成当前 ComfyUI 的保证能力。若要在 2026 版本落地，建议先复制独立 Python 3.10/3.11 环境，按仓库锁定 commit 运行冒烟测试；失败时退回“ComfyUI 生成 PNG → PSD 分层 → Cubism 建模”的稳定路径。

## 5. Cubism 5.3 实际操作（已核验）

### 5.1 版本与授权

Live2D 下载脚本 [download.js](https://cubism.live2d.com/editor/js/download.js) 在调研日返回 Windows/macOS 稳定版 `5.3.03`、beta `5.3.04 beta1`。[FREE vs PRO 对比](https://www.live2d.com/en/cubism/comparison/) 显示 FREE 永久可用但有上限（例如每模型纹理文件最多 1、2048 px；ArtMesh 最多 100；运动参数最多 30；Deformer 总数最多 50；Warp Deformer 分割最多 9×9）。因此“免费版功能完全够用”不能作为通用承诺；复杂数字人应使用试用/PRO 并遵守商业授权。

### 5.2 PSD → ArtMesh

1. 在 Photoshop/GIMP/Photopea 等导出带透明背景的分层 PSD；将需独立运动的前发、后发、脸、眼白/瞳孔、眉、嘴、身体、四肢、配饰分层。
2. Cubism Editor：**File → Open** 选择 PSD。导入后每个 PSD 层通常自动成为 ArtMesh；在 Parts/Draw Order 面板检查遮挡顺序、命名与隐藏层。
3. 使用 **Modeling → ArtMesh → Automatic Mesh generator**（英文菜单名以 Cubism 5.3 为准）生成网格；对眼睛、嘴、发丝等关键层放大检查并手动修网格。

### 5.3 Auto Generation of Deformer（半自动）

官方手册：[Auto Generation of Deformer](https://docs.live2d.com/en/cubism-editor-manual/auto-generation-of-deformer/)。实际菜单是 **Modeling → Deformer → Auto Generation of Deformer**：

1. 先导入已分层 PSD；打开对话框，Cubism 按 ArtMesh 位置预览固定的人体变形器配置。
2. 在“Deformer configuration to be generated”与“ArtMeshes not used”之间拖动/移除 ArtMesh，确认每层归属；可勾选对称生成。
3. 点击 **OK** 后，变形器创建在 Deformer 面板根下，原有变形器不会被删除。检查头发、配饰、非人体部件，必要时手动新建 Warp/Rotation Deformer。

该功能是几何估计和层级生成，不含 AI 绘图、自动拆 PSD、自动完成所有参数键形。

### 5.4 Auto generation of facial motion（半自动）

官方手册：[Auto generation of facial motion](https://docs.live2d.com/en/cubism-editor-manual/face-auto-edit/)。要求脸、左右眼、眼球、眉、耳、嘴、鼻等 ArtMesh 分属 Parts（不存在的部位可省略）。流程：

1. **Parameters palette → Auto generation of facial motion → Generate a face deformer**，为脸部部件选择 ArtMesh，设置变形器转换/Bezier 分割，点击 OK。
2. 再运行 **Generate facial motion**，为脸、眼、眉、嘴选择对应 Warp Deformer，并选择 Angle X/Angle Y 参数；点击 **Angle X/Angle Y** 生成角度运动，必要时 **Four corners** 生成四角键形。
3. 生成后必须在 Parameters palette 逐项检查、修正眼睛闭合、嘴型和遮挡。该功能不能替代人工校正。

### 5.5 参数 ID（已核验）

来自官方 [Standard Parameter List](https://docs.live2d.com/en/cubism-editor-manual/standard-parameter-list/)：

| 用途 | 标准 ID |
|---|---|
| 头部 X/Y/Z | `ParamAngleX` / `ParamAngleY` / `ParamAngleZ` |
| 左/右眼开合 | `ParamEyeLOpen` / `ParamEyeROpen` |
| 眼球 X/Y | `ParamEyeBallX` / `ParamEyeBallY` |
| 嘴型、嘴开合 | `ParamMouthForm` / `ParamMouthOpenY` |
| 身体 X/Y/Z | `ParamBodyAngleX` / `ParamBodyAngleY` / `ParamBodyAngleZ` |
| 呼吸 | `ParamBreath` |

眼睛和嘴通常闭合/关闭为 0、张开为 1；实际取值范围可按模型需要扩展。不要把 `ParamMouthOpenY` 等同于自动生成结果，仍需把参数绑定到嘴部 ArtMesh/Deformer。

## 6. 导出与前端集成（已核验）

官方 [Data for Embedded Use](https://docs.live2d.com/en/cubism-editor-manual/export-moc3-motion3-files/) 明确导出流程：先编辑 Texture Atlas，再 **File → Export embedded file → Export as MOC3 file**。导出设置默认包含 `.moc3`、`.model3.json` 与纹理 PNG，可勾选 `physics3.json`、`motionsync3.json`、`userdata3.json`、`cdi3.json` 等。

典型目录：

```
qiuqiu_model/
├─ qiuqiu.moc3
├─ qiuqiu.model3.json
├─ qiuqiu.physics3.json       # 若配置物理
├─ qiuqiu.cdi3.json           # 可选
├─ textures/                  # Atlas PNG
└─ motions/ expressions/      # 可选 .motion3.json/.exp3.json
```

将整个目录复制到前端静态资源（例如 `client/assets/live2d/models/qiuqiu/`），使用官方 Cubism Web SDK 的 `live2dcubismcore.min.js` 与 SDK 样例加载 `.model3.json`。`model3.json` 内含纹理、MOC3、物理等相对路径，不能只复制单个 PNG 或 JSON。Web/Flutter 的具体渲染封装取决于项目，不存在通用的“替换 Canvas 占位”单行操作。

## 7. 推荐可落地验收清单

1. 记录 ComfyUI、Manager、Cubism、Python、Torch、GPU 驱动版本；保存 Manager snapshot 和工作流 JSON。
2. ComfyUI 以 `127.0.0.1:8188` 启动；SDXL/Flux Loader 均能实际出图，日志无缺失节点。
3. PNG 背景透明、角色正面比例稳定；PSD 至少包含脸、眼、嘴、发、身体等独立层。
4. Cubism 自动变形器生成后，手动检查 ArtMesh 归属；面部自动动作后逐个拖动 `ParamAngleX/Y`、眼睛、嘴参数。
5. 导出 `.moc3 + .model3.json + textures`，在 Cubism Viewer 或 Web SDK 样例加载成功，再接入业务 App。
6. 若 Textoon 失败，不阻塞主线：保留其 ComfyUI 生图结果，转入人工 PSD/Cubism 流程。

## 来源与访问日期

- ComfyUI 官方 README（安装、便携包、Python/PyTorch、模型目录、发布节奏）：<https://github.com/Comfy-Org/ComfyUI>（访问 2026-09-01；release `v0.34.0` 发布 2026-08-26）。
- Comfy Desktop releases：<https://github.com/Comfy-Org/Comfy-Desktop/releases>（访问 2026-09-01；`v1.0.46` 发布 2026-08-28）。
- ComfyUI-Manager README（安装、缺失节点、安全配置）：<https://github.com/ltdrdata/ComfyUI-Manager>（访问 2026-09-01；可见 tag `4.2.2`，2026-06-14）。
- ComfyUI SDXL 示例：<https://github.com/comfyanonymous/ComfyUI_examples/tree/master/sdxl>（访问 2026-09-01）。
- ComfyUI Flux 示例：<https://github.com/comfyanonymous/ComfyUI_examples/tree/master/flux>（访问 2026-09-01）。
- Textoon 官方仓库及 README：<https://github.com/Human3DAIGC/Textoon>（访问 2026-09-01；HEAD `7fec72d`，2025-07-02）。
- Live2D Cubism Editor 下载脚本（稳定/beta 版本）：<https://cubism.live2d.com/editor/js/download.js>（访问 2026-09-01）。
- Live2D FREE/PRO 对比：<https://www.live2d.com/en/cubism/comparison/>（访问 2026-09-01）。
- Cubism Auto Generation of Deformer：<https://docs.live2d.com/en/cubism-editor-manual/auto-generation-of-deformer/>（访问 2026-09-01）。
- Cubism Auto generation of facial motion：<https://docs.live2d.com/en/cubism-editor-manual/face-auto-edit/>（访问 2026-09-01）。
- Cubism Standard Parameter List：<https://docs.live2d.com/en/cubism-editor-manual/standard-parameter-list/>（访问 2026-09-01）。
- Cubism Data for Embedded Use：<https://docs.live2d.com/en/cubism-editor-manual/export-moc3-motion3-files/>（访问 2026-09-01）。
