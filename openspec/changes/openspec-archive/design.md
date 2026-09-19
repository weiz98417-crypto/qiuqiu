# Design: OpenSpec Archive

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 惯例：tasks.md 全部 [x] 的 change → archive/；有开放项的留 changes/。惯例注记写入 openspec/config.yaml 注释或同级 README。 |
| 2 | 全部用 git mv 保留历史；不修改任何被移动文件的内容。 |
| 3 | 空清单（0/0）的 9 个视为已完成（无事可做）一并归档。 |

## Seam

- 纯移动，无代码 seam。

## Testing decisions

- 验证 = git status 干净 + 归档后 changes/ 目录清单与分类一致；无测试语义。