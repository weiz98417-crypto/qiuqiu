import { readFile } from 'node:fs/promises';

const app = await readFile('client/assets/live2d/app.html', 'utf8');
const live2d = await readFile('client/assets/live2d/live2d.html', 'utf8');

const checks = [
  ['user app exposes microphone control', app.includes('id="mic"') && app.includes('id="voiceStatus"')],
  ['user app grants microphone permission to Live2D iframe', app.includes('allow="microphone; autoplay"')],
  ['user app sends audio through user_speech WebSocket message', app.includes("type: 'user_speech'") && app.includes('audio: result.audio')],
  ['user app handles voice status and binary audio frames', app.includes("msg.type === 'voice_status'") && app.includes("msg.type === 'voice_audio'") && app.includes('playAudioBlob(event.data')],
  ['user app surfaces microphone failure fallback', app.includes('麦克风未授权或不可用，文字输入仍可用。') && app.includes("result.event === 'error'")],
  ['user app shows microphone permission pending state before recorder resolves', app.includes('等待浏览器麦克风授权。') && app.indexOf('等待浏览器麦克风授权。') < app.indexOf("liveCall('startRecording')")],
  ['user app reports browser playback status to trace', app.includes("type: 'voice_playback'") && app.includes('reportVoicePlayback(traceId') && app.includes('pendingAudioTraceId = msg.traceId')],
  ['user app keeps text reply independent of audio playback', app.includes("msg.event === 'qiuqiu_reply'") && app.includes('speakFor(text)')],
  ['user app avoids duplicate proactive playback from raw match_event payloads', !app.includes("if (eventData.proactiveText) {")],
  ['Live2D exports recorder controls to the parent app', live2d.includes('window.startRecording = startRecording') && live2d.includes('window.stopRecording = stopRecording') && live2d.includes('window.getVAD = getVAD')],
  ['Live2D recorder returns permission success or failure', live2d.includes('return {ok:true}') && live2d.includes('return {ok:false') && live2d.includes("event:'error'")],
  ['Live2D idle scheduler does not interrupt speaking', live2d.includes('if (!speaking && Date.now() > modeUntil)')],
];

const failures = checks.filter(([, ok]) => !ok);
if (failures.length) {
  console.error(JSON.stringify({ ok: false, failures: failures.map(([name]) => name) }, null, 2));
  process.exit(1);
}

console.log(JSON.stringify({ ok: true, checks: checks.map(([name]) => name) }, null, 2));
