import 'package:flutter/material.dart';

import '../services/match_catalog_service.dart';
import '../theme/app_theme.dart';

class MatchCatalogScreen extends StatefulWidget {
  final String apiBaseUrl;
  final MatchCatalogService? service;
  final ValueChanged<MatchCatalogItem> onSelected;

  const MatchCatalogScreen({
    super.key,
    required this.apiBaseUrl,
    required this.onSelected,
    this.service,
  });

  @override
  State<MatchCatalogScreen> createState() => _MatchCatalogScreenState();
}

class _MatchCatalogScreenState extends State<MatchCatalogScreen> {
  late final MatchCatalogService _service;
  late Future<List<MatchCatalogItem>> _matches;
  String _query = '';
  String _filter = 'all';

  @override
  void initState() {
    super.initState();
    _service = widget.service ?? MatchCatalogService();
    _matches = _service.fetch(widget.apiBaseUrl);
  }

  @override
  void dispose() {
    if (widget.service == null) _service.close();
    super.dispose();
  }

  Future<void> _retry() async {
    final request = _service.fetch(widget.apiBaseUrl);
    setState(() => _matches = request);
    try {
      await request;
    } catch (_) {
      // FutureBuilder renders the error state and exposes retry again.
    }
  }

  List<MatchCatalogItem> _visible(List<MatchCatalogItem> matches) {
    final query = _query.trim().toLowerCase();
    return matches.where((match) {
      if (_filter != 'all' && match.status != _filter) return false;
      if (query.isEmpty) return true;
      return '${match.homeTeam} ${match.awayTeam} ${match.competition}'
          .toLowerCase()
          .contains(query);
    }).toList(growable: false);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppColors.paper,
      appBar: AppBar(
        backgroundColor: AppColors.paper,
        foregroundColor: AppColors.paperInk,
        title: const Text('选择一场比赛'),
        actions: [
          IconButton(
            tooltip: '刷新比赛',
            onPressed: _retry,
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: SafeArea(
        child: FutureBuilder<List<MatchCatalogItem>>(
          future: _matches,
          builder: (context, snapshot) {
            if (snapshot.connectionState == ConnectionState.waiting) {
              return const Center(child: CircularProgressIndicator());
            }
            if (snapshot.hasError) {
              return _CatalogMessage(
                icon: Icons.cloud_off_rounded,
                title: '比赛列表暂时没加载出来',
                detail: '检查网络后重试，已经进入的比赛不会受影响。',
                actionLabel: '重新加载',
                onAction: _retry,
              );
            }
            final allMatches = snapshot.data ?? const [];
            final matches = _visible(allMatches);
            return Column(
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                  child: TextField(
                    onChanged: (value) => setState(() => _query = value),
                    decoration: const InputDecoration(
                      prefixIcon: Icon(Icons.search_rounded),
                      hintText: '搜索球队或赛事',
                    ),
                  ),
                ),
                SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  child: Row(
                    children: [
                      for (final filter in const [
                        ('all', '全部'),
                        ('live', '直播中'),
                        ('scheduled', '未开始'),
                        ('finished', '已结束'),
                      ])
                        Padding(
                          padding: const EdgeInsets.only(right: 8),
                          child: ChoiceChip(
                            label: Text(filter.$2),
                            selected: _filter == filter.$1,
                            onSelected: (_) =>
                                setState(() => _filter = filter.$1),
                          ),
                        ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
                Expanded(
                  child: allMatches.isEmpty
                      ? _CatalogMessage(
                          icon: Icons.sports_soccer_rounded,
                          title: '还没有可看的比赛',
                          detail: '比赛创建后会自动出现在这里。',
                          actionLabel: '刷新',
                          onAction: _retry,
                        )
                      : matches.isEmpty
                          ? const _CatalogMessage(
                              icon: Icons.search_off_rounded,
                              title: '没有找到匹配的比赛',
                              detail: '换个球队名或切换筛选条件试试。',
                            )
                          : RefreshIndicator(
                              onRefresh: () async => _retry(),
                              child: ListView.separated(
                                physics: const AlwaysScrollableScrollPhysics(),
                                padding: const EdgeInsets.all(16),
                                itemCount: matches.length,
                                separatorBuilder: (_, __) =>
                                    const SizedBox(height: 12),
                                itemBuilder: (context, index) {
                                  final match = matches[index];
                                  return _MatchCatalogCard(
                                    match: match,
                                    onTap: () => widget.onSelected(match),
                                  );
                                },
                              ),
                            ),
                ),
              ],
            );
          },
        ),
      ),
    );
  }
}

class _MatchCatalogCard extends StatelessWidget {
  final MatchCatalogItem match;
  final VoidCallback onTap;

  const _MatchCatalogCard({required this.match, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return Card(
      color: Colors.white,
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      match.competition.isEmpty ? '足球比赛' : match.competition,
                      style: Theme.of(context).textTheme.labelLarge,
                    ),
                  ),
                  Chip(label: Text(match.liveLabel)),
                ],
              ),
              const SizedBox(height: 12),
              Text(match.title, style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 8),
              Text(
                match.status == 'scheduled'
                    ? (match.kickoff.isEmpty ? '等待开赛时间' : match.kickoff)
                    : '${match.homeScore} — ${match.awayScore}',
              ),
              const SizedBox(height: 12),
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton.icon(
                  onPressed: onTap,
                  icon: const Icon(Icons.stadium_rounded),
                  label: Text(
                    switch (match.status) {
                      'finished' => '回看陪聊',
                      'scheduled' => '查看赛前',
                      _ => '进入陪看',
                    },
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _CatalogMessage extends StatelessWidget {
  final IconData icon;
  final String title;
  final String detail;
  final String? actionLabel;
  final VoidCallback? onAction;

  const _CatalogMessage({
    required this.icon,
    required this.title,
    required this.detail,
    this.actionLabel,
    this.onAction,
  });

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 44, color: AppColors.paperInk),
            const SizedBox(height: 16),
            Text(title, style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 8),
            Text(detail, textAlign: TextAlign.center),
            if (actionLabel != null && onAction != null) ...[
              const SizedBox(height: 16),
              FilledButton(onPressed: onAction, child: Text(actionLabel!)),
            ],
          ],
        ),
      ),
    );
  }
}
