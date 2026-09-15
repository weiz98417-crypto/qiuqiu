import { writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const valueAfter = (name, fallback = '') => {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
};

export const auditInteractions = (events) => {
  const issues = [];
  const turns = new Map();
  const deliveryStates = new Map();
  const playbackCompleted = new Map();
  const mediaFailed = new Set();
  const textDelivered = new Set();
  const staleTurns = new Set();
  const factRevisions = new Map();
  for (const event of events) {
    if (event.kind !== 'fact_revision') continue;
    for (const factID of event.factIds ?? []) factRevisions.set(factID, event.factRevision ?? '');
  }
  const add = (traceId, category, message) => issues.push({ traceId, severity: 'blocker', category, message });
  for (const event of events) {
    switch (event.kind) {
      case 'turn_planned':
        turns.set(event.traceId, event);
        if ((event.factIds?.length ?? 0) > 0 && !(event.factRevision ?? '').trim()) {
          const grounded = event.factIds.every((factID) => (factRevisions.get(factID) ?? '').trim());
          if (!grounded) add(event.traceId, 'grounding', 'fact-backed turn has no fact revision');
        }
        break;
      case 'delivery':
        deliveryStates.set(event.traceId, event.deliveryState);
        if (event.deliveryState === 'text_delivered' || event.deliveryState === 'completed') textDelivered.add(event.traceId);
        break;
      case 'media_delivery':
        if (event.deliveryState === 'failed') mediaFailed.add(event.traceId);
        break;
      case 'playback_result':
        if (event.playbackState === 'completed' || event.playbackState === 'ended') {
          deliveryStates.set(event.traceId, 'completed');
          playbackCompleted.set(event.deliveryKey, (playbackCompleted.get(event.deliveryKey) ?? 0) + 1);
        }
        break;
      case 'turn_stale':
        staleTurns.add(event.traceId);
        break;
    }
  }
  for (const traceID of turns.keys()) {
    if (traceID && !staleTurns.has(traceID) && !deliveryStates.get(traceID)) add(traceID, 'delivery', 'planned turn has no delivery outcome');
    if (mediaFailed.has(traceID) && !textDelivered.has(traceID)) add(traceID, 'fallback', 'media failed without a delivered text fallback');
  }
  for (const [deliveryKey, count] of playbackCompleted) {
    if ((deliveryKey ?? '').trim() && count > 1) add(deliveryKey, 'delivery', 'delivery completed audible playback more than once');
  }
  return { auditedAt: new Date().toISOString(), blockers: issues.length, issues };
};

export const fetchInteractionLedger = async ({ baseURL, matchID, userID, token, fetchImpl = fetch }) => {
  const events = [];
  let cursor = '';
  let pages = 0;
  do {
    const url = new URL(`${baseURL.replace(/\/$/, '')}/api/matches/${encodeURIComponent(matchID)}/interaction`);
    if (userID) url.searchParams.set('userId', userID);
    url.searchParams.set('limit', '500');
    if (cursor) url.searchParams.set('cursor', cursor);
    const response = await fetchImpl(url, { headers: token ? { authorization: `Bearer ${token}` } : {} });
    if (!response.ok) throw new Error(`interaction audit endpoint returned ${response.status}`);
    const page = await response.json();
    events.push(...(page.events ?? []));
    cursor = page.nextCursor?.trim() ?? '';
    pages += 1;
  } while (cursor);
  return { events, pages, audit: auditInteractions(events) };
};

const main = async () => {
  const baseURL = valueAfter('--base-url', 'http://localhost:8080');
  const matchID = valueAfter('--match-id', 'test');
  const userID = valueAfter('--user-id', '');
  const output = valueAfter('--out', '');
  const token = process.env.APP_TOKEN?.trim() ?? '';
  const report = await fetchInteractionLedger({ baseURL, matchID, userID, token });
  if (report.audit.blockers > 0) throw new Error(`interaction audit blocked release: ${report.audit.blockers}`);
  if (output) await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
};

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main();
