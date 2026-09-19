import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { findDuplicateMigrations } from '../../scripts/lint-migrations.mjs';

async function withTempMigrations(files, fn) {
  const dir = await mkdtemp(join(tmpdir(), 'lint-migrations-'));
  try {
    for (const name of files) {
      await writeFile(join(dir, name), 'SELECT 1;\n');
    }
    return await fn(dir);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

test('unique numeric prefixes pass', async () => {
  await withTempMigrations(
    ['001_a.sql', '002_b.sql', '010_c.sql'],
    async (dir) => {
      assert.deepEqual(await findDuplicateMigrations(dir), []);
    },
  );
});

test('a newly introduced duplicate prefix is reported', async () => {
  await withTempMigrations(
    ['001_a.sql', '002_b.sql', '002_c.sql'],
    async (dir) => {
      const duplicates = await findDuplicateMigrations(dir);
      assert.equal(duplicates.length, 1);
      assert.equal(duplicates[0].prefix, '002');
      assert.deepEqual(duplicates[0].files, ['002_b.sql', '002_c.sql']);
    },
  );
});

test('multiple duplicate groups are all reported in prefix order', async () => {
  await withTempMigrations(
    ['010_x.sql', '010_y.sql', '011_p.sql', '011_q.sql', '012_r.sql'],
    async (dir) => {
      const duplicates = await findDuplicateMigrations(dir);
      assert.deepEqual(
        duplicates.map((d) => d.prefix),
        ['010', '011'],
      );
    },
  );
});

test('non-sql files and files without a numeric prefix are ignored', async () => {
  await withTempMigrations(
    ['001_a.sql', 'README.md', 'notes.sql', '.gitkeep'],
    async (dir) => {
      assert.deepEqual(await findDuplicateMigrations(dir), []);
    },
  );
});
