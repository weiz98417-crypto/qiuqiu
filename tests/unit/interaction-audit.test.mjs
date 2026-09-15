import assert from 'node:assert/strict';
import test from 'node:test';

import { auditInteractions, fetchInteractionLedger } from '../../scripts/evals/interaction-audit.mjs';

test('interaction audit fetches every cursor page before evaluating blockers', async () => {
  const requestedCursors = [];
  const pages = [
    {
      events: [{ id: 'turn', kind: 'turn_planned', traceId: 'trace-1' }],
      nextCursor: 'cursor-2',
    },
    {
      events: [{ id: 'delivery', kind: 'delivery', traceId: 'trace-1', deliveryState: 'completed' }],
      nextCursor: '',
    },
  ];
  const report = await fetchInteractionLedger({
    baseURL: 'http://example.test',
    matchID: 'match-1',
    userID: 'user-1',
    token: 'token',
    fetchImpl: async (url) => {
      requestedCursors.push(url.searchParams.get('cursor') ?? '');
      return { ok: true, json: async () => pages.shift() };
    },
  });
  assert.deepEqual(requestedCursors, ['', 'cursor-2']);
  assert.equal(report.pages, 2);
  assert.equal(report.events.length, 2);
  assert.equal(report.audit.blockers, 0);
});

test('interaction audit catches a duplicate audible completion across pages', () => {
  const report = auditInteractions([
    { kind: 'turn_planned', traceId: 'trace-1' },
    { kind: 'delivery', traceId: 'trace-1', deliveryState: 'text_delivered' },
    { kind: 'playback_result', traceId: 'trace-1', deliveryKey: 'key-1', playbackState: 'ended' },
    { kind: 'playback_result', traceId: 'trace-1', deliveryKey: 'key-1', playbackState: 'completed' },
  ]);
  assert.equal(report.blockers, 1);
  assert.equal(report.issues[0].message, 'delivery completed audible playback more than once');
});
