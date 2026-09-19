// Migration 序号 lint：backend/migrations/*.sql 的数字前缀不允许重复。
//
// 决策记录（2026-09-20，交付卫生打磨轮 #7）：
// - backend/internal/matchstate/postgres.go 的 runMigrations 按文件名排序执行、
//   并以 basename 记入 schema_migrations 账本——因此给已应用的迁移改名会让它
//   在已有部署上重放。仓库现存三组重复序号（010/011/012）无法确认是否已在
//   部署中应用，只能保持原样：本脚本对它们**向 stderr 告警但退出 0**。
// - 这份遗留清单是封闭的（下方 allowlist）：今后任何新引入的重复序号一律
//   退出 1 失败。修复新重复时，给较新的文件分配下一个空闲序号
//   （当前为 045 起），且仅当确认它从未在任何部署应用过时才可改名。
import { readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const migrationsDir = join(
  dirname(fileURLToPath(import.meta.url)),
  '..',
  'backend',
  'migrations',
);

// 历史遗留重复组（改名会触发已应用迁移重放，只告警不失败）。
const LEGACY_DUPLICATE_PREFIXES = new Set([
  '010', // 010_match_lifecycle.sql / 010_relationship_memory_evidence.sql
  '011', // 011_match_event_confirmation.sql / 011_match_metadata.sql
  '012', // 012_match_stats.sql / 012_relationship_memory_pending_decisions.sql
]);

/** 返回 [{ prefix, files }]：数字前缀出现多次的 .sql 分组，按前缀排序。 */
export async function findDuplicateMigrations(dir) {
  const entries = await readdir(dir);
  const groups = new Map();
  for (const name of entries) {
    if (!name.endsWith('.sql')) continue;
    const prefix = /^(\d+)/.exec(name)?.[1];
    if (!prefix) continue;
    if (!groups.has(prefix)) groups.set(prefix, []);
    groups.get(prefix).push(name);
  }
  const duplicates = [];
  for (const [prefix, files] of [...groups].sort()) {
    if (files.length > 1) duplicates.push({ prefix, files: files.sort() });
  }
  return duplicates;
}

async function main() {
  const duplicates = await findDuplicateMigrations(migrationsDir);
  const legacy = duplicates.filter((d) => LEGACY_DUPLICATE_PREFIXES.has(d.prefix));
  const novel = duplicates.filter((d) => !LEGACY_DUPLICATE_PREFIXES.has(d.prefix));

  for (const d of legacy) {
    console.error(
      `[warn] 重复数字前缀 ${d.prefix}_（历史遗留，保持原样——改名会使已应用迁移重放）:\n  ${d.files.join('\n  ')}`,
    );
  }
  if (novel.length > 0) {
    for (const d of novel) {
      console.error(
        `[error] 新的重复数字前缀 ${d.prefix}_:\n  ${d.files.join('\n  ')}`,
      );
    }
    console.error(
      '请给较新的文件分配下一个空闲序号（仅当确认从未在任何部署应用过时才可改名）。',
    );
    process.exit(1);
  }
  console.log('migrations 序号检查通过：无新增重复数字前缀。');
}

const invokedDirectly =
  process.argv[1] !== undefined &&
  import.meta.url === pathToFileURL(process.argv[1]).href;
if (invokedDirectly) await main();
