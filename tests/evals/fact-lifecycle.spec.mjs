import { expect, test } from '@playwright/test';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

test('provisional fact stays private through confirmation, revocation, and correction', async ({ request }) => {
  await operatorPost(request, `/api/matches/${matchId}/reset`, {});
  await operatorPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: 'Barcelona',
    awayTeam: 'Real Madrid',
  });

  const created = await operatorPost(request, `/api/matches/${matchId}/events`, {
    source: 'api-sports',
    providerEventId: 'phase5-goal-candidate',
    eventType: 'goal',
    period: 'first_half',
    clock: '18:20',
    teamId: 'home',
    teamName: 'Barcelona',
    playerName: 'Candidate scorer',
    score: { home: 1, away: 0 },
    description: 'Unconfirmed goal candidate',
    proactiveText: '__quiet__',
    visibility: 'public',
    confirmed: false,
  });
  expect(created.event).toMatchObject({ factStatus: 'provisional', factRevision: 1, confirmed: false });
  await expectPublicScore(request, 0, 0);
  expect((await publicGet(request, `/api/matches/${matchId}/events`)).events).toHaveLength(0);
  expect((await operatorGet(request, `/api/matches/${matchId}/events`)).events[0].factStatus).toBe('provisional');

  const confirmed = await operatorPost(
    request,
    `/api/matches/${matchId}/facts/${created.event.factId}/confirm`,
    {},
  );
  expect(confirmed.event).toMatchObject({ factStatus: 'confirmed', factRevision: 2, confirmed: true });
  await expectPublicScore(request, 1, 0);
  expect((await publicGet(request, `/api/matches/${matchId}/events`)).events).toHaveLength(1);

  const revoked = await operatorPost(
    request,
    `/api/matches/${matchId}/facts/${created.event.factId}/revoke`,
    {},
  );
  expect(revoked.event).toMatchObject({ factStatus: 'revoked', factRevision: 3, confirmed: false });
  await expectPublicScore(request, 0, 0);
  expect((await publicGet(request, `/api/matches/${matchId}/events`)).events).toHaveLength(0);

  const corrected = await operatorPost(
    request,
    `/api/matches/${matchId}/events/${created.event.id}/correct`,
    {
      source: 'operator',
      eventType: 'goal',
      period: 'first_half',
      clock: '18:24',
      teamId: 'home',
      teamName: 'Barcelona',
      playerName: 'Corrected scorer',
      score: { home: 1, away: 0 },
      description: 'Corrected goal candidate',
      proactiveText: '__quiet__',
      visibility: 'public',
      factStatus: 'provisional',
      confirmed: false,
    },
  );
  expect(corrected.event).toMatchObject({
    factId: created.event.factId,
    factStatus: 'provisional',
    factRevision: 4,
    revisionOf: created.event.id,
    playerName: 'Corrected scorer',
  });
  expect(corrected.event.id).not.toBe(created.event.id);
  await expectPublicScore(request, 0, 0);

  const reconfirmed = await operatorPost(
    request,
    `/api/matches/${matchId}/facts/${created.event.factId}/confirm`,
    {},
  );
  expect(reconfirmed.event).toMatchObject({ factStatus: 'confirmed', factRevision: 5, confirmed: true });
  await expectPublicScore(request, 1, 0);

  const publicEvents = (await publicGet(request, `/api/matches/${matchId}/events`)).events;
  expect(publicEvents).toHaveLength(1);
  expect(publicEvents[0]).toMatchObject({ id: corrected.event.id, playerName: 'Corrected scorer' });
  expect(publicEvents[0].id).not.toBe(created.event.id);

  const ledger = (await operatorGet(request, `/api/matches/${matchId}/events`)).events;
  expect(ledger.find((event) => event.id === created.event.id)?.status).toBe('corrected');
  expect(ledger.find((event) => event.id === corrected.event.id)?.factStatus).toBe('confirmed');

  const revisions = await operatorGet(
    request,
    `/api/matches/${matchId}/facts/${created.event.factId}/revisions`,
  );
  expect(revisions.revisions.map((revision) => revision.status)).toEqual([
    'provisional',
    'confirmed',
    'revoked',
    'provisional',
    'confirmed',
  ]);
});

async function expectPublicScore(request, home, away) {
  const state = await publicGet(request, `/api/matches/${matchId}/state`);
  expect(state.snapshot.score).toEqual({ home, away });
}

async function operatorPost(request, path, body) {
  const response = await request.post(path, {
    data: body,
    headers: {
      Authorization: `Bearer ${token}`,
      'Idempotency-Key': `fact-lifecycle-${Date.now()}-${Math.random().toString(16).slice(2)}`,
    },
  });
  if (!response.ok()) throw new Error(`POST ${path} -> ${response.status()}: ${await response.text()}`);
  return response.json();
}

async function operatorGet(request, path) {
  return get(request, path, { Authorization: `Bearer ${token}` });
}

async function publicGet(request, path) {
  return get(request, path);
}

async function get(request, path, headers = {}) {
  const response = await request.get(path, { headers });
  if (!response.ok()) throw new Error(`GET ${path} -> ${response.status()}: ${await response.text()}`);
  return response.json();
}
