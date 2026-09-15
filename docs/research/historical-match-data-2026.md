# 历史比赛样例数据来源

样例比赛只使用 2026 年世界杯及之后的已结束赛事。种子脚本在运行时请求 ESPN 的公开赛事摘要接口，并把返回的首发、替补、阵型、裁判、场地和逐条事件写入后端；接口不可用或比分与事件不一致时直接失败，不写入占位资料。

## 赛事清单

| 比赛 | ESPN 赛事 | 来源 |
| --- | --- | --- |
| Arsenal 3–0 Coventry City | `eng.1`, `401879301` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/eng.1/summary?event=401879301` |
| Liverpool 2–2 Nottingham Forest | `eng.1`, `401879314` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/eng.1/summary?event=401879314` |
| Athletic Club 3–0 Atlético Madrid | `esp.1`, `401882895` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/esp.1/summary?event=401882895` |
| Levante 5–2 Real Betis | `esp.1`, `401882902` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/esp.1/summary?event=401882902` |
| Internazionale 3–2 Napoli | `ita.1`, `401874937` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/ita.1/summary?event=401874937` |
| Juventus 2–0 Parma | `ita.1`, `401874746` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/ita.1/summary?event=401874746` |
| England 2–1 Congo DR（2026 世界杯） | `fifa.world`, `760495` | `https://site.web.api.espn.com/apis/site/v2/sports/soccer/fifa.world/summary?event=760495` |

## 字段规则

- `rosters[].roster[]` 转换为首发和替补，`starter` 决定 `lineup`，`position.abbreviation` 保留原始位置。
- `rosters[].formation` 写入双方阵型。
- `gameInfo.officials` 仅记录职位为 Referee 的裁判，`gameInfo.venue.fullName` 写入球场。
- `keyEvents` 只转换进球、黄牌、红牌和换人；原始英文描述和 ESPN 来源 URL 保存在事件证据中。
- 教练不在该摘要接口返回，因此保持空值，不推测、不编造。
- 每个进球按事件顺序递增比分；最终事件比分必须与 ESPN 终场比分一致。
