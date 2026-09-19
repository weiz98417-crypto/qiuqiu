# MVP Extras 技术设计

## 1. 极简身份

### 1.1 数据模型

```dart
class UserProfile {
  String nickname;        // 用户昵称，默认 ""
  String? favoriteTeam;   // 主队名称，可选
  String? favoriteTeamId; // api-sports 的 team_id
  DateTime createdAt;

  bool get hasProfile => nickname.isNotEmpty;
}
```

### 1.2 存储方案

Phase 0-3 使用本地 SharedPreferences 存储，不上云。

```dart
// 存储
final prefs = await SharedPreferences.getInstance();
await prefs.setString('profile', jsonEncode(userProfile.toJson()));

// 读取
final data = prefs.getString('profile');
final profile = data != null ? UserProfile.fromJson(jsonDecode(data)) : UserProfile.empty();
```

### 1.3 设置页（Flutter）

```
┌─────────────────────┐
│   设置               │
├─────────────────────┤
│ 昵称    [____球友___] │
│ 主队    [选择球队  ▼] │
│ 话痨    ○安静 ○标准 ●活跃 │
│                     │
│  [保存]              │
└─────────────────────┘
```

球队选择器：从 api-sports.io 拉取热门联赛球队列表，本地缓存。

## 2. 用户偏好记忆

### 2.1 偏好如何影响球球行为

| 场景 | 无偏好 | 用户主队=皇马 |
|------|--------|-------------|
| 皇马进球 | 中性兴奋 | **更兴奋** "我们的球！" |
| 皇马失球 | 中性遗憾 | **更遗憾** "哎..." |
| 皇马被判罚 | 客观描述 | **偏心** "这有点严吧？" |

### 2.2 偏好注入 LLM

用户偏好作为 User Context 层注入 LLM Prompt，注入前强制执行输入清洗：

```go
// 服务端清洗（每次 LLM 调用前执行）
func SanitizeUserContext(nickname, favoriteTeam string) UserContext {
    // 昵称：中英文数字下划线，max 20 字符，否则截断并替换非法字符
    nickname = regexp.MustCompile(`[^\p{Han}\w_]`).ReplaceAllString(nickname, "")
    if len([]rune(nickname)) > 20 {
        nickname = string([]rune(nickname)[:20])
    }

    // 主队名称：必须来自 api-sports 合法球队枚举，否则清空
    if !isValidTeam(favoriteTeam) {
        favoriteTeam = ""
    }

    return UserContext{Nickname: nickname, FavoriteTeam: favoriteTeam}
}
```

**Prompt 注入防护**：用户数据用 `"""` 包裹并通过独立分隔符与 system prompt 隔离，不直接拼接。

```
## 用户信息
- 用户昵称：{nickname}（{has_profile ? "有" : "没有"}设置）
- 主队：{favorite_team}（如果设置了）
- 话痨偏好：{talkativeness}
```

### 2.3 偏好存储结构

```go
type UserPreferences struct {
    Nickname       string `json:"nickname"`
    FavoriteTeam   string `json:"favorite_team"`
    FavoriteTeamID string `json:"favorite_team_id"`
    Talkativeness  string `json:"talkativeness"` // "quiet"|"normal"|"active"
}
```

Flutter 侧存储在 SharedPreferences，每次 WebSocket 连接时发送给服务端。
