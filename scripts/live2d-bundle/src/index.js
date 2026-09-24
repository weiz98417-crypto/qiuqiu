// Entry for the rebuilt live2d display bundle.
// Exposes the same globals the hand-written bundle used to provide, so
// live2d.html keeps consuming window.PIXI / window.Live2DModel unchanged.
// pixi.js provides the full core API; importing pixi.js-legacy for its side
// effects registers the Canvas 2D renderer plugins, which is what lets the
// model come up on WebGL-less webviews (emulator). Do NOT alias pixi.js to
// pixi.js-legacy here: the alias is circular (legacy re-exports pixi.js) and
// silently drops every core export.
import * as PIXI from 'pixi.js';
import 'pixi.js-legacy';
import { Live2DModel } from 'pixi-live2d-display-lipsyncpatch/cubism4';

window.PIXI = PIXI;
window.Live2DModel = Live2DModel;
