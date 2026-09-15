import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('web recorder has bounded pre-roll and recording duration', async () => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  assert.match(html, /maxPreRollBytes=16000\*2/);
  assert.match(html, /preRoll\.shift\(\)/);
  assert.match(html, /maxRecordingBytes=16000\*2\*30/);
  assert.match(html, /recordedBytes>=maxRecordingBytes\)\{finish\(true\)/);
});
