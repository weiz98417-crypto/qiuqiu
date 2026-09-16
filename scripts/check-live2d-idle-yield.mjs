import { readFile } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// presentation-mapping 3.4 (ADR-0007 ownership, offline tier): the Live2D
// page's idle scheduler must yield while a presentation hold (HoldMS armed by
// the last qiuqiu-live2d-state apply) is live and only fill idle-phase gaps
// once it elapses. This check extracts the real declarations and functions
// (scheduleIdle / armPresentationHold / setSpeaking) from
// client/assets/live2d/live2d.html and drives them in a stubbed sandbox —
// the same pattern as check-presentation-map.mjs for the mapping JSON.
const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const htmlPath = join(repoRoot, 'client', 'assets', 'live2d', 'live2d.html');
const html = await readFile(htmlPath, 'utf8');
const failures = [];

function extractVar(name) {
  const match = html.match(new RegExp(`^var ${name} = .*;$`, 'm'));
  if (!match) failures.push(`live2d.html: missing "var ${name}" declaration`);
  return match ? match[0] : '';
}

function extractObject(name) {
  const start = html.indexOf(`var ${name} = `);
  if (start === -1) {
    failures.push(`live2d.html: missing "var ${name}" declaration`);
    return '';
  }
  return extractBraced(start, `var ${name}`);
}

function extractFunction(name) {
  const start = html.indexOf(`function ${name}(`);
  if (start === -1) {
    failures.push(`live2d.html: missing function ${name}`);
    return '';
  }
  return extractBraced(start, `function ${name}`);
}

function extractBraced(start, label) {
  const bodyStart = html.indexOf('{', start);
  let depth = 0;
  for (let index = bodyStart; index >= 0 && index < html.length; index++) {
    if (html[index] === '{') depth++;
    else if (html[index] === '}') {
      depth--;
      if (depth === 0) return html.slice(start, index + 1);
    }
  }
  failures.push(`live2d.html: unbalanced braces in ${label}`);
  return '';
}

const declarations = [
  'speaking', 'currentMode', 'modeUntil', 'presentationHoldUntil',
  'PRESENTATION_DEFAULT_HOLD_MS', 'idleTimer',
].map(extractVar).filter(Boolean);
const motionAliases = extractObject('motionAliases');
const functions = [
  'scheduleIdle', 'armPresentationHold', 'setSpeaking',
].map(extractFunction).filter(Boolean);

// The message handler must arm the hold before the speaking update so the
// mode reset cannot erase a hold the same apply just started.
const armCall = html.indexOf('armPresentationHold(data);');
const speakingCall = html.indexOf('setSpeaking(data.speaking === true);');
if (armCall === -1 || speakingCall === -1 || armCall > speakingCall) {
  failures.push('live2d.html: the state handler must call armPresentationHold(data) before setSpeaking(...)');
}

if (!failures.length) {
  const machine = new Function(`
var now = 1000000;
var Date = { now: function() { return now; } };
var document = { body: { dataset: {} } };
var timerCallback = null;
function setTimeout(fn) { timerCallback = fn; return 1; }
function clearTimeout() { timerCallback = null; }
var performed = [];
function performAction(action) { performed.push(action); }
${[...declarations, motionAliases, ...functions].join('\n')}
return {
  set now(value) { now = value; },
  get currentMode() { return currentMode; },
  set currentMode(value) { currentMode = value; },
  get modeUntil() { return modeUntil; },
  set modeUntil(value) { modeUntil = value; },
  get presentationHoldUntil() { return presentationHoldUntil; },
  reset: function() {
    now = 1000000;
    speaking = false;
    currentMode = 'idle';
    modeUntil = 0;
    presentationHoldUntil = 0;
    performed.length = 0;
    timerCallback = null;
    scheduleIdle();
  },
  tickIdle() { var callback = timerCallback; timerCallback = null; if (callback) callback(); },
  performed: function() { return performed.slice(); },
  armPresentationHold: armPresentationHold,
  setSpeaking: setSpeaking,
};
`)();

  const expect = (description, actual, expected) => {
    const got = JSON.stringify(actual);
    const want = JSON.stringify(expected);
    if (got !== want) failures.push(`${description}: got ${got}, want ${want}`);
  };
  const t0 = 1000000;

  // A presentation apply arms the hold and the idle tick yields until it
  // elapses, then the idle layer resumes.
  machine.reset();
  machine.now = t0;
  machine.currentMode = 'event';
  machine.armPresentationHold({ holdMs: 2600 });
  expect('hold deadline armed', machine.presentationHoldUntil, t0 + 2600);
  machine.now = t0 + 1000;
  machine.tickIdle();
  expect('idle tick yields during the hold', machine.performed(), []);
  machine.now = t0 + 2601;
  machine.tickIdle();
  expect('idle layer resumes after the hold', machine.performed(), ['idle']);

  // The speaking-off mode reset must not cut a live hold short.
  machine.reset();
  machine.now = t0;
  machine.currentMode = 'event';
  machine.armPresentationHold({ holdMs: 2600 });
  machine.setSpeaking(false);
  expect('reset preserves the mode during a hold', machine.currentMode, 'event');
  expect('reset preserves the hold deadline', machine.presentationHoldUntil, t0 + 2600);
  if (machine.modeUntil < t0 + 2600) {
    failures.push(`reset lowered modeUntil below the hold: ${machine.modeUntil}`);
  }

  // With no hold live the reset behaves as before.
  machine.reset();
  machine.now = t0;
  machine.currentMode = 'speak';
  machine.modeUntil = t0 + 3000;
  machine.setSpeaking(false);
  expect('reset without a hold', [machine.currentMode, machine.modeUntil, machine.presentationHoldUntil], ['idle', 0, 0]);

  // Idle-phase applies (the client idle tier) arm nothing.
  machine.reset();
  machine.now = t0;
  machine.currentMode = 'idle';
  machine.armPresentationHold({ holdMs: 2600 });
  expect('idle mode arms nothing', machine.presentationHoldUntil, 0);
  machine.currentMode = 'event';
  machine.armPresentationHold({ motion: 'idle_02', holdMs: 2600 });
  expect('idle-tier motion arms nothing', machine.presentationHoldUntil, 0);

  // Applies without holdMs fall back to the backend default hold.
  machine.reset();
  machine.now = t0;
  machine.currentMode = 'event';
  machine.armPresentationHold({});
  expect('default HoldMS fallback', machine.presentationHoldUntil, t0 + 1800);

  // The speak window never shortens a live hold.
  machine.reset();
  machine.now = t0;
  machine.currentMode = 'event';
  machine.armPresentationHold({ holdMs: 5000 });
  machine.setSpeaking(true);
  if (machine.modeUntil < t0 + 5000) {
    failures.push(`speak window shortened a live hold: modeUntil ${machine.modeUntil}`);
  }
}

if (failures.length) {
  throw new Error(`live2d.html idle-yield ownership check failed:\n  - ${failures.join('\n  - ')}`);
}
console.log('live2d.html idle-yield OK: scheduleIdle yields during presentation holds and resumes after');
