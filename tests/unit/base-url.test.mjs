import assert from 'node:assert/strict';
import test from 'node:test';

import { qiuqiuBaseURL } from '../evals/support/base-url.mjs';

test('runtime client uses the configured ephemeral backend port', () => {
  assert.equal(
    qiuqiuBaseURL({ QIUQIU_BASE_URL: 'http://127.0.0.1:43127/' }),
    'http://127.0.0.1:43127',
  );
});

test('runtime client defaults to the local backend', () => {
  assert.equal(qiuqiuBaseURL({}), 'http://127.0.0.1:8080');
});
