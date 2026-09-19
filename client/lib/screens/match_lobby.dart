import 'package:flutter/material.dart';

import '../services/match_view_data.dart';
import '../services/match_overview_service.dart';
import '../services/websocket_service.dart';
import '../theme/app_theme.dart';
import '../widgets/connection_mark.dart';
import '../widgets/metal_button.dart';
class MatchLobby extends StatefulWidget {
  final MatchViewData match;
  final MatchOverviewData overview;
  final bool overviewLoading;
  final String? overviewError;
  final SocketStatus socketStatus;
  final VoidCallback onEnter;
  final VoidCallback onReconnect;
  final VoidCallback onOpenSettings;
  final VoidCallback onLeave;

  const MatchLobby({
    super.key,
    required this.match,
    required this.overview,
    required this.overviewLoading,
    required this.overviewError,
    required this.socketStatus,
    required this.onEnter,
    required this.onReconnect,
    required this.onOpenSettings,
    required this.onLeave,
  });

  @override
  State<MatchLobby> createState() => _MatchLobbyState();
}

class _MatchLobbyState extends State<MatchLobby> {
  int _tabIndex = 0;

  MatchOverviewData get details {
    final overview = widget.overview;
    return overview.copyWith(
      homeScore: widget.match.homeScore,
      awayScore: widget.match.awayScore,
      period:
          widget.match.period.isEmpty ? overview.period : widget.match.period,
      clock: widget.match.clock.isEmpty ? overview.clock : widget.match.clock,
      liveLabel: widget.match.liveLabel,
    );
  }

  String _teamName(String value, String fallback) =>
      value == '主队' || value == '客队' ? fallback : value;

  @override
  Widget build(BuildContext context) {
    final overview = details;
    final homeTeam = _teamName(overview.homeTeam, widget.match.homeTeam);
    final awayTeam = _teamName(overview.awayTeam, widget.match.awayTeam);
    final title =
        overview.competition.trim().isEmpty ? '比赛详情' : overview.competition;
    return ColoredBox(
      color: const Color(0x330B2E68),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.md,
          AppSpacing.sm,
          AppSpacing.md,
          AppSpacing.md,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                IconButton(
                  tooltip: '返回比赛列表',
                  onPressed: widget.onLeave,
                  color: AppColors.ink,
                  icon: const Icon(Icons.arrow_back_rounded),
                ),
                Expanded(
                  child: Text(
                    '$title · ${overview.liveLabel}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.labelMedium?.copyWith(
                          color: AppColors.ink,
                          letterSpacing: 0.6,
                        ),
                  ),
                ),
                ConnectionMark(status: widget.socketStatus, dark: true),
                const SizedBox(width: AppSpacing.xs),
                IconButton(
                  tooltip: '陪看设置',
                  onPressed: widget.onOpenSettings,
                  color: AppColors.ink,
                  icon: const Icon(Icons.tune_rounded),
                ),
              ],
            ),
            _MatchScoreHeader(
              overview: overview,
              homeTeam: homeTeam,
              awayTeam: awayTeam,
            ),
            const SizedBox(height: AppSpacing.sm),
            if (widget.overviewLoading)
              const LinearProgressIndicator(
                minHeight: 2,
                color: AppColors.skyBlue,
                backgroundColor: Colors.transparent,
              )
            else if (widget.overviewError != null)
              Text(
                '详细资料暂时不可用，仍可查看实时比分和进入陪看。',
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: AppColors.yellow,
                    ),
              ),
            const SizedBox(height: AppSpacing.sm),
            _OverviewTabs(
              selectedIndex: _tabIndex,
              onSelected: (index) => setState(() => _tabIndex = index),
            ),
            const SizedBox(height: AppSpacing.sm),
            Expanded(
              child: SingleChildScrollView(
                padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                child: switch (_tabIndex) {
                  0 => _OverviewTab(overview: overview),
                  1 => _LineupTab(overview: overview),
                  2 => _EventsTab(overview: overview),
                  _ => _StatsTab(overview: overview),
                },
              ),
            ),
            if (widget.socketStatus == SocketStatus.failed)
              OutlinedButton.icon(
                onPressed: widget.onReconnect,
                icon: const Icon(Icons.refresh_rounded),
                label: const Text('重新连接比赛'),
              )
            else
              MetalButton(
                onPressed: widget.socketStatus == SocketStatus.connected
                    ? widget.onEnter
                    : null,
                leading: const Icon(Icons.forum_rounded),
                child: Text(
                  widget.socketStatus == SocketStatus.connected
                      ? '进入球球陪看'
                      : '正在连接比赛…',
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _MatchScoreHeader extends StatelessWidget {
  final MatchOverviewData overview;
  final String homeTeam;
  final String awayTeam;

  const _MatchScoreHeader({
    required this.overview,
    required this.homeTeam,
    required this.awayTeam,
  });

  @override
  Widget build(BuildContext context) {
    final scoreStyle = Theme.of(context).textTheme.displaySmall?.copyWith(
      color: AppColors.ink,
      fontFamily: AppFonts.scoreboard,
      fontFeatures: const [FontFeature.tabularFigures()],
    );
    return _LobbyPanel(
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.md,
        AppSpacing.sm,
        AppSpacing.md,
        AppSpacing.md,
      ),
      child: Column(
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              _StatusPill(label: overview.liveLabel),
              if (overview.round.trim().isNotEmpty) ...[
                const SizedBox(width: AppSpacing.xs),
                Text(
                  overview.round,
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: AppColors.muted,
                      ),
                ),
              ],
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          Row(
            children: [
              Expanded(
                child: Text(
                  homeTeam,
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.titleLarge?.copyWith(
                        color: AppColors.ink,
                      ),
                ),
              ),
              Column(
                children: [
                  Text('${overview.homeScore} — ${overview.awayScore}',
                      style: scoreStyle),
                  Text(
                    overview.clock.trim().isEmpty ? '赛前' : overview.clock,
                    style: Theme.of(context).textTheme.labelSmall?.copyWith(
                          color: AppColors.skyBlue,
                          fontFamily: AppFonts.scoreboard,
                        ),
                  ),
                ],
              ),
              Expanded(
                child: Text(
                  awayTeam,
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.titleLarge?.copyWith(
                        color: AppColors.ink,
                      ),
                ),
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          Wrap(
            alignment: WrapAlignment.center,
            spacing: AppSpacing.md,
            runSpacing: AppSpacing.xs,
            children: [
              if (overview.kickoff.trim().isNotEmpty)
                _MetaLabel(
                    icon: Icons.schedule_rounded, text: overview.kickoff),
              if (overview.venue.trim().isNotEmpty)
                _MetaLabel(icon: Icons.stadium_outlined, text: overview.venue),
              if (overview.referee.trim().isNotEmpty)
                _MetaLabel(icon: Icons.sports_rounded, text: overview.referee),
            ],
          ),
        ],
      ),
    );
  }
}

class _OverviewTabs extends StatelessWidget {
  final int selectedIndex;
  final ValueChanged<int> onSelected;

  const _OverviewTabs({required this.selectedIndex, required this.onSelected});

  @override
  Widget build(BuildContext context) {
    const labels = ['概览', '阵容', '事件', '数据'];
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          for (var index = 0; index < labels.length; index++)
            Padding(
              padding: const EdgeInsets.only(right: AppSpacing.xs),
              child: ChoiceChip(
                label: Text(labels[index]),
                selected: selectedIndex == index,
                onSelected: (_) => onSelected(index),
                selectedColor: AppColors.championBlue,
                labelStyle: TextStyle(
                  color: selectedIndex == index ? Colors.white : AppColors.ink,
                  fontWeight: FontWeight.w700,
                ),
                side: BorderSide(
                  color: selectedIndex == index
                      ? AppColors.skyBlue
                      : AppColors.ink.withValues(alpha: 0.28),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _OverviewTab extends StatelessWidget {
  final MatchOverviewData overview;

  const _OverviewTab({required this.overview});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _LobbyPanel(
          title: '比赛状态',
          child: Wrap(
            spacing: AppSpacing.xs,
            runSpacing: AppSpacing.xs,
            children: [
              _InfoChip(label: _periodLabel(overview.period)),
              _InfoChip(
                  label: '比分 ${overview.homeScore} — ${overview.awayScore}'),
              if (overview.halftimeHomeScore != null &&
                  overview.halftimeAwayScore != null)
                _InfoChip(
                  label:
                      '半场 ${overview.halftimeHomeScore} — ${overview.halftimeAwayScore}',
                ),
              _InfoChip(
                  label:
                      '黄牌 ${overview.cardCount('home', 'yellow_card')} · ${overview.cardCount('away', 'yellow_card')}'),
              _InfoChip(
                  label:
                      '红牌 ${overview.cardCount('home', 'red_card')} · ${overview.cardCount('away', 'red_card')}'),
            ],
          ),
        ),
        const SizedBox(height: AppSpacing.sm),
        _LobbyPanel(
          title: '最新动态',
          child: _EventList(
            events:
                overview.chronologicalEvents.take(5).toList(growable: false),
            emptyLabel: '暂无公开比赛事件',
          ),
        ),
        const SizedBox(height: AppSpacing.sm),
        _LobbyPanel(
          title: '球队信息',
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: _TeamMeta(
                  name: overview.homeTeam,
                  coach: overview.homeCoach,
                  formation: overview.homeFormation,
                ),
              ),
              const SizedBox(width: AppSpacing.md),
              Expanded(
                child: _TeamMeta(
                  name: overview.awayTeam,
                  coach: overview.awayCoach,
                  formation: overview.awayFormation,
                  alignEnd: true,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _LineupTab extends StatelessWidget {
  final MatchOverviewData overview;

  const _LineupTab({required this.overview});

  @override
  Widget build(BuildContext context) {
    if (overview.homePlayers.isEmpty && overview.awayPlayers.isEmpty) {
      return const _EmptyLobbyState(
        icon: Icons.groups_rounded,
        title: '首发名单暂未公布',
        detail: '比赛资料同步后，双方首发、替补和教练会显示在这里。',
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: _TeamLineup(
                name: overview.homeTeam,
                coach: overview.homeCoach,
                formation: overview.homeFormation,
                starters: overview.homeStarters,
                bench: overview.homeBench,
              ),
            ),
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              child: _TeamLineup(
                name: overview.awayTeam,
                coach: overview.awayCoach,
                formation: overview.awayFormation,
                starters: overview.awayStarters,
                bench: overview.awayBench,
                alignEnd: true,
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _EventsTab extends StatelessWidget {
  final MatchOverviewData overview;

  const _EventsTab({required this.overview});

  @override
  Widget build(BuildContext context) {
    return _LobbyPanel(
      title: '完整比赛事件',
      child: _EventList(
        events: overview.chronologicalEvents,
        emptyLabel: '暂无公开比赛事件',
      ),
    );
  }
}

class _StatsTab extends StatelessWidget {
  final MatchOverviewData overview;

  const _StatsTab({required this.overview});

  @override
  Widget build(BuildContext context) {
    if (overview.teamStats.isEmpty && overview.stats.isEmpty) {
      return Column(
        children: [
          const _EmptyLobbyState(
            icon: Icons.query_stats_rounded,
            title: '详细数据暂未接入',
            detail: '数据源提供控球率、射门、角球、犯规、越位、传球和 xG 后会自动显示。',
          ),
          const SizedBox(height: AppSpacing.sm),
          _LobbyPanel(
            title: '当前纪律情况',
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceAround,
              children: [
                _DisciplineMetric(
                  label: overview.homeTeam,
                  yellow: overview.cardCount('home', 'yellow_card'),
                  red: overview.cardCount('home', 'red_card'),
                ),
                _DisciplineMetric(
                  label: overview.awayTeam,
                  yellow: overview.cardCount('away', 'yellow_card'),
                  red: overview.cardCount('away', 'red_card'),
                ),
              ],
            ),
          ),
        ],
      );
    }
    final rows = overview.teamStats.isNotEmpty
        ? overview.teamStats
            .map((stat) => _TeamStatRow(stat: stat))
            .toList(growable: false)
        : overview.stats.entries
            .map((entry) => _StatRow(label: entry.key, value: '${entry.value}'))
            .toList(growable: false);
    return _LobbyPanel(
      title: '比赛数据',
      child: Column(
        children: rows,
      ),
    );
  }
}

class _LobbyPanel extends StatelessWidget {
  final String? title;
  final Widget child;
  final EdgeInsetsGeometry padding;

  const _LobbyPanel(
      {this.title,
      required this.child,
      this.padding = const EdgeInsets.all(AppSpacing.sm)});

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: const Color(0x40111824),
        border: Border.all(color: AppColors.skyBlue.withValues(alpha: 0.28)),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Padding(
        padding: padding,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (title != null) ...[
              Text(
                title!,
                style: Theme.of(context).textTheme.titleSmall?.copyWith(
                      color: AppColors.ink,
                      fontWeight: FontWeight.w800,
                    ),
              ),
              const SizedBox(height: AppSpacing.xs),
            ],
            child,
          ],
        ),
      ),
    );
  }
}

class _TeamLineup extends StatelessWidget {
  final String name;
  final String coach;
  final String formation;
  final List<MatchPlayerOverview> starters;
  final List<MatchPlayerOverview> bench;
  final bool alignEnd;

  const _TeamLineup({
    required this.name,
    required this.coach,
    required this.formation,
    required this.starters,
    required this.bench,
    this.alignEnd = false,
  });

  @override
  Widget build(BuildContext context) {
    final alignment =
        alignEnd ? CrossAxisAlignment.end : CrossAxisAlignment.start;
    return _LobbyPanel(
      child: Column(
        crossAxisAlignment: alignment,
        children: [
          Text(name,
              style: Theme.of(context).textTheme.titleMedium?.copyWith(
                  color: AppColors.ink, fontWeight: FontWeight.w800)),
          if (coach.trim().isNotEmpty)
            _SmallMeta(label: '教练 · $coach', alignEnd: alignEnd),
          if (formation.trim().isNotEmpty)
            _SmallMeta(label: '阵型 · $formation', alignEnd: alignEnd),
          const SizedBox(height: AppSpacing.xs),
          _LineupGroup(label: '首发', players: starters, alignEnd: alignEnd),
          const SizedBox(height: AppSpacing.xs),
          _LineupGroup(label: '替补', players: bench, alignEnd: alignEnd),
        ],
      ),
    );
  }
}

class _LineupGroup extends StatelessWidget {
  final String label;
  final List<MatchPlayerOverview> players;
  final bool alignEnd;

  const _LineupGroup(
      {required this.label, required this.players, required this.alignEnd});

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment:
          alignEnd ? CrossAxisAlignment.end : CrossAxisAlignment.start,
      children: [
        Text(label,
            style: Theme.of(context)
                .textTheme
                .labelSmall
                ?.copyWith(color: AppColors.skyBlue)),
        if (players.isEmpty)
          Text('暂无',
              style: Theme.of(context)
                  .textTheme
                  .labelSmall
                  ?.copyWith(color: AppColors.muted))
        else
          for (final player in players)
            Text(
              '${player.number.isEmpty ? '·' : player.number} ${player.name}',
              textAlign: alignEnd ? TextAlign.right : TextAlign.left,
              style: Theme.of(context)
                  .textTheme
                  .labelSmall
                  ?.copyWith(color: AppColors.ink, height: 1.6),
            ),
      ],
    );
  }
}

class _EventList extends StatelessWidget {
  final List<MatchEventOverview> events;
  final String emptyLabel;

  const _EventList({required this.events, required this.emptyLabel});

  @override
  Widget build(BuildContext context) {
    if (events.isEmpty)
      return Text(emptyLabel, style: const TextStyle(color: AppColors.muted));
    return Column(
      children: [
        for (final event in events)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.xs),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: 44,
                  child: Text(event.displayClock,
                      style: const TextStyle(
                          color: AppColors.skyBlue,
                          fontFamily: AppFonts.scoreboard)),
                ),
                Icon(_eventIcon(event.eventType),
                    size: 17, color: _eventColor(event.eventType)),
                const SizedBox(width: AppSpacing.xs),
                Expanded(
                  child: Text(
                    '${event.teamName.isEmpty ? '' : '${event.teamName} · '}${event.label}',
                    style: const TextStyle(color: AppColors.ink, height: 1.35),
                  ),
                ),
                if (event.eventType == 'goal')
                  Text('${event.homeScore}—${event.awayScore}',
                      style: const TextStyle(
                          color: AppColors.ink,
                          fontFamily: AppFonts.scoreboard)),
              ],
            ),
          ),
      ],
    );
  }
}

class _TeamMeta extends StatelessWidget {
  final String name;
  final String coach;
  final String formation;
  final bool alignEnd;

  const _TeamMeta(
      {required this.name,
      required this.coach,
      required this.formation,
      this.alignEnd = false});

  @override
  Widget build(BuildContext context) {
    final align = alignEnd ? TextAlign.right : TextAlign.left;
    return Column(
      crossAxisAlignment:
          alignEnd ? CrossAxisAlignment.end : CrossAxisAlignment.start,
      children: [
        Text(name,
            textAlign: align,
            style: const TextStyle(
                color: AppColors.ink, fontWeight: FontWeight.w800)),
        _SmallMeta(
            label: coach.trim().isEmpty ? '教练资料暂无' : '教练 · $coach',
            alignEnd: alignEnd),
        _SmallMeta(
            label: formation.trim().isEmpty ? '阵型资料暂无' : '阵型 · $formation',
            alignEnd: alignEnd),
      ],
    );
  }
}

class _SmallMeta extends StatelessWidget {
  final String label;
  final bool alignEnd;

  const _SmallMeta({required this.label, required this.alignEnd});

  @override
  Widget build(BuildContext context) {
    return Text(label,
        textAlign: alignEnd ? TextAlign.right : TextAlign.left,
        style:
            const TextStyle(color: AppColors.muted, fontSize: 12, height: 1.5));
  }
}

class _MetaLabel extends StatelessWidget {
  final IconData icon;
  final String text;

  const _MetaLabel({required this.icon, required this.text});

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 14, color: AppColors.skyBlue),
        const SizedBox(width: 4),
        Text(text,
            style: const TextStyle(color: AppColors.muted, fontSize: 11)),
      ],
    );
  }
}

class _StatusPill extends StatelessWidget {
  final String label;

  const _StatusPill({required this.label});

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: AppColors.championBlue.withValues(alpha: 0.72),
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: AppColors.skyBlue.withValues(alpha: 0.75)),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        child: Text(label,
            style: const TextStyle(
                color: Colors.white,
                fontSize: 12,
                fontWeight: FontWeight.w800)),
      ),
    );
  }
}

class _InfoChip extends StatelessWidget {
  final String label;

  const _InfoChip({required this.label});

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
          color: AppColors.night.withValues(alpha: 0.38),
          borderRadius: BorderRadius.circular(8)),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
        child: Text(label,
            style: const TextStyle(color: AppColors.ink, fontSize: 12)),
      ),
    );
  }
}

class _DisciplineMetric extends StatelessWidget {
  final String label;
  final int yellow;
  final int red;

  const _DisciplineMetric(
      {required this.label, required this.yellow, required this.red});

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Text(label,
            style: const TextStyle(
                color: AppColors.ink, fontWeight: FontWeight.w700)),
        const SizedBox(height: 4),
        Text('黄 $yellow  ·  红 $red',
            style: const TextStyle(color: AppColors.muted, fontSize: 12)),
      ],
    );
  }
}

class _StatRow extends StatelessWidget {
  final String label;
  final String value;

  const _StatRow({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(label, style: const TextStyle(color: AppColors.muted)),
          Text(value,
              style: const TextStyle(
                  color: AppColors.ink, fontWeight: FontWeight.w800)),
        ],
      ),
    );
  }
}

class _TeamStatRow extends StatelessWidget {
  final MatchTeamStat stat;

  const _TeamStatRow({required this.stat});

  String _value(double value) {
    final number = value == value.roundToDouble()
        ? value.toInt().toString()
        : value.toStringAsFixed(1);
    return '$number${stat.unit}';
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 7),
      child: Row(
        children: [
          Expanded(
            child: Text(
              _value(stat.home),
              style: const TextStyle(
                color: AppColors.ink,
                fontWeight: FontWeight.w800,
                fontFamily: AppFonts.scoreboard,
              ),
            ),
          ),
          Expanded(
            flex: 2,
            child: Text(
              stat.label,
              textAlign: TextAlign.center,
              style: const TextStyle(color: AppColors.muted, fontSize: 12),
            ),
          ),
          Expanded(
            child: Text(
              _value(stat.away),
              textAlign: TextAlign.right,
              style: const TextStyle(
                color: AppColors.ink,
                fontWeight: FontWeight.w800,
                fontFamily: AppFonts.scoreboard,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _EmptyLobbyState extends StatelessWidget {
  final IconData icon;
  final String title;
  final String detail;

  const _EmptyLobbyState(
      {required this.icon, required this.title, required this.detail});

  @override
  Widget build(BuildContext context) {
    return _LobbyPanel(
      child: Column(
        children: [
          Icon(icon, color: AppColors.skyBlue, size: 34),
          const SizedBox(height: AppSpacing.xs),
          Text(title,
              style: const TextStyle(
                  color: AppColors.ink, fontWeight: FontWeight.w800)),
          const SizedBox(height: 4),
          Text(detail,
              textAlign: TextAlign.center,
              style: const TextStyle(
                  color: AppColors.muted, fontSize: 12, height: 1.45)),
        ],
      ),
    );
  }
}

IconData _eventIcon(String eventType) => switch (eventType) {
      'goal' => Icons.sports_soccer_rounded,
      'yellow_card' => Icons.crop_square_rounded,
      'red_card' => Icons.square_rounded,
      'substitution' => Icons.swap_vert_rounded,
      'penalty' || 'penalty_awarded' => Icons.gpp_good_rounded,
      'var_check' || 'var_result' => Icons.tv_rounded,
      'injury' => Icons.healing_rounded,
      _ => Icons.circle_outlined,
    };

Color _eventColor(String eventType) => switch (eventType) {
      'goal' => AppColors.orange,
      'yellow_card' => AppColors.yellow,
      'red_card' => AppColors.red,
      'substitution' => AppColors.green,
      _ => AppColors.skyBlue,
    };

String _periodLabel(String period) => switch (period.trim().toLowerCase()) {
      '' || 'pre_match' => '赛前',
      'first_half' => '上半场',
      'half_time' || 'halftime' => '中场休息',
      'second_half' => '下半场',
      'extra_time' => '加时赛',
      'penalties' => '点球大战',
      'finished' || 'full_time' || 'fulltime' => '全场结束',
      _ => period,
    };

