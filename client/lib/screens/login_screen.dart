import 'package:flutter/material.dart';

import '../services/session_service.dart';
import '../theme/app_theme.dart';

/// 登录 / 注册页（login-credential-seam）。邮箱+密码双合一：identifier 未
/// 绑定即注册（绑定到当前匿名身份，记忆零迁移），已绑定即登录（切换账号）。
/// 视觉为现有主题占位，最终稿等用户提供的背景图（ADR-0020）。
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
  final _identifierController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _agreed = false;
  bool _submitting = false;
  String? _error;

  @override
  void dispose() {
    _identifierController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  bool get _formValid =>
      _identifierController.text.contains('@') &&
      _passwordController.text.length >= 8 &&
      _agreed;

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
            ? '邮箱或密码不正确'
            : '登录失败，请稍后再试';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('登录球球')),
      body: SafeArea(
        top: false,
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: ListView(
              padding: const EdgeInsets.all(AppSpacing.lg),
              shrinkWrap: true,
              children: [
                Text(
                  '登录后，你的看球记忆、画像和赛程订阅'
                  '会在换机或重装后原样保留。',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                const SizedBox(height: AppSpacing.lg),
                TextField(
                  controller: _identifierController,
                  keyboardType: TextInputType.emailAddress,
                  autofillHints: const [AutofillHints.email],
                  decoration: const InputDecoration(
                    labelText: '邮箱',
                    border: OutlineInputBorder(),
                  ),
                  onChanged: (_) => setState(() {}),
                ),
                const SizedBox(height: AppSpacing.md),
                TextField(
                  controller: _passwordController,
                  obscureText: true,
                  autofillHints: const [AutofillHints.password],
                  decoration: const InputDecoration(
                    labelText: '密码（至少 8 位）',
                    border: OutlineInputBorder(),
                  ),
                  onChanged: (_) => setState(() {}),
                ),
                const SizedBox(height: AppSpacing.md),
                CheckboxListTile(
                  value: _agreed,
                  onChanged: (value) =>
                      setState(() => _agreed = value ?? false),
                  controlAffinity: ListTileControlAffinity.leading,
                  contentPadding: EdgeInsets.zero,
                  title: const Text(
                    '我已阅读并同意用户协议与隐私政策',
                    style: TextStyle(fontSize: 13),
                  ),
                ),
                if (_error != null) ...[
                  Text(
                    _error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                ],
                const SizedBox(height: AppSpacing.sm),
                FilledButton(
                  onPressed: _formValid && !_submitting ? _submit : null,
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
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
