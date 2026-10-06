import 'package:flutter/material.dart';
import 'package:share_plus/share_plus.dart';

import '../services/journal_service.dart';

/// 球友手记 + 赛季记忆册（teammate-journal）：手记列表（点赞/忘掉/分享）+
/// 赛季册装订视图（每场一条：比分+手记摘要+共同瞬间）。手记是阅读面——
/// 比分来自账本终场投影（后端确定性校验），删除=物理删（隐私生命周期）。
/// 分享=最小渲染面（文本卡片经系统分享，share_plus；10.0 核查：图片卡片
/// MVP 不存在，本页即最小分享面）。
class JournalScreen extends StatefulWidget {
  final JournalService service;

  const JournalScreen({super.key, required this.service});

  @override
  State<JournalScreen> createState() => _JournalScreenState();
}

class _JournalScreenState extends State<JournalScreen> {
  static const _tabJournal = 0;
  static const _tabAlbum = 1;

  List<JournalEntry>? _entries;
  List<AlbumPage>? _album;
  String? _error;
  bool _loading = true;
  String? _notice;
  int _tab = _tabJournal;

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
      final entries = await widget.service.fetch();
      List<AlbumPage>? album;
      try {
        album = await widget.service.fetchAlbum();
      } on JournalException {
        album = null; // 赛季册降级=空视图，不拖垮手记列表
      }
      if (!mounted) return;
      setState(() {
        _entries = entries;
        _album = album;
        _loading = false;
      });
    } on JournalException catch (error) {
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

  Future<void> _toggleLike(JournalEntry entry) async {
    final target = !entry.liked;
    setState(() {
      entry.liked = target;
    });
    try {
      await widget.service.setLiked(entry.id, target);
    } catch (_) {
      if (!mounted) return;
      setState(() {
        entry.liked = !target;
        _notice = '点赞没送上，稍后再试试';
      });
    }
  }

  Future<void> _forget(JournalEntry entry) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('忘掉这篇手记？'),
        content: const Text('删除后不会留存。'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(dialogContext, false), child: const Text('再想想')),
          TextButton(onPressed: () => Navigator.pop(dialogContext, true), child: const Text('忘掉')),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await widget.service.forget(entry.id);
      if (!mounted) return;
      setState(() {
        _entries?.removeWhere((item) => item.id == entry.id);
        _notice = '已忘掉这篇手记';
      });
      await _load();
    } on JournalException catch (error) {
      if (!mounted) return;
      setState(() {
        _notice = error.message;
      });
    }
  }

  void _share(JournalEntry entry) {
    final buffer = StringBuffer();
    buffer.write('【球球手记】');
    buffer.write('${entry.homeTeam} ${entry.score} ${entry.awayTeam}');
    if ((entry.season ?? '').isNotEmpty) buffer.write('（${entry.season} 赛季）');
    buffer.write('\n');
    buffer.write(entry.body);
    buffer.write('\n——和我一起看球的球球');
    Share.share(buffer.toString(), subject: '球球手记：${entry.homeTeam} vs ${entry.awayTeam}');
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('球友手记'),
        actions: [
          IconButton(onPressed: _load, icon: const Icon(Icons.refresh)),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            child: SegmentedButton<int>(
              segments: const [
                ButtonSegment(value: _tabJournal, label: Text('手记')),
                ButtonSegment(value: _tabAlbum, label: Text('赛季记忆册')),
              ],
              selected: {_tab},
              onSelectionChanged: (selection) => setState(() => _tab = selection.first),
            ),
          ),
          if (_notice != null)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Text(_notice!, style: Theme.of(context).textTheme.bodySmall),
            ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : _error != null
                    ? Center(
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(_error!),
                            TextButton(onPressed: _load, child: const Text('重试')),
                          ],
                        ),
                      )
                    : (_tab == _tabJournal ? _buildJournalList() : _buildAlbum()),
          ),
        ],
      ),
    );
  }

  Widget _buildJournalList() {
    final entries = _entries ?? const [];
    if (entries.isEmpty) {
      return const Center(child: Text('看完一场球，手记就会出现在这里。'));
    }
    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: entries.length,
      itemBuilder: (context, index) {
        final entry = entries[index];
        return Card(
          margin: const EdgeInsets.only(bottom: 12),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${entry.homeTeam} ${entry.score} ${entry.awayTeam}',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                    ),
                    if ((entry.season ?? '').isNotEmpty)
                      Text(entry.season!, style: Theme.of(context).textTheme.bodySmall),
                  ],
                ),
                const SizedBox(height: 8),
                Text(entry.body),
                if (entry.goals.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(
                    '进球：${entry.goals.join('、')}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
                const SizedBox(height: 8),
                Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    TextButton.icon(
                      onPressed: () => _toggleLike(entry),
                      icon: Icon(entry.liked ? Icons.favorite : Icons.favorite_border),
                      label: Text(entry.liked ? '记着呢' : '记下这篇'),
                    ),
                    TextButton.icon(
                      onPressed: () => _share(entry),
                      icon: const Icon(Icons.share),
                      label: const Text('分享'),
                    ),
                    TextButton.icon(
                      onPressed: () => _forget(entry),
                      icon: const Icon(Icons.delete_outline),
                      label: const Text('忘掉'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _buildAlbum() {
    final album = _album ?? const [];
    if (album.isEmpty) {
      return const Center(child: Text('赛季册还没有装订页，看一场就有了。'));
    }
    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: album.length,
      itemBuilder: (context, index) {
        final page = album[index];
        return Card(
          margin: const EdgeInsets.only(bottom: 12),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${page.homeTeam} ${page.score} ${page.awayTeam}',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                    ),
                    if ((page.season ?? '').isNotEmpty)
                      Text(page.season!, style: Theme.of(context).textTheme.bodySmall),
                  ],
                ),
                if ((page.journal ?? '').isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(page.journal!),
                ],
                if (page.moments.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    '共同瞬间：${page.moments.join('；')}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
                if (page.liked) ...[
                  const SizedBox(height: 6),
                  const Icon(Icons.favorite, size: 16),
                ],
              ],
            ),
          ),
        );
      },
    );
  }
}
