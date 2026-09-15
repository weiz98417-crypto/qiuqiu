import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

test('a natural one-second pause stays inside the same utterance', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    const stream = { getTracks: () => [{ stop() {} }] };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => stream },
    });

    class PausedSpeechAudioContext {
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
        window.__qPumpMicrophoneFrame = (level) => {
          const input = new Float32Array(4096);
          input.fill(level);
          processor.onaudioprocess?.({
            inputBuffer: { getChannelData: () => input },
          });
        };
        return processor;
      }

      close() {
        this.state = 'closed';
      }
    }

    window.AudioContext = PausedSpeechAudioContext;
    window.webkitAudioContext = PausedSpeechAudioContext;
  });
  await page.route('http://127.0.0.1:41734/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41734/');
  await page.evaluate(() => {
    window.__qRecorderEvents = [];
    window.__qRecStart();
  });
  await page.waitForFunction(
    () => typeof window.__qPumpMicrophoneFrame === 'function',
  );

  await page.evaluate(() => {
    window.__qPumpMicrophoneFrame(0.024);
    window.__qPumpMicrophoneFrame(0.024);
    window.__qPumpMicrophoneFrame(0);
  });
  await page.waitForTimeout(1000);
  expect(
    await page.evaluate(() => {
      window.__qRecorderEvents.push(...window.__qRecDrain());
      return window.__qRecorderEvents.some((event) => event.e === 'sentenceEnd');
    }),
  ).toBe(false);

  await page.evaluate(() => {
    window.__qPumpMicrophoneFrame(0.024);
    window.__qPumpMicrophoneFrame(0.024);
    window.__qPumpMicrophoneFrame(0);
  });
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          window.__qRecorderEvents.push(...window.__qRecDrain());
          return window.__qRecorderEvents.filter(
            (event) => event.e === 'sentenceEnd',
          ).length;
        }),
      { timeout: 3000 },
    )
    .toBe(1);
});

test('steady outdoor noise does not create a phantom utterance', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    const stream = { getTracks: () => [{ stop() {} }] };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => stream },
    });

    class OutdoorNoiseAudioContext {
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
        window.__qPumpMicrophoneFrame = (level) => {
          const input = new Float32Array(4096);
          input.fill(level);
          processor.onaudioprocess?.({
            inputBuffer: { getChannelData: () => input },
          });
        };
        return processor;
      }

      close() {
        this.state = 'closed';
      }
    }

    window.AudioContext = OutdoorNoiseAudioContext;
    window.webkitAudioContext = OutdoorNoiseAudioContext;
  });
  await page.route('http://127.0.0.1:41735/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41735/');
  await page.evaluate(() => {
    window.__qRecorderEvents = [];
    window.__qRecStart();
  });
  await page.waitForFunction(
    () => typeof window.__qPumpMicrophoneFrame === 'function',
  );

  await page.evaluate(() => {
    for (let frame = 0; frame < 12; frame += 1) {
      window.__qPumpMicrophoneFrame(0.012);
    }
    window.__qPumpMicrophoneFrame(0);
  });
  await page.waitForTimeout(1600);

  expect(
    await page.evaluate(() => {
      window.__qRecorderEvents.push(...window.__qRecDrain());
      return window.__qRecorderEvents.filter((event) =>
        ['speaking', 'audioChunk', 'sentenceEnd'].includes(event.e),
      );
    }),
  ).toEqual([]);
});

test('quiet speech stays active after the higher start threshold', async ({ page }) => {
  const html = await readFile(
    new URL('../../client/web/index.html', import.meta.url),
    'utf8',
  );

  await page.addInitScript(() => {
    const stream = { getTracks: () => [{ stop() {} }] };
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => stream },
    });

    class QuietSpeechAudioContext {
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
        window.__qPumpMicrophoneFrame = (level) => {
          const input = new Float32Array(4096);
          input.fill(level);
          processor.onaudioprocess?.({
            inputBuffer: { getChannelData: () => input },
          });
        };
        return processor;
      }

      close() {
        this.state = 'closed';
      }
    }

    window.AudioContext = QuietSpeechAudioContext;
    window.webkitAudioContext = QuietSpeechAudioContext;
  });
  await page.route('http://127.0.0.1:41736/**', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.abort();
      return;
    }
    await route.fulfill({ contentType: 'text/html', body: html });
  });
  await page.goto('http://127.0.0.1:41736/');
  await page.evaluate(() => {
    window.__qRecorderEvents = [];
    window.__qRecStart();
  });
  await page.waitForFunction(
    () => typeof window.__qPumpMicrophoneFrame === 'function',
  );

  await page.evaluate(async () => {
    window.__qPumpMicrophoneFrame(0.024);
    window.__qPumpMicrophoneFrame(0.024);
    for (let frame = 0; frame < 7; frame += 1) {
      window.__qPumpMicrophoneFrame(0.01);
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  });
  expect(
    await page.evaluate(() => {
      window.__qRecorderEvents.push(...window.__qRecDrain());
      return window.__qRecorderEvents.some((event) => event.e === 'sentenceEnd');
    }),
  ).toBe(false);

  await page.evaluate(() => window.__qPumpMicrophoneFrame(0));
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          window.__qRecorderEvents.push(...window.__qRecDrain());
          return window.__qRecorderEvents.filter(
            (event) => event.e === 'sentenceEnd',
          ).length;
        }),
      { timeout: 3000 },
    )
    .toBe(1);
});
