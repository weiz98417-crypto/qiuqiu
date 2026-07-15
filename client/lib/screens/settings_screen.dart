import 'package:flutter/material.dart';

import '../services/preferences_service.dart';
import '../theme/app_theme.dart';

class SettingsScreen extends StatefulWidget {
  final UserProfile initialProfile;
  final Future<void> Function(UserProfile) onSave;

  const SettingsScreen({
    super.key,
    required this.initialProfile,
    required this.onSave,
  });

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _nicknameController;
  late UserProfile _draft;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _nicknameController = TextEditingController(
      text: widget.initialProfile.nickname,
    );
    _draft = widget.initialProfile;
  }

  @override
  void dispose() {
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
                  index: '05',
                  title: '声音',
                  subtitle: '关闭后仍保留字幕和文字对话',
                  value: _draft.soundEnabled,
                  onChanged: (value) {
                    setState(() {
                      _draft = _draft.withSoundEnabled(value);
                    });
                  },
                ),
                const SizedBox(height: AppSpacing.lg),
                const _SectionLabel(index: '06', label: '支持球队'),
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
