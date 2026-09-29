import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show rootBundle;

import '../services/session_service.dart';
import '../theme/app_theme.dart';

/// 登录 / 注册页（login-credential-seam）。identifier+密码双合一：未绑定即
/// 注册（绑定到当前匿名身份，记忆零迁移），已绑定即登录（切换账号）。
///
/// 布局为品牌区 + 浅色表单卡：移动端竖屏品牌区在上、表单在下；宽屏
/// （>900）左右分栏。品牌区背景图走约定路径 assets/images/login-bg.jpg
/// （ADR-0020 终稿图位）：文件存在即显示，不存在落晚霞渐变占位——占位态
/// 本身成立（暖色陪伴气质）。
class LoginScreen extends StatefulWidget {
  final SessionService sessions;
  final String baseUrl;
  final String deviceId;

  const LoginScreen({
    super.key,
    required this.sessions,
    required this.baseUrl,
    required this.deviceId,
  });

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  static const _bgAssetPath = 'assets/images/login-bg.jpg';
  static const _wideBreakpoint = 900.0;

  final _identifierController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _agreed = false;
  bool _obscure = true;
  bool _submitting = false;
  bool _hasBackgroundImage = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _probeBackgroundAsset();
  }

  @override
  void dispose() {
    _identifierController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  // 图位探测：assets/images/login-bg.jpg 在 manifest 里（即文件已放进约定
  // 路径）才切到背景图，否则保持晚霞渐变占位。
  Future<void> _probeBackgroundAsset() async {
    try {
      await rootBundle.load(_bgAssetPath);
    } catch (_) {
      return; // 图位未就位：占位渐变兜底，属预期路径。
    }
    if (mounted) {
      setState(() => _hasBackgroundImage = true);
    }
  }

  // 后端校验 identifier（邮箱或 3-32 位用户名）与密码（>=7），客户端只拦
  // 空值，不拦纯用户名。
  bool get _formValid =>
      _identifierController.text.trim().isNotEmpty &&
      _passwordController.text.length >= 7 &&
      _agreed;

  void _goBack() {
    final nav = Navigator.of(context);
    if (nav.canPop()) nav.pop();
  }

  Future<void> _submit() async {
    if (!_formValid || _submitting) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      final credentials = await widget.sessions.login(
        baseUrl: widget.baseUrl,
        deviceId: widget.deviceId,
        identifier: _identifierController.text,
        password: _passwordController.text,
      );
      if (!mounted) return;
      Navigator.pop(context, credentials);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _error = error is SessionException && error.statusCode == 401
            ? '账号或密码不正确'
            : '登录失败，请稍后再试';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: LayoutBuilder(
        builder: (context, constraints) {
          if (constraints.maxWidth > _wideBreakpoint) {
            return Row(
              children: [
                Expanded(
                  flex: 3,
                  child: _brandPanel(
                    padding: const EdgeInsets.fromLTRB(56, 48, 56, 56),
                    big: true,
                  ),
                ),
                Expanded(flex: 2, child: _formPanel()),
              ],
            );
          }
          // 移动端竖屏：品牌区在上、表单在下；品牌区圆角压在表单卡前。
          final topInset = MediaQuery.of(context).padding.top;
          return SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                ClipRRect(
                  borderRadius: const BorderRadius.vertical(
                    bottom: Radius.circular(28),
                  ),
                  child: SizedBox(
                    height: 300,
                    child: _brandPanel(
                      padding: EdgeInsets.fromLTRB(20, topInset + 8, 20, 40),
                    ),
                  ),
                ),
                _formCard,
              ],
            ),
          );
        },
      ),
    );
  }

  // ---- 品牌区（晚霞渐变占位 / 约定路径背景图 + 暗色遮罩） ----

  /// [big] 为宽屏分栏态：标语字号放大、渐变整幅铺开。返回键只在竖屏
  /// 品牌区出现（宽屏时挂在右侧表单面板上）。
  Widget _brandPanel({required EdgeInsets padding, bool big = false}) {
    return Container(
      padding: padding,
      decoration: _hasBackgroundImage
          ? const BoxDecoration(
              image: DecorationImage(
                image: AssetImage(_bgAssetPath),
                fit: BoxFit.cover,
              ),
            )
          : const BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                stops: [0, 0.38, 0.62, 1],
                colors: [
                  AppColors.championBlueDeep, // 黄昏天顶
                  AppColors.orangeDeep, // 晚霞主色带
                  AppColors.orange, // 落日辉光
                  Color(0xFF43150A), // 暮色地面
                ],
              ),
            ),
      // 遮罩：压暗图位（或补足占位对比），保品牌文字可读。
      foregroundDecoration: _hasBackgroundImage
          ? const BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                stops: [0, 0.55, 1],
                colors: [
                  Color(0x59070B12),
                  Color(0x8C070B12),
                  Color(0xCC43150A),
                ],
              ),
            )
          : const BoxDecoration(
              // 占位态叠加落日径向辉光，晚霞气质。
              gradient: RadialGradient(
                center: Alignment(0.55, 0.1),
                radius: 0.9,
                colors: [Color(0x40F2C94C), Color(0x00F2C94C)],
              ),
            ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (!big)
            Align(
                alignment: Alignment.centerLeft,
                child: _backButton(onPaper: false)),
          const Spacer(),
          Text(
            '嗨，来了。先看球。',
            style: TextStyle(
              fontSize: big ? 46 : 34,
              height: 1.25,
              fontWeight: FontWeight.w900,
              letterSpacing: -0.5,
              color: AppColors.ink,
              fontFamily: AppFonts.body,
            ),
          ),
          const SizedBox(height: 10),
          Text(
            '球球 QIUQIU · AI 足球陪看',
            style: TextStyle(
              fontSize: 13,
              letterSpacing: 2,
              color: Colors.white.withValues(alpha: 0.72),
              fontFamily: AppFonts.body,
            ),
          ),
        ],
      ),
    );
  }

  // ---- 表单侧（浅色卡，暖色陪伴气质） ----

  Widget get _formCard {
    // Material 而非 Container(color:)：ListTile 的墨水涟漪画在最近的
    // Material 上，中间夹 ColoredBox 会被 debug assert 拦下。
    return Material(
      color: AppColors.paper,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(24, 30, 24, 36),
        child: _formColumn,
      ),
    );
  }

  Widget _formPanel() {
    return Material(
      color: AppColors.paper,
      child: SafeArea(
        top: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.only(left: 8, top: 8),
              child: Align(
                alignment: Alignment.centerLeft,
                child: _backButton(onPaper: true),
              ),
            ),
            Expanded(
              child: Center(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.fromLTRB(40, 24, 40, 40),
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 420),
                    child: _formColumn,
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget get _formColumn {
    final inkSoft = AppColors.paperInk.withValues(alpha: 0.62);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text(
          '登录球球',
          style: TextStyle(
            fontSize: 24,
            fontWeight: FontWeight.w800,
            color: AppColors.paperInk,
          ),
        ),
        const SizedBox(height: 6),
        Text(
          '登录后，你的看球记忆、画像和赛程订阅'
          '会在换机或重装后原样保留。',
          style: TextStyle(fontSize: 14, height: 1.55, color: inkSoft),
        ),
        const SizedBox(height: 20),
        TextField(
          controller: _identifierController,
          keyboardType: TextInputType.text,
          autofillHints: const [AutofillHints.username, AutofillHints.email],
          decoration: _lightInputDecoration(
            '账号',
            hint: '邮箱或用户名',
          ),
          onChanged: (_) => setState(() {}),
        ),
        const SizedBox(height: AppSpacing.md),
        TextField(
          controller: _passwordController,
          obscureText: _obscure,
          autofillHints: const [AutofillHints.password],
          decoration: _lightInputDecoration('密码（至少 7 位）').copyWith(
            suffixIcon: IconButton(
              icon: Icon(
                _obscure
                    ? Icons.visibility_outlined
                    : Icons.visibility_off_outlined,
                size: 20,
                color: inkSoft,
              ),
              onPressed: () => setState(() => _obscure = !_obscure),
            ),
          ),
          onChanged: (_) => setState(() {}),
        ),
        CheckboxListTile(
          value: _agreed,
          onChanged: (value) => setState(() => _agreed = value ?? false),
          controlAffinity: ListTileControlAffinity.leading,
          contentPadding: EdgeInsets.zero,
          dense: true,
          activeColor: AppColors.orange,
          title: Text(
            '我已阅读并同意用户协议与隐私政策',
            style: TextStyle(
              fontSize: 13,
              color: AppColors.paperInk.withValues(alpha: 0.78),
            ),
          ),
        ),
        if (_error != null) ...[
          Align(
            alignment: Alignment.centerLeft,
            child: Text(
              _error!,
              style: const TextStyle(
                fontSize: 13,
                color: AppColors.red,
              ),
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
        ],
        const SizedBox(height: AppSpacing.xs),
        FilledButton(
          onPressed: _formValid && !_submitting ? _submit : null,
          style: FilledButton.styleFrom(
            backgroundColor: AppColors.orange,
            foregroundColor: Colors.white,
            disabledBackgroundColor: AppColors.orange.withValues(alpha: 0.38),
            disabledForegroundColor: Colors.white.withValues(alpha: 0.85),
            minimumSize: const Size.fromHeight(52),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(10),
            ),
          ),
          child: _submitting
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('登录 / 注册'),
        ),
        const SizedBox(height: AppSpacing.md),
        Text(
          '首次使用会直接为你创建账号；'
          '已有账号输入原密码即可进入。',
          style: TextStyle(fontSize: 13, height: 1.5, color: inkSoft),
        ),
      ],
    );
  }

  InputDecoration _lightInputDecoration(String label, {String? hint}) {
    const radius = BorderRadius.all(Radius.circular(10));
    const side = BorderSide(color: Color(0xFFDED7C8));
    return InputDecoration(
      labelText: label,
      hintText: hint,
      hintStyle: TextStyle(
        fontSize: 14,
        color: AppColors.paperInk.withValues(alpha: 0.38),
      ),
      labelStyle: TextStyle(
        fontSize: 14,
        color: AppColors.paperInk.withValues(alpha: 0.62),
      ),
      filled: true,
      fillColor: Colors.white,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      border: const OutlineInputBorder(borderRadius: radius, borderSide: side),
      enabledBorder: const OutlineInputBorder(
        borderRadius: radius,
        borderSide: side,
      ),
      focusedBorder: const OutlineInputBorder(
        borderRadius: radius,
        borderSide: BorderSide(color: AppColors.orange, width: 2),
      ),
    );
  }

  Widget _backButton({required bool onPaper}) {
    return IconButton(
      onPressed: _goBack,
      style: IconButton.styleFrom(
        backgroundColor:
            onPaper ? Colors.transparent : Colors.black.withValues(alpha: 0.24),
        shape: const CircleBorder(),
      ),
      icon: Icon(
        Icons.arrow_back,
        size: 22,
        color: onPaper ? AppColors.paperInk : Colors.white,
      ),
    );
  }
}
