// Entry for the rebuilt live2d display bundle.
// Exposes the same globals the hand-written bundle used to provide, so
// live2d.html keeps consuming window.PIXI / window.Live2DModel unchanged.
// pixi.js provides the full core API; importing pixi.js-legacy for its side
// effects registers the Canvas 2D renderer plugins, which is what lets the
// model come up on WebGL-less webviews (emulator). Do NOT alias pixi.js to
// pixi.js-legacy here: the alias is circular (legacy re-exports pixi.js) and
// silently drops every core export.
//
// Engine base (live2d-engine-swap 6.1): the display library is
// pixi-live2d-display-advanced (active upstream, same PixiJS v7 lineage)
// replacing the 404'd lipsyncpatch fork. The advanced fork's own lipsync
// stays DISABLED — the app drives the mouth through the vendored wLipSync
// bridge (live2d.html / live2d_view.dart); the fork's lipsync only ever
// activates through model.speak() audio, which no qiuqiu surface calls, and
// both surfaces set internalModel.lipSync = false after load to pin that.
import * as PIXI from 'pixi.js';
import 'pixi.js-legacy';
import { Live2DModel } from 'pixi-live2d-display-advanced/cubism4';

window.PIXI = PIXI;
window.Live2DModel = Live2DModel;
