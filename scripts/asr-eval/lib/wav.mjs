// 纯 node WAV 工具(asr-eval 内部用,零依赖,与 scripts/turn-model 先例同风格)。
// 只处理评估链路需要的形状:PCM16 WAV、单声道线性重采样、白噪声混合。

/** Float32(-1..1) → 16bit PCM WAV Buffer。 */
export function encodeWav(floatSamples, sampleRate) {
  const n = floatSamples.length;
  const pcm = new DataView(new ArrayBuffer(44 + n * 2));
  const w = (offset, s) => {
    for (let i = 0; i < s.length; i++) pcm.setUint8(offset + i, s.charCodeAt(i));
  };
  w(0, 'RIFF');
  pcm.setUint32(4, 36 + n * 2, true);
  w(8, 'WAVE');
  w(12, 'fmt ');
  pcm.setUint32(16, 16, true);
  pcm.setUint16(20, 1, true); // PCM
  pcm.setUint16(22, 1, true); // mono
  pcm.setUint32(24, sampleRate, true);
  pcm.setUint32(28, sampleRate * 2, true);
  pcm.setUint16(32, 2, true);
  pcm.setUint16(34, 16, true);
  w(36, 'data');
  pcm.setUint32(40, n * 2, true);
  for (let i = 0; i < n; i++) {
    let v = Math.max(-1, Math.min(1, floatSamples[i]));
    pcm.setInt16(44 + i * 2, v < 0 ? v * 0x8000 : v * 0x7fff, true);
  }
  return Buffer.from(pcm.buffer);
}

/** 解析 PCM16 WAV(支持多余的 LIST/fact chunk);多声道取平均。 */
export function decodeWav(buffer) {
  if (buffer.toString('ascii', 0, 4) !== 'RIFF' || buffer.toString('ascii', 8, 12) !== 'WAVE') {
    throw new Error('not a RIFF/WAVE buffer');
  }
  let offset = 12;
  let sampleRate = 0;
  let channels = 0;
  let bitsPerSample = 0;
  let audioFormat = 0;
  let data = null;
  while (offset + 8 <= buffer.length) {
    const id = buffer.toString('ascii', offset, offset + 4);
    const size = buffer.readUInt32LE(offset + 4);
    const body = offset + 8;
    if (id === 'fmt ') {
      audioFormat = buffer.readUInt16LE(body);
      channels = buffer.readUInt16LE(body + 2);
      sampleRate = buffer.readUInt32LE(body + 4);
      bitsPerSample = buffer.readUInt16LE(body + 14);
    } else if (id === 'data') {
      data = buffer.subarray(body, Math.min(body + size, buffer.length));
    }
    offset = body + size + (size % 2);
  }
  if (audioFormat !== 1 && audioFormat !== 0xfffe) throw new Error(`unsupported wav format ${audioFormat}`);
  if (bitsPerSample !== 16) throw new Error(`unsupported bit depth ${bitsPerSample}`);
  const frames = Math.floor(data.length / 2 / channels);
  const samples = new Float32Array(frames);
  for (let i = 0; i < frames; i++) {
    let acc = 0;
    for (let ch = 0; ch < channels; ch++) acc += data.readInt16LE((i * channels + ch) * 2);
    samples[i] = acc / channels / 0x7fff;
  }
  return { sampleRate, channels, samples };
}

/** 线性插值重采样(评估用足够;报告如实标注)。 */
export function resampleLinear(samples, fromRate, toRate) {
  if (fromRate === toRate) return samples;
  const ratio = fromRate / toRate;
  const outLen = Math.floor(samples.length / ratio);
  const out = new Float32Array(outLen);
  for (let i = 0; i < outLen; i++) {
    const pos = i * ratio;
    const i0 = Math.floor(pos);
    const frac = pos - i0;
    const s0 = samples[i0];
    const s1 = samples[Math.min(i0 + 1, samples.length - 1)];
    out[i] = s0 + (s1 - s0) * frac;
  }
  return out;
}

/** 整体增益。 */
export function scale(samples, factor) {
  const out = new Float32Array(samples.length);
  for (let i = 0; i < samples.length; i++) out[i] = samples[i] * factor;
  return out;
}

/**
 * 混入白噪声,目标信噪比 snrDb(相对语音 RMS)。LCG 伪随机,种子固定保证可重复。
 */
export function mixNoise(samples, snrDb, seed = 42) {
  let state = seed >>> 0 || 1;
  const rand = () => {
    // LCG (Numerical Recipes 参数),输出 -1..1
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return (state / 0x80000000) - 1;
  };
  let energy = 0;
  for (let i = 0; i < samples.length; i++) energy += samples[i] * samples[i];
  const speechRms = Math.sqrt(energy / Math.max(1, samples.length));
  const noiseRms = speechRms / Math.pow(10, snrDb / 20);
  const out = new Float32Array(samples.length);
  for (let i = 0; i < samples.length; i++) {
    out[i] = Math.max(-1, Math.min(1, samples[i] + rand() * noiseRms));
  }
  return out;
}

export function durationMs(samples, sampleRate) {
  return Math.round((samples.length / sampleRate) * 1000);
}
