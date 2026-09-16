import 'package:flutter/material.dart';

import '../services/portrait_service.dart';
import '../theme/app_theme.dart';

/// 球球懂我 — the user-facing portrait page (C3). Lists the real stored
/// portrait entries, edits text inline, and deletes with confirmation; the
/// backend applies deletes as tombstones so the very next turn forgets.
class PortraitScreen extends StatefulWidget {
  final PortraitService service;

  const PortraitScreen({super.key, required this.service});

  @override
  State<PortraitScreen> createState() => _PortraitScreenState();
}

class _PortraitScreenState extends State<PortraitScreen> {
  PortraitData? _data;
  String? _error;
  bool _loading = true;
  bool _busy = false;
  String? _notice;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final data = await widget.service.load();
      if (!mounted) return;
      setState(() {
        _data = data;
        _loading = false;
      });
    } on PortraitException catch (error) {
      if (!mounted) return;
      setState(() {
        _error = error.message;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _error = '球球这边暂时没连上，稍后再试试';
        _loading = false;
      });
    }
  }

  Future<void> _editEntry(PortraitEntryData entry) async {
    final controller = TextEditingController(text: entry.content);
    final updated = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: AppColors.terrace,
        title: Text('修改「${entry.label}」'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 120,
          decoration: const InputDecoration(
            labelText: '球球该记住什么',
            hintText: '用一句话告诉球球',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () =>
                Navigator.pop(dialogContext, controller.text.trim()),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    if (updated == null || updated.isEmpty || updated == entry.content) {
      return;
    }
    await _run(() => widget.service.edit(
          topic: entry.topic,
          subTopic: entry.subTopic,
          content: updated,
          entryId: entry.id,
        ));
  }

  Future<void> _forgetEntry(PortraitEntryData entry) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: AppColors.terrace,
        title: Text('让球球忘掉「${entry.label}」吗？'),
        content: const Text('删除后，下一句聊天开始球球就不会再提这件事。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('再想想'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: AppColors.red,
            ),
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('忘掉'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await _run(() => widget.service.forgetOne(
          topic: entry.topic,
          subTopic: entry.subTopic,
          entryId: entry.id,
        ));
  }

  Future<void> _forgetAll() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: AppColors.terrace,
        title: const Text('让球球忘掉这里的一切吗？'),
        content: const Text('球球对你的印象会全部清空，从下一句聊天开始生效。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('再想想'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: AppColors.red,
            ),
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('全部忘掉'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    await _run(() => widget.service.forgetAll());
  }

  Future<void> _run(Future<PortraitData> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _notice = null;
    });
    try {
      final data = await action();
      if (!mounted) return;
      setState(() {
        _data = data;
        _busy = false;
      });
    } on PortraitException catch (error) {
      if (!mounted) return;
      setState(() {
        _notice = error.message;
        _busy = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _notice = '操作没有成功，稍后再试试';
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('球球懂我'),
        actions: [
          if (_data != null && _data!.isNotEmpty)
            IconButton(
              tooltip: '全部忘掉',
              onPressed: _busy ? null : _forgetAll,
              icon: const Icon(Icons.auto_delete_outlined),
            ),
        ],
      ),
      body: SafeArea(
        top: false,
        child: _buildBody(context),
      ),
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_error!, style: Theme.of(context).textTheme.bodyMedium),
            const SizedBox(height: AppSpacing.md),
            FilledButton(onPressed: _load, child: const Text('重试')),
          ],
        ),
      );
    }
    final data = _data;
    if (data == null || data.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.lg),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.sports_soccer, size: 48, color: AppColors.muted),
              const SizedBox(height: AppSpacing.md),
              Text('球球还在慢慢了解你',
                  style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: AppSpacing.xs),
              Text(
                '一起看几场球，球球会记住你的喜好；这里也会一点点丰富起来。',
                textAlign: TextAlign.center,
                style: Theme.of(context)
                    .textTheme
                    .bodyMedium
                    ?.copyWith(color: AppColors.muted),
              ),
            ],
          ),
        ),
      );
    }

    final sections = <String, List<PortraitEntryData>>{};
    for (final entry in data.entries) {
      sections.putIfAbsent(entry.topicLabel, () => []).add(entry);
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.md,
          AppSpacing.sm,
          AppSpacing.md,
          AppSpacing.lg,
        ),
        children: [
          Text(
            '这些是球球记住的关于你的印象，聊天时会自然提起。你可以修改，也可以让球球忘掉——改动立刻生效。',
            style: Theme.of(context)
                .textTheme
                .bodyMedium
                ?.copyWith(color: AppColors.muted),
          ),
          if (_notice != null) ...[
            const SizedBox(height: AppSpacing.sm),
            Text(
              _notice!,
              style: Theme.of(context)
                  .textTheme
                  .bodyMedium
                  ?.copyWith(color: AppColors.red),
            ),
          ],
          const SizedBox(height: AppSpacing.md),
          if (data.updatedAt != null) ...[
            Text(
              '画像更新：${_formatDate(data.updatedAt!)}',
              style: Theme.of(context)
                  .textTheme
                  .labelMedium
                  ?.copyWith(color: AppColors.muted),
            ),
            const SizedBox(height: AppSpacing.sm),
          ],
          for (final section in sections.entries) ...[
            _SectionHeader(label: section.key),
            const SizedBox(height: AppSpacing.xs),
            for (final entry in section.value)
              _PortraitTile(
                entry: entry,
                busy: _busy,
                onEdit: () => _editEntry(entry),
                onForget: () => _forgetEntry(entry),
              ),
            const SizedBox(height: AppSpacing.md),
          ],
        ],
      ),
    );
  }

  static String _formatDate(DateTime value) {
    final local = value.toLocal();
    String two(int number) => number.toString().padLeft(2, '0');
    return '${local.year.toString().padLeft(4, '0')}-${two(local.month)}-${two(local.day)}';
  }
}

class _SectionHeader extends StatelessWidget {
  final String label;

  const _SectionHeader({required this.label});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.only(top: AppSpacing.xs),
      decoration: const BoxDecoration(
        border: Border(top: BorderSide(color: AppColors.line)),
      ),
      child: Row(
        children: [
          Text(label, style: Theme.of(context).textTheme.titleLarge),
        ],
      ),
    );
  }
}

class _PortraitTile extends StatelessWidget {
  final PortraitEntryData entry;
  final bool busy;
  final VoidCallback onEdit;
  final VoidCallback onForget;

  const _PortraitTile({
    required this.entry,
    required this.busy,
    required this.onEdit,
    required this.onForget,
  });

  @override
  Widget build(BuildContext context) {
    final updatedAt = entry.updatedAt;
    return Container(
      margin: const EdgeInsets.only(bottom: AppSpacing.sm),
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.md,
        vertical: AppSpacing.sm,
      ),
      decoration: BoxDecoration(
        color: AppColors.terrace,
        border: Border.all(color: AppColors.line),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text(
                      entry.label,
                      style: Theme.of(context).textTheme.labelLarge?.copyWith(
                            color: AppColors.orange,
                          ),
                    ),
                    if (entry.isUserEdited) ...[
                      const SizedBox(width: AppSpacing.xs),
                      Text(
                        '你修改的',
                        style: Theme.of(context)
                            .textTheme
                            .labelMedium
                            ?.copyWith(color: AppColors.skyBlue),
                      ),
                    ],
                  ],
                ),
                const SizedBox(height: AppSpacing.xxs),
                Text(entry.content, style: Theme.of(context).textTheme.bodyLarge),
                if (updatedAt != null) ...[
                  const SizedBox(height: AppSpacing.xxs),
                  Text(
                    '更新于 ${_PortraitScreenState._formatDate(updatedAt)}',
                    style: Theme.of(context)
                        .textTheme
                        .labelMedium
                        ?.copyWith(color: AppColors.muted),
                  ),
                ],
              ],
            ),
          ),
          IconButton(
            tooltip: '修改',
            onPressed: busy ? null : onEdit,
            icon: const Icon(Icons.edit_outlined),
          ),
          IconButton(
            tooltip: '让球球忘掉',
            onPressed: busy ? null : onForget,
            icon: const Icon(Icons.delete_outline),
          ),
        ],
      ),
    );
  }
}
