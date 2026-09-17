import { expect, test } from '@playwright/test';
import { openTextMode } from './support/open-text-mode.mjs';

const token = process.env.APP_TOKEN || 'qiuqiu-dev-token';
const matchId = 'test';

test.use({
  permissions: ['microphone'],
  launchOptions: {
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
      : {}),
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
    ],
  },
});

test.beforeEach(async ({ page, request }) => {
  await page.addInitScript((value) => {
    localStorage.setItem('qiuqiu.app.token', value);
    localStorage.setItem('flutter.first_meeting_completed', 'true');
  }, token);
  // 顶层帧：伪造麦克风（与 client-microphone.spec.mjs 的部署客户端测试同一
  // 套路）——getUserMedia 返回假轨道，AudioContext 按帧重放一段说话音量，
  // 用 window.__qReleaseSpeech 控制放音时机。iframe 里不安装，避免干扰
  // live2d 页自己的 qLipSync AudioContext。
  await page.addInitScript(() => {
    if (window !== window.top) return;
    const frames = [0, 0, 0.024, 0.018, 0.026, 0.017, 0.022, 0.019, 0, 0];
    const track = {
      label: '测试麦克风',
      getSettings: () => ({ deviceId: 'test-microphone' }),
      stop() {},
    };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        addEventListener() {},
        enumerateDevices: async () => [
          { kind: 'audioinput', deviceId: 'test-microphone', label: '测试麦克风' },
        ],
        getUserMedia: async () => ({
          getAudioTracks: () => [track],
          getTracks: () => [track],
        }),
      },
    });

    class IntegrationAudioContext {
      sampleRate = 48000;
      state = 'running';
      destination = {};

      createMediaStreamSource() {
        return { connect() {} };
      }

      createScriptProcessor() {
        const processor = {
          onaudioprocess: null,
          connect() {},
          disconnect() {},
        };
        let index = 0;
        const timer = setInterval(() => {
          if (!window.__qReleaseSpeech) return;
          if (!processor.onaudioprocess || index >= frames.length) {
            if (index >= frames.length) clearInterval(timer);
            return;
          }
          const input = new Float32Array(4096);
          input.fill(frames[index]);
          index += 1;
          processor.onaudioprocess({
            inputBuffer: { getChannelData: () => input },
          });
        }, 20);
        return processor;
      }

      close() {
        this.state = 'closed';
      }
    }

    window.AudioContext = IntegrationAudioContext;
    window.webkitAudioContext = IntegrationAudioContext;
  });
  // 所有帧（含 live2d iframe）：记录 Flutter 侧 live2d 桥
  // （live2d_bridge_web.dart sendLive2dState）发来的每一次
  // qiuqiu-live2d-state 表演应用——这是表演相位在用户侧的可观察载体。
  await page.addInitScript(() => {
    window.__qLive2dStates = [];
    window.addEventListener('message', (event) => {
      const data = event.data;
      if (data && data.type === 'qiuqiu-live2d-state') {
        window.__qLive2dStates.push({
          expression: data.expression ?? '',
          motion: data.motion ?? '',
          speaking: data.speaking === true,
          at: Date.now(),
        });
      }
    });
  });

  await apiPost(request, `/api/matches/${matchId}/reset`, {});
  await apiPost(request, `/api/matches/${matchId}/config`, {
    homeTeam: '西班牙',
    awayTeam: '德国',
    homePlayers: [
      { number: '10', name: '佩德里', position: 'CM' },
      { number: '8', name: '法比安', position: 'CM' },
      { number: '19', name: '亚马尔', position: 'RW' },
    ],
    awayPlayers: [{ number: '10', name: '穆西亚拉', position: 'AM' }],
  });
  await startMatchClock(request, 1421);
});

test('假麦克风说话触发 user_speaking 的听姿，球球回话时切到说姿', async ({ page, request }) => {
  test.setTimeout(90_000);
  const sentFrames = [];
  page.on('websocket', (socket) => {
    socket.on('framesent', (event) => sentFrames.push(String(event.payload)));
  });

  await page.goto(matchURL());
  await enableAccessibility(page);
  await expect(page.getByText('比赛已连接')).toBeVisible({ timeout: 15_000 });

  // 等部署客户端暴露录音入口并完成采集启动（与 client-microphone 同一
  // 节奏），然后向假设备放一段说话音量的帧。
  await page.waitForFunction(() => typeof window.__qRecStart === 'function');
  await page.waitForTimeout(1500);
  await page.evaluate(() => {
    window.__qReleaseSpeech = true;
  });

  // 说话被 VAD 识别：控制器进入 user_speaking，向后台发 user_activity，
  // 并把身体切到 phases.user_speaking = listening/listen_01 的听姿。
  await expect
    .poll(
      () => sentFrames.some((frame) =>
        frame.includes('"type":"user_activity"') && frame.includes('"state":"speaking"'),
      ),
      { timeout: 10_000 },
    )
    .toBe(true);
  const live2dFrame = () => page.frames().find((frame) => frame.url().includes('/live2d.html'));
  await expect
    .poll(async () => {
      const frame = live2dFrame();
      if (!frame) return null;
      const states = await frame.evaluate(() => window.__qLive2dStates ?? []);
      return states.some((state) => state.expression === 'listening' && state.motion === 'listen_01');
    }, { timeout: 10_000 })
    .toBe(true);

  // 球球回话：文字回合先落 understanding 的 thinking/think，回答送达时
  // 进入 qiuqiu_speaking 的说姿（本环境的回答由后端 presentation 驱动，
  // 即 presentation-map 的 chat/speak 家族；与 phases.qiuqiu_speaking =
  // chat/speak_01 是同一说姿组）。
  await openTextMode(page);
  await sendText(page, '在吗？');
  await expect(page.getByText(/在，听着呢。/).last()).toBeVisible({ timeout: 30_000 });
  await expect
    .poll(async () => {
      const frame = live2dFrame();
      if (!frame) return null;
      const states = await frame.evaluate(() => window.__qLive2dStates ?? []);
      return states.some((state) => state.expression === 'thinking' && state.motion === 'think');
    }, { timeout: 10_000 })
    .toBe(true);
  await expect
    .poll(async () => {
      const frame = live2dFrame();
      if (!frame) return null;
      const states = await frame.evaluate(() => window.__qLive2dStates ?? []);
      return states.some((state) =>
        state.expression === 'chat' && ['speak', 'speak_01'].includes(state.motion));
    }, { timeout: 10_000 })
    .toBe(true);
});

async function enableAccessibility(page) {
  const button = page.getByRole('button', { name: 'Enable accessibility' });
  try {
    await button.waitFor({ state: 'visible', timeout: 3000 });
    await button.evaluate((element) => element.click());
  } catch {}
}

async function sendText(page, text) {
  const textbox = page.getByLabel('直接和球球说…');
  await expect(textbox).toBeVisible();
  await textbox.click();
  await textbox.pressSequentially(text, { delay: 5 });
  await page.getByRole('button', { name: '发送这句话' }).click();
}

async function apiPost(request, path, body) {
  const response = await request.post(path, {
    data: body,
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`POST ${path} failed: ${response.status()} ${await response.text()}`);
  }
  return response.json();
}

function testIdempotencyKey() {
  return `test-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function matchURL() {
  return `/?matchId=${encodeURIComponent(matchId)}`;
}

async function startMatchClock(request, elapsedSeconds) {
  const response = await request.patch(`/api/matches/${matchId}/clock`, {
    data: { action: 'set', period: 'first_half', elapsedSeconds, expectedVersion: 0 },
    headers: { Authorization: `Bearer ${token}`, 'Idempotency-Key': testIdempotencyKey() },
  });
  if (!response.ok()) {
    throw new Error(`PATCH clock failed: ${response.status()} ${await response.text()}`);
  }
}
