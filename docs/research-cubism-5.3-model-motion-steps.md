# Live2D Cubism Editor 5.3.03：从 PSD 到可嵌入模型与动作（操作底稿）

> 用途：给“自己的数字人形象”制作基础模型，供 Textoon 或其他 Cubism SDK 运行时加载。以下菜单以 Cubism Editor 5.3.03（Windows）为基准；官方英文手册在 2026-09-01 的页面可能显示 5.4 alpha 的更新日期，涉及 5.4 新功能的地方已单独标注。不要把本底稿当作“自动生成 Live2D”的说明：ComfyUI 只负责生成/修图，分层、绑定、参数和动作仍在 Cubism Editor 完成。

## 0. 文件和命名约定

建立项目目录（示例）：

```
my-avatar/
  source/avatar.psd
  model/avatar.cmo3
  export/avatar.moc3
  export/avatar.model3.json
  export/avatar.2048/texture_00.png
  export/avatar.physics3.json
  export/motions/idle.motion3.json
  export/expressions/happy.exp3.json
```

建议在建模前固定参数 ID；动画文件会按 ID 找参数，建完动画后改 ID 会导致曲线丢失或模型与动画断链。[官方：Animation Preparation](https://docs.live2d.com/en/cubism-editor-manual/animation-preparation/)

推荐 ID（可按自己的部件增删，但不要重复）：

|用途|ID|范围（最小/默认/最大）|
|---|---|---|
|脸左右、上下、倾斜|`ParamAngleX/Y/Z`|-30 / 0 / 30|
|身体左右、上下、倾斜|`ParamBodyAngleX/Y/Z`|-10 / 0 / 10|
|左/右眼开合|`ParamEyeLOpen`, `ParamEyeROpen`|0 / 1 / 1|
|眼球左右、上下|`ParamEyeBallX`, `ParamEyeBallY`|-1 / 0 / 1|
|眉毛上下|`ParamBrowLY`, `ParamBrowRY`|-1 / 0 / 1|
|嘴开合、嘴型|`ParamMouthOpenY`, `ParamMouthForm`|0 / 0 / 1；-1 / 0 / 1|
|呼吸|`ParamBreath`|0 / 0 / 1|
|头发摆动|`ParamHairFront`, `ParamHairSide`, `ParamHairBack`|-1 / 0 / 1|

这些名称和默认范围来自官方 Standard Parameter List；如果 Textoon/运行时已有接口约定，优先保持其 ID。[官方：Standard Parameter List](https://docs.live2d.com/en/cubism-editor-manual/standard-parameter-list/)

## 1. 在 ComfyUI/绘图软件准备可绑定 PSD

### 1.1 分层清单

在 Photoshop 或 CLIP STUDIO PAINT 中制作 `avatar.psd`。每个最终可独立变形的部件放一层（可用组整理，但组内最底层最终成为纹理）：

1. `Head_Base`：脸和耳朵；
2. `EyeL_White`, `EyeL_Iris`, `EyeL_Pupil`, `EyeL_Lid`, `EyeL_Brow`（右眼同理）；
3. `Mouth_Inside`, `Mouth_Teeth`, `Mouth_Tongue`, `Mouth_Line`；
4. `Hair_Back`, `Hair_Side_L/R`, `Hair_Front`；
5. `Body`, `Clothes`, `Arm_L/R`, `Hand_L/R`；
6. 需要切换的服装/表情各自独立层；
7. 各层向外扩画（被遮挡区域也要补画），否则转头、眨眼会露出空洞。

### 1.2 PSD 硬性检查

在 Photoshop：

1. `图像(Image) > 模式(Mode) > RGB 颜色(RGB Color)`；
2. `图像(Image) > 模式(Mode) > 8 位/通道(8 Bits/Channel)`；
3. `编辑(Edit) > 转换为配置文件(Convert to Profile)`，目标设为 sRGB（不是“指定配置文件”）；
4. 对每个部件合并线稿、填充、滤镜和剪贴蒙版；执行 `应用图层蒙版(Apply Layer Mask)`；
5. 删除 Photoshop CC 2019+ 的路径信息；层名全部唯一；不要把图层蒙版留在导入 PSD 中。

Cubism 官方只保证 Photoshop 和 CLIP STUDIO PAINT 生成的 PSD；非 RGB、非 8bit/channel 的 PSD 无法导入。[官方：Notes on PSD creation](https://docs.live2d.com/en/cubism-editor-manual/precautions-for-psd-data/)

**成功标准**：在 Photoshop 中逐层隐藏/显示时，每层都是完整、带透明背景的部件；文件属性为 RGB/8bit/sRGB，保存为 `.psd`。

## 2. 新建模型并导入 PSD

1. 启动 Cubism Editor，拖动 `avatar.psd` 到 Modeling 工作区 View 区；也可用 `File > Open`。
2. 在 `Model Settings` 选择 `Create new model from PSD file`。只有要兼容 Cubism 5.2 或更早混合模式时才选 `Create new model from PSD file (Cubism 5.2 blend mode or earlier)`。
3. 导入后每个 PSD 图层会自动成为一个 ArtMesh，并被放入 Parts。默认 ArtMesh 边缘留 1 px；导入前可在 `File > Environment settings > Modeling > Margins inside the generated ArtMesh` 修改。
4. 在 `Parts` palette 重命名并分组；拖拽调整绘制顺序（眼白应在眼睑后，瞳孔在眼白上）。
5. 保存源工程：`File > Save As`，保存为 `model/avatar.cmo3`。

已有模型要追加或替换 PSD：再次拖入 PSD，在 `Model Settings` 选择 `Add PSDs to or replace PSDs in the open model`，再按对话框选择新增 ArtMesh、仅注册源图或替换既有 PSD。[官方：Import PSDs](https://docs.live2d.com/en/cubism-editor-manual/psd-import/)

## 3. ArtMesh：自动网格后手调

### 3.1 自动网格

1. 在 `Parts` palette 选择一个或多个 ArtMesh；右键选择 `Auto Generation of ArtMesh`（或 Modeling 工作区工具栏的自动网格按钮）。
2. 在对话框设置网格密度/精度；脸、眼睑、嘴角、头发尖端需要更密，纯色衣服可低密度。
3. 确认预览无跨透明区域的大三角形后执行生成。

### 3.2 手动修网格

1. 选中 ArtMesh，进入工具栏 `Edit Mesh`（或右键 `Edit Mesh manually`）；拖拽顶点移动，单击边/面添加顶点，删除工具去掉多余点。
2. 网格必须覆盖可见像素；眼球、嘴线等细长部件沿轮廓布点；不要让一个三角形跨越需要独立变形的边界。
3. 完成后点击 `Finish/Exit Mesh Edit` 返回普通选择。

**成功标准**：在 View 中拖动 ArtMesh，纹理不撕裂、不出现大面积透明三角；网格顶点数量足以支撑计划中的弯曲。

## 4. Texture Atlas（纹理图集）

1. 点击工具栏 `Edit Texture Atlas`（或 `Modeling > Texture > Edit Texture Atlas`）。
2. 首次打开在 `New texture setting` 输入宽高（建议 2048×2048；目标 SDK 若只支持方形，必须用方形），选择 `Image for all models`，点击 `Set automatic layout`。
3. 检查每个 ArtMesh 纹理是否完整放入；可拖动、旋转和缩放单个图块。边缘留出防止采样串色的间距。
4. 需要多张图时点击 `Add Texture`，在纹理列表切换；右键纹理标签可 `Rename texture`（只改编辑器显示名，不改导出文件名）。
5. 点击 `OK` 保存图集，再 `File > Save` 保存 `.cmo3`。

**成功标准**：所有显示中的 ArtMesh 都有对应纹理图块，无越界/重叠；在 Viewer 中开启 mipmap 后边缘不出现白线。[官方：Edit Texture Atlas](https://docs.live2d.com/en/cubism-editor-manual/texture-atlas-edit/)

## 5. Deformer 层级与参数建模

### 5.1 建立层级

在 `Deformer` palette 采用“全身 Rotation → 身体/头部 Warp → 局部 Warp → ArtMesh”的树：

1. 选中头部 ArtMesh，点击 `Create Warp Deformer`（或 `Modeling > Deformer > Create Warp Deformer`），在对话框设置横/纵 Bezier divisions（脸通常 5×5）；命名 `D_Warp_Head`。
2. 选中 `D_Warp_Head`，点击 `Create Rotation Deformer`，中心放在脖子/头部旋转轴，命名 `D_Rot_Head`，并把 `D_Warp_Head` 拖到其子级。
3. 局部头发、眼睛和嘴各建 Warp（`D_Warp_HairFront`、`D_Warp_EyeL` 等），将对应 ArtMesh 拖入。
4. 在 Deformer palette 拖动对象改变父子关系；父级改变会传递给子级。不要在同一层把一个 ArtMesh 同时挂两个互相独立的根变形器。

官方自动生成变形器可从 `Modeling > Deformer > Auto Generation of Deformer` 调用；自动结果仍须检查是否包住全部子对象。[官方：Making and placement of Warp Deformer](https://docs.live2d.com/en/cubism-editor-manual/making-and-placement-of-warp-deformer/) · [Rotation Deformer](https://docs.live2d.com/en/cubism-editor-manual/making-and-rotation-of-rotationdeformer/)

### 5.2 创建参数和三点关键形

1. 打开 `Parameter` palette，点击 `Add parameter`（`+`），输入名称和固定 ID，填写 Minimum/Default/Maximum。
2. 选中要驱动的 ArtMesh/Deformer，在参数行右键 `Add 3 keyforms`（或点击参数行的三点图标），得到最小、默认、最大三个关键形。
3. 把参数滑块拖到最小值，使用 `Edit Form`/`Ctrl` 拖动父 Deformer 或顶点制作左/闭合形；拖到最大值制作右/张开形；回到默认值检查中立形。不要直接移动 Deformer 外框改变结构，应该编辑其 keyform。
4. `Angle X/Y/Z`：分别制作左右转头、抬低头、左右侧倾；先在父级 `D_Rot_Head` 做大方向，再在 `D_Warp_Head` 修脸型。
5. 眼睛：`ParamEyeLOpen/ROpen` 在 0 制作闭眼（上下眼睑贴合），1 制作睁眼；必要时最大值 1.5 表示睁大。眼球 ArtMesh 绑定 `ParamEyeBallX/Y` 的 -1/0/1 三点，并用 `Clipping Mask` 让瞳孔只显示在眼白内。
6. 嘴：`ParamMouthOpenY` 0 为闭合、1 为张开；`ParamMouthForm` -1 做撇嘴/生气、+1 做微笑。嘴内、牙齿、舌头分别做显示/透明度或形状 keyform。
7. 眉毛、身体、头发按标准 ID 添加 keyform；身体和头发不要与 Textoon/运行时将要实时控制的参数重复驱动。

**成功标准**：逐个拖动参数时，所有关键形连续插值，无跳变、露底或左右方向反转；`Parameter` palette 中每个使用的参数都有清晰 ID 和范围。[官方：Parameters](https://docs.live2d.com/en/cubism-editor-manual/parameter/) · [Keyform editing](https://docs.live2d.com/en/cubism-editor-manual/parameter-modifyication/)

## 6. 呼吸、眨眼、口型与物理摆动

### 6.1 呼吸参数

给胸口/肩膀/身体 Warp 建 `ParamBreath`（0 默认，1 吸气），在参数 0 与 1 制作胸廓和肩膀的微小形变。Viewer 的 `Animation > Breath` 只会作用于 ID 为 `PARAM_BREATH` 或 `ParamBreath` 的参数。

### 6.2 自动眨眼和口型

1. 在 Animation 工作区打开模型，打开 `Animation > Eye Blinking Setting`，将左/右眼开合参数分别指定为 `ParamEyeLOpen`、`ParamEyeROpen`。
2. 录制普通动作时，在 `File > Export Embedded File > Export motion file` 的导出对话框勾选 `Burn Lip-sync and Blink into Motion`，使眨眼/口型曲线烘焙进 motion3；否则由运行时的眨眼和口型系统实时叠加。
3. 若表情将眼睛设为非默认值，在 Viewer 的表达式设置中右键该眼睛参数选择 `Multiply mode`，否则眨眼会不自然。[官方：Cubism Viewer（眨眼/口型烘焙说明）](https://docs.live2d.com/en/cubism-editor-manual/cubism3-viewer-for-ow/)

### 6.3 物理（头发/衣服）

1. `Modeling > Open Physics Settings`，Physics tab 的 `Calculate FPS` 选择 60（可选 15/30/120；应与目标场景一致）。
2. 点击 `Add` 新建组，命名如 `HairFront`。
3. 在 `Input Settings` 点击 `Add`，输入 `ParamAngleX`（Type=`Angle`，Influence 例如 100%）和 `ParamAngleY`（Type=`Position Y`，按需要降低 Influence）。输入是“摆动的原因”。
4. 在 `Output Settings` 点击 `Add`，选择 `ParamHairFront`，设置影响方向/Influence；输出是“被摆动的参数”。同一类型输入影响总和不得超过 100%。
5. 调整 `Physical model settings` 的长度、重量、阻尼、反弹等，观察 Pendulum preview；用 View/Preview 检查头发是否稳定。需要多段头发就新增输出参数或物理组。
6. 打开 `Edit Group` 调整组顺序；若出现红色处理顺序提示，先调整组优先级，再调输出 Influence。保存 `.cmo3`。

官方 Physics 设置入口、输入/输出和 FPS 规则见 [About Physics](https://docs.live2d.com/en/cubism-editor-manual/physics-operation/)。

## 7. 表情文件（exp3）

1. `File > New > Animation` 新建场景，建议保存为 `avatar_exp.can3`；在 `Inspector` 设置 Scene name、Tag，并把 Duration 设为 **2 帧**（1 帧不能导出表达式 motion）。
2. 将模型拖到 Timeline；只给表情相关参数打 key（眉、眼笑、嘴型、脸红等），不要给 Angle XY、头发摆动等动作参数打 key。运行时会移动的 `MouthOpenY`、`EyeBallX/Y` 通常留在默认值，除非产品明确需要。
3. `File > Export Embedded File > Export motion file` 导出表情用 `motion3.json`。
4. 启动 Cubism Viewer（for OW），拖入 `.moc3`；`File > Import > Expression Motion (motion3.json / exp3.json)` 导入所有表情 motion；在 Resource 区逐个设置淡入时间和 Multiply mode。
5. `File > Export > All facial expression motions` 批量导出为 `.exp3.json`；或右键单个表情 `Save` 覆盖导出。最后用 `File > Export > Model Settings (model3.json)` 将 expressions 列表写入 model3。

官方：[Create Facial Expressions in Animation View](https://docs.live2d.com/en/cubism-editor-manual/create-facial-expressions/) · [Expression Settings and Export](https://docs.live2d.com/en/cubism-editor-manual/setting-and-exporting-facial-expressions/)

## 8. 制作动作 motion3

1. `File > New > Animation` 新建场景并保存为 `motions/idle.can3`；在 `Inspector` 设置 Scene name、Tag、帧率（常用 30 或 60 fps）和 Duration（如 180 帧=3 秒）。
2. 把 `.cmo3` 或已导出的模型拖到 `Timeline`，在 `Model loading and placement` 调整画布位置和缩放。
3. 在 Timeline 选模型轨道，展开参数；将播放头移到 0 帧，在参数行右键 `Add keyframe`（或 `Record parameter operations` 后拖动参数自动记录）。
4. 在 30/60/90/120/150/180 帧分别设置呼吸、轻微头部/身体摆动、眼睛开合等关键帧。选中关键帧拖动可改时间；右键可复制/粘贴或删除。
5. 打开 `Graph Editor` 调整曲线：慢入慢出用平滑/Bezier，机械循环用线性；在末帧复制首帧以闭合循环。用 Timeline 播放和 Onion Skin 检查抖动。
6. 需要淡入淡出：在 motion 导出设置中填写 Fade in/Fade out（毫秒）；Viewer 播放时检查与其他 motion 过渡无突跳。
7. `File > Export Embedded File > Export motion file`，选择目标 SDK/帧率、输出路径，导出 `idle.motion3.json`。若由运行时实时处理眨眼/口型，不勾选烘焙；要完全复现编辑器预览则勾选 `Burn Lip-sync and Blink into Motion`。

官方动画流程：[Animation Preparation](https://docs.live2d.com/en/cubism-editor-manual/animation-preparation/) · [Create a form animation](https://docs.live2d.com/en/cubism-editor-manual/create-form-animation/) · [Create Facial Expressions](https://docs.live2d.com/en/cubism-editor-manual/create-facial-expressions/)

## 9. 导出嵌入数据

### 9.1 MOC3、纹理和 model3

1. 回到 Modeling 工作区，先在 Parts palette 显示要导出的部件、隐藏不需要的部件；确认 Texture Atlas 已保存。
2. `File > Export Embedded File > Export as MOC3 file`。
3. 在 Export settings：`Export version` 选择目标 SDK；`Export target` 选 `1/1` 保持纹理原尺寸；勾选 `Export physics settings file (physics3.json)`、`Export display information file (cdi3.json)`（需要参数/部件名称映射时），按需勾选 user data/motion-sync；点击 `OK`。
4. 输出应包含 `.moc3`、`.model3.json` 和纹理 PNG；model3 中的 FileReferences 相对路径必须指向实际文件。不要移动单个 PNG 而不更新路径。

### 9.2 单独导出 Physics / motion

- Physics：`File > Export Embedded File > Export Physics settings`，生成 `.physics3.json`。
- Motion：Animation 工作区 `File > Export Embedded File > Export motion file`，生成 `.motion3.json`。
- CDI：在 MOC3 导出设置勾选 `Export display information file (cdi3.json)`；这是给 AE 插件等读取名称/参数链接的映射文件，并非运行时必需。

官方：[Exporting MOC3 files and model3.json](https://docs.live2d.com/en/cubism-editor-manual/export-moc3-motion3-files/) · [Export Model Settings](https://docs.live2d.com/en/cubism-editor-manual/export-model3-json/)

## 10. Viewer 验收

1. 启动与 Editor 同目录的 `CubismViewer.exe`（Cubism Viewer for OW）；拖入 `avatar.moc3` 或 `avatar.model3.json`。
2. `File > Import > Motion` 导入 `idle.motion3.json`；`File > Import > Expression Motion (motion3.json / exp3.json)` 导入表情。
3. 在 `Animation` 菜单逐项打开/关闭 `Enable facial expressions`、`Automatic Eye-blinking`、`Enable Physics`、`Enable Pose Switching`、`Breath`；拖动 `Cursor tracking` 检查 Angle X/Y/Z 和眼球参数。
4. `Show > Display of collision detection` 显示碰撞体，确认头发不会穿脸；观察帧率、纹理白边、遮罩边缘和动作循环。
5. 点击 `File > Export > Model Settings (model3.json)` 保存 Viewer 中的 expressions/pose/motion 列表；带 `*` 的资源表示尚未保存。

**交付验收清单**：

- [ ] PSD 为 RGB/8bit/sRGB，层名唯一且每个部件有补画；
- [ ] ArtMesh 网格覆盖像素，眼球有 Clipping Mask；
- [ ] Deformer 树无孤儿、父子方向正确；
- [ ] Angle X/Y/Z、EyeOpen、EyeBall、MouthOpen/Form、Breath 至少各有可用 keyform；
- [ ] Physics 组无红色顺序警告，60 fps 预览稳定；
- [ ] Viewer 能加载 model3/moc3、纹理、physics3，动作和表情均可播放；
- [ ] 所有 JSON 的相对路径在干净复制目录中仍有效。

## 11. 5.3.03 与 5.4 alpha/旧版差异

- 5.3.03 仍使用 `File > Export Embedded File` 这一组菜单；不要把 5.4 alpha 页面出现的新 Form Animation（FA）/Motion-sync 功能当成 5.3 必需步骤。
- Physics 的默认 `Calculate FPS` 在 Cubism 5 起为 60（旧版曾为 30）；导出前应在 `Modeling > Open Physics Settings` 明确确认。
- Viewer for OW 只加载嵌入数据（`.moc3`、`.model3.json`、`.motion3.json`、`.exp3.json`），不能直接加载编辑工程 `.cmo3`/`.can3`。
- 若官方页面写“Updated 2026/2025”且标题为 5.4 alpha，菜单与 5.3 相同的基础功能可沿用；仅 5.4 标注的新按钮/新算法不写入 5.3.03 操作路径。

