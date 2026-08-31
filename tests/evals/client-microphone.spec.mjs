import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

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

test('普通说话音量会触发说话和句子结束事件', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    const frames = [
      0,
      0,
      0.024,
      0.018,
      0.026,
      0.017,
      0.022,
      0.019,
      0,
      0,
    ];
    const stream = { getTracks: () => [{ stop() {} }] };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => stream },
    });

    class FakeAudioContext {
      sampleRate = 48000;
      state = 'suspended';
      destination = {};

      async resume() {
        this.state = 'running';
        window.__qAudioContextResumed = true;
      }

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

    window.AudioContext = FakeAudioContext;
    window.webkitAudioContext = FakeAudioContext;
  });
  await page.route('http://127.0.0.1:41730/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41730/');

  await page.evaluate(() => {
    window.__qRecorderEvents = [];
    window.__qRecStart();
  });

  await expect
    .poll(
      () =>
        page.evaluate(() => {
          window.__qRecorderEvents.push(...window.__qRecDrain());
          const events = window.__qRecorderEvents;
          const listeningIndex = events.findIndex((event) => event.e === 'listening');
          const speakingIndex = events.findIndex((event) => event.e === 'speaking');
          const sentenceEndIndex = events.findIndex((event) => event.e === 'sentenceEnd');
          const chunks = events.filter((event) => event.e === 'audioChunk');
          return (
            window.__qAudioContextResumed === true &&
            listeningIndex >= 0 &&
            speakingIndex > listeningIndex &&
            sentenceEndIndex > speakingIndex &&
            chunks.length > 0 &&
            chunks.every(
              (event) =>
                event.e === 'audioChunk' &&
                typeof event.a === 'string' &&
                event.a.length > 0,
            ) &&
            events.at(-1)?.e === 'sentenceEnd'
          );
        }),
      { timeout: 3000 },
    )
    .toBe(true);
});

test('部署客户端能从浏览器麦克风接收说话事件', async ({ page }) => {
  const sentFrames = [];
  page.on('websocket', (socket) => {
    socket.on('framesent', (event) => sentFrames.push(event.payload));
  });
  await page.addInitScript(() => {
    localStorage.setItem('flutter.first_meeting_completed', 'true');
    window.__qReleaseSpeech = false;
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
          {
            kind: 'audioinput',
            deviceId: 'test-microphone',
            label: '测试麦克风',
          },
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

  await page.goto('/');
  await page.waitForFunction(() => typeof window.__qRecStart === 'function');
  await page.waitForTimeout(1500);
  await page.evaluate(() => {
    window.__qReleaseSpeech = true;
  });

  await expect
    .poll(
      () =>
        sentFrames.some((frame) =>
          String(frame).includes('"type":"asr_chunk"'),
        ),
      { timeout: 8000 },
    )
    .toBe(true);
});

test('选择外接麦克风后录音使用精确的设备输入', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    const devices = [
      { kind: 'audioinput', deviceId: 'built-in', label: '内置麦克风' },
      { kind: 'audioinput', deviceId: 'usb-condenser', label: 'USB 电容麦克风' },
    ];
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        addEventListener() {},
        enumerateDevices: async () => devices,
        getUserMedia: async (constraints) => {
          const deviceId = constraints.audio.deviceId?.exact || 'built-in';
          window.__qRequestedAudioDevice = deviceId;
          const selected = devices.find((device) => device.deviceId === deviceId);
          const track = {
            label: selected?.label || '内置麦克风',
            getSettings: () => ({ deviceId }),
            stop() {},
          };
          return {
            getAudioTracks: () => [track],
            getTracks: () => [track],
          };
        },
      },
    });

    class DeviceAudioContext {
      sampleRate = 48000;
      state = 'running';
      destination = {};
      createMediaStreamSource() {
        return { connect() {} };
      }
      createScriptProcessor() {
        return { onaudioprocess: null, connect() {}, disconnect() {} };
      }
      close() {
        this.state = 'closed';
      }
    }
    window.AudioContext = DeviceAudioContext;
    window.webkitAudioContext = DeviceAudioContext;
  });
  await page.route('http://127.0.0.1:41731/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41731/');

  await page.evaluate(async () => {
    await window.__qRecSelectDevice('usb-condenser');
    window.__qRecStart();
  });

  await expect
    .poll(() => page.evaluate(() => window.__qRequestedAudioDevice))
    .toBe('usb-condenser');
  await expect
    .poll(() =>
      page.evaluate(() =>
        window
          .__qRecDrain()
          .some(
            (event) =>
              event.e === 'listening' &&
              event.deviceId === 'usb-condenser' &&
              event.deviceLabel === 'USB 电容麦克风',
          ),
      ),
    )
    .toBe(true);
});

test('已保存的麦克风被拔出后自动回退系统默认输入', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    localStorage.setItem('qiuqiu.operator.voiceDeviceId', 'missing-device');
    localStorage.setItem('qiuqiu.operator.voiceDevicePreferenceSet', '1');
    window.__qRequestedAudioDevices = [];
    const builtIn = {
      kind: 'audioinput',
      deviceId: 'built-in',
      label: '内置麦克风',
    };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        addEventListener() {},
        enumerateDevices: async () => [builtIn],
        getUserMedia: async (constraints) => {
          const deviceId = constraints.audio.deviceId?.exact || '';
          window.__qRequestedAudioDevices.push(deviceId || 'default');
          if (deviceId === 'missing-device') {
            const error = new Error('device unavailable');
            error.name = 'OverconstrainedError';
            throw error;
          }
          const track = {
            label: builtIn.label,
            getSettings: () => ({ deviceId: builtIn.deviceId }),
            stop() {},
          };
          return {
            getAudioTracks: () => [track],
            getTracks: () => [track],
          };
        },
      },
    });

    class FallbackAudioContext {
      sampleRate = 48000;
      state = 'running';
      destination = {};
      createMediaStreamSource() {
        return { connect() {} };
      }
      createScriptProcessor() {
        return { onaudioprocess: null, connect() {}, disconnect() {} };
      }
      close() {
        this.state = 'closed';
      }
    }
    window.AudioContext = FallbackAudioContext;
    window.webkitAudioContext = FallbackAudioContext;
  });
  await page.route('http://127.0.0.1:41732/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41732/');

  await page.evaluate(() => window.__qRecStart());

  await expect
    .poll(() => page.evaluate(() => window.__qRequestedAudioDevices))
    .toEqual(['missing-device', 'default']);
  await expect
    .poll(() =>
      page.evaluate(() => ({
        selected: localStorage.getItem('qiuqiu.operator.voiceDeviceId'),
        listening: window
          .__qRecDrain()
          .some(
            (event) =>
              event.e === 'listening' && event.deviceId === 'built-in',
          ),
      })),
    )
    .toEqual({ selected: null, listening: true });
});

test('切换输入时丢弃延迟返回的旧麦克风', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    const devices = [
      { kind: 'audioinput', deviceId: 'built-in', label: '内置麦克风' },
      { kind: 'audioinput', deviceId: 'usb-condenser', label: 'USB 电容麦克风' },
    ];
    window.__qOldTrackStopped = false;
    window.__qAudioContexts = 0;
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        addEventListener() {},
        enumerateDevices: async () => devices,
        getUserMedia: async (constraints) => {
          const deviceId = constraints.audio.deviceId?.exact || 'built-in';
          const track = {
            label: devices.find((device) => device.deviceId === deviceId)?.label,
            getSettings: () => ({ deviceId }),
            stop() {
              if (deviceId === 'built-in') window.__qOldTrackStopped = true;
            },
          };
          const stream = {
            getAudioTracks: () => [track],
            getTracks: () => [track],
          };
          if (deviceId === 'built-in') {
            return new Promise((resolve) => {
              window.__qResolveOldMicrophone = () => resolve(stream);
            });
          }
          return stream;
        },
      },
    });

    class SwitchingAudioContext {
      sampleRate = 48000;
      state = 'running';
      destination = {};
      constructor() {
        window.__qAudioContexts += 1;
      }
      createMediaStreamSource() {
        return { connect() {} };
      }
      createScriptProcessor() {
        return { onaudioprocess: null, connect() {}, disconnect() {} };
      }
      close() {
        this.state = 'closed';
      }
    }
    window.AudioContext = SwitchingAudioContext;
    window.webkitAudioContext = SwitchingAudioContext;
  });
  await page.route('http://127.0.0.1:41733/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41733/');

  await page.evaluate(() => window.__qRecStart());
  await page.waitForFunction(() => typeof window.__qResolveOldMicrophone === 'function');
  await page.evaluate(async () => {
    window.__qRecorderEvents = [];
    await window.__qRecSelectDevice('usb-condenser');
    window.__qRecStart();
  });
  await expect
    .poll(() =>
      page.evaluate(() => {
        window.__qRecorderEvents.push(...window.__qRecDrain());
        return window.__qRecorderEvents.filter(
          (event) => event.e === 'listening',
        ).length;
      }),
    )
    .toBe(1);

  await page.evaluate(() => window.__qResolveOldMicrophone());
  await page.waitForTimeout(200);
  await expect
    .poll(() =>
      page.evaluate(() => {
        window.__qRecorderEvents.push(...window.__qRecDrain());
        return {
          contexts: window.__qAudioContexts,
          listening: window.__qRecorderEvents.filter(
            (event) => event.e === 'listening',
          ).length,
          oldStopped: window.__qOldTrackStopped,
        };
      }),
    )
    .toEqual({ contexts: 1, listening: 1, oldStopped: true });
});

test('欢迎流程不会阻塞部署客户端的流式语音转写', async ({ page }) => {
  const sentFrames = [];
  const receivedFrames = [];
  page.on('websocket', (socket) => {
    socket.on('framesent', (event) => sentFrames.push(event.payload));
    socket.on('framereceived', (event) => receivedFrames.push(event.payload));
  });

  await page.addInitScript(() => {
    localStorage.setItem('flutter.first_meeting_completed', 'false');
    localStorage.setItem('qiuqiu.operator.voiceDeviceId', 'usb-condenser');
    localStorage.setItem('qiuqiu.operator.voiceDevicePreferenceSet', '1');
    window.__qReleaseSpeech = false;
    window.__qRequestedAudioDevices = [];
    const frames = [0, 0, 0.024, 0.018, 0.026, 0.017, 0.022, 0.019, 0, 0];
    const devices = [
      { kind: 'audioinput', deviceId: 'built-in', label: '内置麦克风' },
      { kind: 'audioinput', deviceId: 'usb-condenser', label: 'USB 电容麦克风' },
    ];
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        addEventListener() {},
        enumerateDevices: async () => devices,
        getUserMedia: async (constraints) => {
          const deviceId = constraints.audio.deviceId?.exact || 'built-in';
          window.__qRequestedAudioDevices.push(deviceId);
          const selected = devices.find((device) => device.deviceId === deviceId);
          const track = {
            label: selected?.label || '内置麦克风',
            getSettings: () => ({ deviceId }),
            stop() {},
          };
          return {
            getAudioTracks: () => [track],
            getTracks: () => [track],
          };
        },
      },
    });

    class FlowAudioContext {
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
          if (!window.__qReleaseSpeech || !processor.onaudioprocess) return;
          if (index >= frames.length) {
            clearInterval(timer);
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

    window.AudioContext = FlowAudioContext;
    window.webkitAudioContext = FlowAudioContext;
  });

  const countSent = (type) =>
    sentFrames.filter((frame) => String(frame).includes(`"type":"${type}"`))
      .length;
  const received = (pattern) =>
    receivedFrames.some((frame) => pattern.test(String(frame)));
  const receivedSummary = () =>
    receivedFrames
      .map((frame) => {
        try {
          const message = JSON.parse(String(frame));
          return [
            message.type,
            message.state,
            message.event,
            message.data?.source,
          ]
            .filter(Boolean)
            .join(':');
        } catch {
          return 'binary';
        }
      })
      .slice(-30);

  await page.goto('/');
  await page.waitForFunction(() => typeof window.__qRecStart === 'function');

  await expect
    .poll(() => page.evaluate(() => window.__qRequestedAudioDevices.at(-1)))
    .toBe('usb-condenser');
  await expect.poll(() => countSent('asr_start'), { timeout: 8000 }).toBeGreaterThan(0);
  await expect.poll(() => {
    for (const frame of sentFrames) {
      try {
        const message = JSON.parse(String(frame));
        if (message.type === 'asr_start') return message.timezone || '';
      } catch {}
    }
    return '';
  }).not.toBe('');
  await expect
    .poll(
      () =>
        receivedFrames.some((frame) => {
          const payload = String(frame);
          return (
            payload.includes('"type":"first_meeting_status"') &&
            payload.includes('"state":"delivered"')
          );
        }),
      {
        timeout: 12000,
        message: `received=${JSON.stringify(receivedSummary())}`,
      },
    )
    .toBe(true);
  await page.evaluate(() => {
    window.__qReleaseSpeech = true;
  });
  await expect.poll(() => countSent('asr_finish'), { timeout: 8000 }).toBeGreaterThan(0);
  await expect
    .poll(() => received(/"type":"transcript_(partial|final|error)"/), {
      timeout: 15000,
    })
    .toBe(true);

  const startsBeforeReload = countSent('asr_start');
  await page.reload();
  await page.waitForFunction(() => typeof window.__qRecStart === 'function');

  await expect
    .poll(() => page.evaluate(() => window.__qRequestedAudioDevices.at(-1)))
    .toBe('usb-condenser');
  await expect
    .poll(() => countSent('asr_start'), { timeout: 8000 })
    .toBeGreaterThan(startsBeforeReload);
  await expect
    .poll(
      () =>
        receivedFrames.some((frame) => {
          const payload = String(frame);
          return (
            payload.includes('"type":"first_meeting_status"') &&
            payload.includes('"state":"skipped"')
          );
        }),
      {
        timeout: 8000,
        message: `received=${JSON.stringify(receivedSummary())}`,
      },
    )
    .toBe(true);
});
