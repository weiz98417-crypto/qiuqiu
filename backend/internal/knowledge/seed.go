package knowledge

// YAML seed 导入器（ADR-0017 修订）：repo YAML 从运行时事实源降级为
// seed——首次导入 + 新部署补种。幂等纪律：已存在的条目 id 一律跳过，
// 重跑零新增；运营在 DB 里的编辑永不被 seed 覆盖（知识条目错误从「git
// 修正」升级为「运营台修正 + 审计」，YAML 只是冷启动原料）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// parseEntryFile 是 Load 与 SeedDir 共用的单文件解析口径（字段校验与
// Load 逐字一致：id/topics/answer 必填、triggers 必带 quote）。
func parseEntryFile(path string, raw []byte) (Entry, error) {
	var entry Entry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		return Entry{}, fmt.Errorf("knowledge parse %s: %w", path, err)
	}
	if entry.ID == "" || strings.TrimSpace(entry.Answer) == "" || len(entry.Topics) == 0 {
		return Entry{}, fmt.Errorf("knowledge entry %s missing id/topics/answer", path)
	}
	if len(entry.Triggers) > 0 && strings.TrimSpace(entry.Quote) == "" {
		return Entry{}, fmt.Errorf("knowledge entry %s has triggers but no quote (weave anchor required)", path)
	}
	return entry, nil
}

// SeedDir 把目录下全部 *.yaml 条目导入 store，返回新增条数。以条目 id 为
// 幂等键：存在即跳过（含内容不同的情形——DB 是事实源，YAML 陈旧不追平）。
func SeedDir(ctx context.Context, store Store, dir string) (int, error) {
	if store == nil {
		return 0, fmt.Errorf("knowledge seed: nil store")
	}
	seeded := 0
	err := filepath.WalkDir(dir, func(path string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yaml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("knowledge read %s: %w", path, err)
		}
		entry, err := parseEntryFile(path, raw)
		if err != nil {
			return err
		}
		if _, err := store.Get(ctx, entry.ID); err == nil {
			return nil // 幂等：已导入过（或运营已改）——跳过。
		} else if err != ErrNotFound {
			return fmt.Errorf("knowledge seed lookup %s: %w", entry.ID, err)
		}
		if _, err := store.Put(ctx, entry, "seed"); err != nil {
			return fmt.Errorf("knowledge seed insert %s: %w", entry.ID, err)
		}
		seeded++
		return nil
	})
	if err != nil {
		return seeded, fmt.Errorf("knowledge dir: %w", err)
	}
	return seeded, nil
}
