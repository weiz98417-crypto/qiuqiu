import 'package:flutter/material.dart';
import '../services/preferences_service.dart';

class SettingsScreen extends StatefulWidget {
  final UserProfile initialProfile;
  final void Function(UserProfile) onSave;

  const SettingsScreen({
    super.key,
    required this.initialProfile,
    required this.onSave,
  });

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _nickCtrl;
  late String _favoriteTeam;
  late String _talkativeness;

  @override
  void initState() {
    super.initState();
    _nickCtrl = TextEditingController(text: widget.initialProfile.nickname);
    _favoriteTeam = widget.initialProfile.favoriteTeam;
    _talkativeness = widget.initialProfile.talkativeness;
  }

  @override
  void dispose() {
    _nickCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('设置'), backgroundColor: const Color(0xFF1A1A2E)),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          children: [
            TextField(
              controller: _nickCtrl,
              decoration: const InputDecoration(labelText: '昵称', hintText: '给自己起个名字'),
              maxLength: 20,
            ),
            const SizedBox(height: 24),
            const Text('话痨程度', style: TextStyle(color: Colors.white70)),
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'quiet', label: Text('安静')),
                ButtonSegment(value: 'normal', label: Text('标准')),
                ButtonSegment(value: 'active', label: Text('活跃')),
              ],
              selected: {_talkativeness},
              onSelectionChanged: (v) => setState(() => _talkativeness = v.first),
            ),
            const Spacer(),
            FilledButton(
              onPressed: () {
                widget.onSave(UserProfile(
                  nickname: _nickCtrl.text.trim(),
                  favoriteTeam: _favoriteTeam,
                  talkativeness: _talkativeness,
                ));
                Navigator.pop(context);
              },
              child: const Text('保存'),
            ),
          ],
        ),
      ),
    );
  }
}
