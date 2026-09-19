import 'package:flutter/material.dart';

import '../services/websocket_service.dart';
import '../theme/app_theme.dart';
class ConnectionMark extends StatelessWidget {
  final SocketStatus status;
  final bool dark;

  const ConnectionMark({required this.status, this.dark = false});

  @override
  Widget build(BuildContext context) {
    final connected = status == SocketStatus.connected;
    final failed = status == SocketStatus.failed;
    final label = connected
        ? '现场'
        : failed
            ? '连接失败'
            : '连接中';
    return Semantics(
      label: connected
          ? '比赛已连接'
          : failed
              ? '比赛连接失败'
              : '比赛正在连接',
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: connected
                  ? AppColors.green
                  : failed
                      ? AppColors.red
                      : AppColors.yellow,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: AppSpacing.xxs),
          Text(
            label,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  color: dark ? AppColors.paperInk : AppColors.ink,
                ),
          ),
        ],
      ),
    );
  }
}
