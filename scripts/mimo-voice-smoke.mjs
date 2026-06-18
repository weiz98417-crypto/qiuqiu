const key = process.env.MIMO_API_KEY;
if (!key) {
  console.error(JSON.stringify({ ok: false, error: 'MIMO_API_KEY is required' }, null, 2));
  process.exit(1);
}

const url = `${process.env.MIMO_BASE_URL || 'https://api.xiaomimimo.com/v1'}/chat/completions`;
const sampleText = Buffer.from('e5889ae6898de8bf99e79083e698afe6b395e6af94e5ae89e58aa9e694bbefbc8ce4ba9ae9a9ace5b094e58f82e4b88ee7ad96e58aa8e38082', 'hex').toString('utf8');

async function post(payload) {
  const started = Date.now();
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'api-key': key, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  const text = await res.text();
  let json;
  try {
    json = JSON.parse(text);
  } catch {
    json = { raw: text.slice(0, 300) };
  }
  return { ok: res.ok, status: res.status, ms: Date.now() - started, json };
}

function messageText(json) {
  const message = json?.choices?.[0]?.message || {};
  return message.content || message.reasoning_content || '';
}

const tts = await post({
  model: 'mimo-v2.5-tts',
  messages: [{ role: 'assistant', content: sampleText }],
  audio: { format: 'wav', voice: process.env.MIMO_VOICE || 'Chloe' },
});

const audio = tts.json?.choices?.[0]?.message?.audio?.data || '';
const audioBytes = audio ? Buffer.from(audio.includes(',') ? audio.split(',').at(-1) : audio, 'base64').length : 0;
if (!tts.ok || !audioBytes) {
  console.error(JSON.stringify({
    ok: false,
    step: 'tts',
    status: tts.status,
    ms: tts.ms,
    error: JSON.stringify(tts.json).slice(0, 500),
  }, null, 2));
  process.exit(1);
}

const asr = await post({
  model: 'mimo-v2.5-asr',
  messages: [{
    role: 'user',
    content: [{ type: 'input_audio', input_audio: { data: `data:audio/wav;base64,${audio}` } }],
  }],
  asr_options: { language: 'zh' },
});

const recognized = messageText(asr.json);
const expectedAnchors = ['法比安', '亚马尔'];
const matchedAnchors = expectedAnchors.filter((anchor) => recognized.includes(anchor));
const acceptable = asr.ok && matchedAnchors.length >= 1;

console.log(JSON.stringify({
  ok: acceptable,
  tts: { status: tts.status, ms: tts.ms, audioBytes },
  asr: { status: asr.status, ms: asr.ms, text: recognized, matchedAnchors },
}, null, 2));

if (!acceptable) process.exit(1);
