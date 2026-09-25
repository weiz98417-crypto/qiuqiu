import 'package:flutter/material.dart';

import '../services/preferences_service.dart';
import '../services/session_service.dart';
import 'login_screen.dart';
import '../theme/app_theme.dart';

class SettingsScreen extends StatefulWidget {
  final UserProfile initialProfile;
  final Future<void> Function(UserProfile) onSave;

  /// Opens the 球球懂我 page (C3); the entry hides when the caller cannot
  /// provide the session-backed portrait service.
  final VoidCallback? onOpenPortrait;
  /// 话痨档位变更回调（保存后触发）：走 WS set_talkativeness 即时持久化。
  final void Function(String tier)? onTalkativenessChanged;
  /// 登录凭证缝（ADR-0020）：提供即展示「账号与同步」段。
  final SessionService? sessions;
  final String baseUrl;
  final String deviceId;

  const SettingsScreen({
    super.key,
    required this.initialProfile,
    required this.onSave,
    this.onTalkativenessChanged,
    this.onOpenPortrait,
    this.sessions,
    this.baseUrl = '',
    this.deviceId = '',
  });

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _nicknameController;
  late UserProfile _draft;
  bool _saving = false;
  final ValueNotifier<String?> _loginIdentifier = ValueNotifier(null);
  bool _submittingLogout = false;

  @override
  void initState() {
    super.initState();
    _nicknameController = TextEditingController(
      text: widget.initialProfile.nickname,
    );
    _draft = widget.initialProfile;
    widget.sessions?.savedIdentifier().then((identifier) {
      _loginIdentifier.value = (identifier == null || identifier.isEmpty)
          ? null
          : identifier;
    });
  }

  Future<void> _openLogin() async {
    final sessions = widget.sessions;
    if (sessions == null) return;
    await Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => LoginScreen(
          sessions: sessions,
          baseUrl: widget.baseUrl,
          deviceId: widget.deviceId,
        ),
      ),
    );
    _loginIdentifier.value = await sessions.savedIdentifier();
  }

  Future<void> _confirmLogout() async {
    final sessions = widget.sessions;
    if (sessions == null || _submittingLogout) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('退出登录？'),
        content: const Text(
          '退出后球球在本机还是认识你，'
          '但换设备前将无法通过邮箱找回记忆。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('退出'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    setState(() => _submittingLogout = true);
    await sessions.logout(baseUrl: widget.baseUrl);
    if (!mounted) return;
    setState(() => _submittingLogout = false);
    _loginIdentifier.value = await sessions.savedIdentifier();
  }

  @override
  void dispose() {
    _loginIdentifier.dispose();
    _nicknameController.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_saving) return;
    setState(() => _saving = true);
    final profile = _draft
        .copyWith(nickname: _nicknameController.text.trim())
        .ensureOutputAvailable();
    await widget.onSave(profile);
    if (profile.talkativeness != widget.initialProfile.talkativeness) {
      widget.onTalkativenessChanged?.call(profile.talkativeness);
    }
    if (!mounted) return;
    Navigator.pop(context, profile);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('陪看设置')),
      body: SafeArea(
        top: false,
        child: Align(
          alignment: Alignment.topCenter,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 520),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.md,
                AppSpacing.xs,
                AppSpacing.md,
                AppSpacing.lg,
              ),
              children: [
                Text(
                  '把声量交给你',
                  style: Theme.of(context).textTheme.headlineMedium,
                ),
                const SizedBox(height: AppSpacing.xs),
                Text(
                  '球球会记住你的偏好，但不会把技术设置带进看球。',
                  style: Theme.of(
                    context,
                  ).textTheme.bodyMedium?.copyWith(color: AppColors.muted),
                ),
                const SizedBox(height: AppSpacing.xl),
                const _SectionLabel(index: '01', label: '称呼'),
                const SizedBox(height: AppSpacing.sm),
                TextField(
                  controller: _nicknameController,
                  maxLength: 20,
                  textInputAction: TextInputAction.done,
                  decoration: const InputDecoration(
                    labelText: '你的昵称',
                    hintText: '球球怎么称呼你',
                  ),
                ),
                const SizedBox(height: AppSpacing.lg),
                const _SectionLabel(index: '02', label: '话痨程度'),
                const SizedBox(height: AppSpacing.sm),
                SegmentedButton<String>(
                  showSelectedIcon: false,
                  segments: const [
                    ButtonSegment(value: 'quiet', label: Text('安静')),
                    ButtonSegment(value: 'normal', label: Text('刚好')),
                    ButtonSegment(value: 'active', label: Text('热闹')),
                  ],
                  selected: {_draft.talkativeness},
                  onSelectionChanged: (selection) {
                    setState(() {
                      _draft = _draft.copyWith(talkativeness: selection.first);
                    });
                  },
                ),
                const SizedBox(height: AppSpacing.xl),
                _PreferenceSwitch(
                  index: '03',
                  title: '连续对话',
                  subtitle: '进入陪看后自然接着聊，也可以随时关掉',
                  value: _draft.continuousConversation,
                  onChanged: (value) {
                    setState(() {
                      _draft = _draft.copyWith(continuousConversation: value);
                    });
                  },
                ),
                _PreferenceSwitch(
                  index: '04',
                  title: '抢话打断',
                  subtitle: '球球说话时你直接开口，她会停下来听你说',
                  value: _draft.duplexPlaybackCapture,
                  onChanged: (value) {
                    setState(() {
                      _draft = _draft.copyWith(duplexPlaybackCapture: value);
                    });
                  },
                ),
                _PreferenceSwitch(
                  index: '05',
                  title: '字幕',
                  subtitle: '把球球说的话同步显示在画面里',
                  value: _draft.subtitlesEnabled,
                  onChanged: (value) {
                    setState(() {
                      _draft = _draft.withSubtitlesEnabled(value);
                    });
                  },
                ),
                _PreferenceSwitch(
                  index: '06',
                  title: '声音',
                  subtitle: '关闭后仍保留字幕和文字对话',
                  value: _draft.soundEnabled,
                  onChanged: (value) {
                    setState(() {
                      _draft = _draft.withSoundEnabled(value);
                    });
                  },
                ),
                // 气氛感知（ambient-audio-observation 6.3）：只做说明、不做
                // 独立开关——气氛旁路跟随观赛会话生效，sidecar 摘除后旁路
                // 静默消失；文案即 design.md 的隐私口径。
                const _PreferenceNote(
                  index: '07',
                  title: '气氛感知',
                  subtitle:
                      '观看比赛时，球球会分析现场声音的气氛（欢呼/嘘声）来陪你看球；音频不会被保存，比赛结束后不留任何声音记录。',
                ),
                const SizedBox(height: AppSpacing.lg),
                const _SectionLabel(index: '08', label: '支持球队'),
                const SizedBox(height: AppSpacing.sm),
                DropdownMenu<String>(
                  width: double.infinity,
                  initialSelection:
                      _draft.favoriteTeam.isEmpty ? '利物浦' : _draft.favoriteTeam,
                  label: const Text('主队偏好'),
                  dropdownMenuEntries: const [
                    DropdownMenuEntry(value: '利物浦', label: '利物浦'),
                    DropdownMenuEntry(value: '切尔西', label: '切尔西'),
                    DropdownMenuEntry(value: '中立', label: '中立看球'),
                  ],
                  onSelected: (value) {
                    if (value == null) return;
                    setState(() {
                      _draft = _draft.copyWith(favoriteTeam: value);
                    });
                  },
                ),
                const SizedBox(height: AppSpacing.xxl),
                FilledButton(
                  onPressed: _saving ? null : _save,
                  child: Text(_saving ? '正在保存…' : '保存陪看偏好'),
                ),
                if (widget.onOpenPortrait != null) ...[
                  const SizedBox(height: AppSpacing.lg),
                  const _SectionLabel(index: '09', label: '球球懂我'),
                  const SizedBox(height: AppSpacing.xs),
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('看看球球眼中的你'),
                    subtitle: const Text('球球记住的印象可以修改，也可以让它忘掉'),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: widget.onOpenPortrait,
                  ),
                ],
                if (widget.sessions != null) ...[
                  const SizedBox(height: AppSpacing.lg),
                  const _SectionLabel(index: '10', label: '账号与同步'),
                  const SizedBox(height: AppSpacing.xs),
                  ValueListenableBuilder<String?>(
                    valueListenable: _loginIdentifier,
                    builder: (context, identifier, _) => Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        ListTile(
                          contentPadding: EdgeInsets.zero,
                          title: Text(identifier ?? '未登录'),
                          subtitle: Text(
                            identifier == null
                                ? '登录后，换机或重装也能找回你的看球记忆'
                                : '你的看球记忆已与这个邮箱同步',
                          ),
                        ),
                        const SizedBox(height: AppSpacing.sm),
                        if (identifier == null)
                          OutlinedButton(
                            onPressed: _openLogin,
                            child: const Text('登录 / 注册'),
                          )
                        else
                          OutlinedButton(
                            onPressed: _submittingLogout ? null : _confirmLogout,
                            child: Text(
                              _submittingLogout ? '正在退出…' : '退出登录',
                            ),
                          ),
                      ],
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SectionLabel extends StatelessWidget {
  final String index;
  final String label;

  const _SectionLabel({required this.index, required this.label});

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        SizedBox(
          width: 40,
          child: Text(
            index,
            style: Theme.of(context).textTheme.titleLarge?.copyWith(
              color: AppColors.orange,
              fontFeatures: const [FontFeature.tabularFigures()],
            ),
          ),
        ),
        Text(label, style: Theme.of(context).textTheme.titleLarge),
      ],
    );
  }
}

/// 说明行：与 _PreferenceSwitch 同版式但不带开关——用于跟随会话生效、
/// 不提供独立开关的能力说明（如气氛感知的隐私口径）。
class _PreferenceNote extends StatelessWidget {
  final String index;
  final String title;
  final String subtitle;

  const _PreferenceNote({
    required this.index,
    required this.title,
    required this.subtitle,
  });

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: const BoxDecoration(
        border: Border(top: BorderSide(color: AppColors.line)),
      ),
      child: ListTile(
        contentPadding: EdgeInsets.zero,
        minTileHeight: 72,
        title: Row(
          children: [
            SizedBox(
              width: 40,
              child: Text(
                index,
                style: Theme.of(context).textTheme.labelLarge?.copyWith(
                  color: AppColors.orange,
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ),
            Text(title),
          ],
        ),
        subtitle: Padding(
          padding: const EdgeInsets.only(left: 40, top: AppSpacing.xxs),
          child: Text(subtitle),
        ),
      ),
    );
  }
}

class _PreferenceSwitch extends StatelessWidget {
  final String index;
  final String title;
  final String subtitle;
  final bool value;
  final ValueChanged<bool> onChanged;

  const _PreferenceSwitch({
    required this.index,
    required this.title,
    required this.subtitle,
    required this.value,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: const BoxDecoration(
        border: Border(top: BorderSide(color: AppColors.line)),
      ),
      child: SwitchListTile(
        contentPadding: EdgeInsets.zero,
        minTileHeight: 72,
        title: Row(
          children: [
            SizedBox(
              width: 40,
              child: Text(
                index,
                style: Theme.of(context).textTheme.labelLarge?.copyWith(
                  color: AppColors.orange,
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ),
            Text(title),
          ],
        ),
        subtitle: Padding(
          padding: const EdgeInsets.only(left: 40, top: AppSpacing.xxs),
          child: Text(subtitle),
        ),
        value: value,
        onChanged: onChanged,
      ),
    );
  }
}
