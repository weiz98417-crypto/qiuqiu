import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';

import '../services/match_catalog_service.dart';
import '../theme/app_theme.dart';
import '../widgets/metal_button.dart';
import '../widgets/mobile_theme_canvas.dart';

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
  final TextEditingController _searchController = TextEditingController();
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
    _searchController.dispose();
    if (widget.service == null) _service.close();
    super.dispose();
  }

  void _selectMatch(MatchCatalogItem match) {
    _searchController.clear();
    setState(() {
      _query = '';
      _filter = 'all';
    });
    widget.onSelected(match);
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
      backgroundColor: AppColors.night,
      body: MobileThemeCanvas(
        backgroundAsset: 'assets/images/stadium-sunset.png',
        overlayColor: const Color(0x26070B12),
        child: SafeArea(
          child: Center(
            child: LayoutBuilder(
              builder: (context, constraints) {
                final stageWidth =
                    constraints.maxWidth < 520 ? constraints.maxWidth : 520.0;
                final isPhoneWidth = constraints.maxWidth <= 560;
                return SizedBox(
                  width: stageWidth,
                  height: constraints.maxHeight,
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: const Color(0x24111824),
                      borderRadius: BorderRadius.circular(
                        isPhoneWidth ? 0 : 24,
                      ),
                      boxShadow: isPhoneWidth
                          ? const []
                          : const [
                              BoxShadow(
                                color: Color(0x66000000),
                                blurRadius: 28,
                                offset: Offset(0, 12),
                              ),
                            ],
                    ),
                    child: FutureBuilder<List<MatchCatalogItem>>(
                      future: _matches,
                      builder: (context, snapshot) {
                        if (snapshot.connectionState ==
                            ConnectionState.waiting) {
                          return const Center(
                            child: CircularProgressIndicator(),
                          );
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
                            SizedBox(
                              height: 64,
                              child: Padding(
                                padding:
                                    const EdgeInsets.fromLTRB(20, 8, 12, 4),
                                child: Row(
                                  children: [
                                    const Expanded(
                                      child: Text(
                                        '选择一场比赛',
                                        style: TextStyle(
                                          color: AppColors.ink,
                                          fontSize: 20,
                                          fontWeight: FontWeight.w800,
                                        ),
                                      ),
                                    ),
                                    IconButton(
                                      tooltip: '刷新比赛',
                                      onPressed: _retry,
                                      color: AppColors.ink,
                                      icon: const Icon(Icons.refresh_rounded),
                                    ),
                                  ],
                                ),
                              ),
                            ),
                            Padding(
                              padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                              child: TextField(
                                controller: _searchController,
                                onChanged: (value) =>
                                    setState(() => _query = value),
                                decoration: const InputDecoration(
                                  prefixIcon: Icon(Icons.search_rounded),
                                  hintText: '搜索球队或赛事',
                                ),
                              ),
                            ),
                            SingleChildScrollView(
                              scrollDirection: Axis.horizontal,
                              padding:
                                  const EdgeInsets.symmetric(horizontal: 16),
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
                                        onSelected: (_) => setState(
                                          () => _filter = filter.$1,
                                        ),
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
                                            physics:
                                                const AlwaysScrollableScrollPhysics(),
                                            padding: const EdgeInsets.all(16),
                                            itemCount: matches.length,
                                            separatorBuilder: (_, __) =>
                                                const SizedBox(height: 12),
                                            itemBuilder: (context, index) {
                                              final match = matches[index];
                                              return _MatchCatalogCard(
                                                match: match,
                                                onTap: () =>
                                                    _selectMatch(match),
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
              },
            ),
          ),
        ),
      ),
    );
  }
}

class _MatchCatalogCard extends StatelessWidget {
  final MatchCatalogItem match;
  final VoidCallback onTap;

  const _MatchCatalogCard({required this.match, required this.onTap});

  String get _backgroundAsset {
    final home = match.homeTeam.trim();
    final away = match.awayTeam.trim();
    if (home == '西班牙' && away == '德国') {
      return 'assets/images/match-card-stadium.png';
    }
    final competition = match.competition.trim();
    if (competition.contains('世界杯') || competition.contains('冠军联赛')) {
      return 'assets/images/match-card-digital-player.png';
    }
    if (competition.contains('足总杯') || competition.contains('足协杯')) {
      return 'assets/images/match-card-blue-ball.png';
    }
    return 'assets/images/match-card-stadium-lights.png';
  }

  @override
  Widget build(BuildContext context) {
    final cardRadius = BorderRadius.circular(16);
    final card = Card(
      color: Colors.transparent,
      elevation: 0,
      margin: EdgeInsets.zero,
      clipBehavior: Clip.antiAlias,
      shape: RoundedRectangleBorder(borderRadius: cardRadius),
      child: Ink(
        decoration: BoxDecoration(
          borderRadius: cardRadius,
          border: Border.all(color: AppColors.skyBlue.withValues(alpha: 0.38)),
          boxShadow: [
            BoxShadow(
              color: AppColors.night.withValues(alpha: 0.38),
              blurRadius: 14,
              offset: const Offset(0, 8),
            ),
          ],
        ),
        child: Stack(
          children: [
            Positioned.fill(
              child: Image.asset(
                _backgroundAsset,
                fit: BoxFit.cover,
                errorBuilder: (_, __, ___) => const ColoredBox(
                  color: AppColors.terrace,
                ),
              ),
            ),
            Positioned.fill(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.topLeft,
                    end: Alignment.bottomRight,
                    colors: [
                      AppColors.night.withValues(alpha: 0.46),
                      AppColors.night.withValues(alpha: 0.12),
                      AppColors.night.withValues(alpha: 0.62),
                    ],
                    stops: const [0, 0.48, 1],
                  ),
                ),
              ),
            ),
            InkWell(
              borderRadius: cardRadius,
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
                            match.competition.isEmpty
                                ? '足球比赛'
                                : match.competition,
                            style: Theme.of(context)
                                .textTheme
                                .labelLarge
                                ?.copyWith(
                              color: AppColors.ink,
                              shadows: const [
                                Shadow(
                                  color: AppColors.night,
                                  blurRadius: 6,
                                ),
                              ],
                            ),
                          ),
                        ),
                        DecoratedBox(
                          decoration: BoxDecoration(
                            color: AppColors.night.withValues(alpha: 0.62),
                            borderRadius: BorderRadius.circular(8),
                            border: Border.all(
                              color: AppColors.ink.withValues(alpha: 0.28),
                            ),
                          ),
                          child: Padding(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 10,
                              vertical: 6,
                            ),
                            child: Text(
                              match.liveLabel,
                              style: Theme.of(context)
                                  .textTheme
                                  .labelMedium
                                  ?.copyWith(color: AppColors.ink),
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 18),
                    Text(
                      match.title,
                      style: Theme.of(context).textTheme.titleLarge?.copyWith(
                        color: AppColors.ink,
                        fontWeight: FontWeight.w900,
                        shadows: const [
                          Shadow(
                            color: AppColors.night,
                            blurRadius: 8,
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      match.status == 'scheduled'
                          ? (match.kickoff.isEmpty ? '等待开赛时间' : match.kickoff)
                          : '${match.homeScore} — ${match.awayScore}',
                      style: const TextStyle(
                        color: AppColors.ink,
                        fontWeight: FontWeight.w700,
                        shadows: [
                          Shadow(color: AppColors.night, blurRadius: 6),
                        ],
                      ),
                    ),
                    const SizedBox(height: 18),
                    Align(
                      alignment: Alignment.centerRight,
                      child: MetalButton(
                        onPressed: onTap,
                        leading: const Icon(Icons.stadium_rounded),
                        child: Text(
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
          ],
        ),
      ),
    );
    return card
        .animate()
        .fadeIn(duration: 280.ms, curve: Curves.easeOutCubic)
        .slideY(begin: 0.04, end: 0, duration: 340.ms);
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
            Text(
              title,
              style: Theme.of(context).textTheme.titleLarge?.copyWith(
                    color: AppColors.paperInk,
                  ),
            ),
            const SizedBox(height: 8),
            Text(
              detail,
              textAlign: TextAlign.center,
              style: const TextStyle(color: AppColors.paperInk),
            ),
            if (actionLabel != null && onAction != null) ...[
              const SizedBox(height: 16),
              MetalButton(
                onPressed: onAction,
                animateEntrance: true,
                child: Text(actionLabel!),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
