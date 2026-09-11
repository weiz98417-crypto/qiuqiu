import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';

import '../services/browser_history.dart';
import '../services/match_catalog_service.dart';
import '../services/session_service.dart';
import 'match_catalog_screen.dart';
import 'match_screen.dart';

bool shouldAutoEnterMatch(MatchCatalogItem _) => false;

class MatchEntryScreen extends StatefulWidget {
  static const _configuredSocketUrl = String.fromEnvironment('QIUQIU_WS_URL');
  final MatchCatalogService? catalogService;

  const MatchEntryScreen({super.key, this.catalogService});

  @override
  State<MatchEntryScreen> createState() => _MatchEntryScreenState();
}

class _MatchEntryScreenState extends State<MatchEntryScreen> {
  String? _deepLinkedMatchId;

  @override
  void initState() {
    super.initState();
    if (kIsWeb) {
      final matchId = Uri.base.queryParameters['matchId']?.trim();
      if (matchId != null && matchId.isNotEmpty) {
        _deepLinkedMatchId = matchId;
      }
    }
  }

  String get _apiBaseUrl {
    if (MatchEntryScreen._configuredSocketUrl.isNotEmpty) {
      return normalizeAPIBaseURL(MatchEntryScreen._configuredSocketUrl);
    }
    if (kIsWeb && Uri.base.host.isNotEmpty) {
      return Uri(
        scheme: Uri.base.scheme,
        host: Uri.base.host,
        port: Uri.base.hasPort ? Uri.base.port : null,
      ).toString();
    }
    return 'http://10.0.2.2:8080';
  }

  @override
  Widget build(BuildContext context) {
    final deepLinkedMatchId = _deepLinkedMatchId;
    if (deepLinkedMatchId != null) {
      return MatchScreen(
        matchId: deepLinkedMatchId,
        autoEnter: true,
        onExit: _leaveDeepLink,
      );
    }
    return MatchCatalogScreen(
      apiBaseUrl: _apiBaseUrl,
      service: widget.catalogService,
      onSelected: (match) {
        Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => MatchScreen(
              matchId: match.matchId,
              onExit: () => Navigator.of(context).pop(),
              // Every catalog selection opens the match data view first. The
              // user must explicitly enter companion mode from that overview.
              autoEnter: shouldAutoEnterMatch(match),
              initialMatch: MatchViewData(
                homeTeam: match.homeTeam,
                awayTeam: match.awayTeam,
                competition: match.competition,
                period: match.status == 'finished'
                    ? 'finished'
                    : match.status == 'live'
                        ? 'first_half'
                        : 'pre_match',
                homeScore: match.homeScore,
                awayScore: match.awayScore,
                hasMatchInfo: true,
              ),
            ),
          ),
        );
      },
    );
  }

  void _leaveDeepLink() {
    if (!mounted) return;
    final queryParameters = Map<String, String>.from(Uri.base.queryParameters)
      ..remove('matchId');
    replaceBrowserUrl(
      Uri.base.replace(queryParameters: queryParameters),
    );
    setState(() => _deepLinkedMatchId = null);
  }
}
