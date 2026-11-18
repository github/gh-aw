/// <reference path="./runtime.d.ts" />
// Cracktro deck runtime: a reusable, content-free engine for full-screen slide decks.
// Classic script (no build, works from file://); load it before the deck script that calls CracktroDeck.create(config).
// It owns the stage, band headers, dock chrome, navigation, URL hash, search, slide map and animated background.
(() => {
  // ---------- helpers ----------
  const h = (tag, attrs = {}, ...kids) => {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
      if (k === "style" && typeof v === "object") Object.assign(el.style, v);
      else if (k.startsWith("--")) el.style.setProperty(k, v);
      else if (k in el && k !== "class") el[k] = v;
      else el.setAttribute(k, v);
    }
    el.append(...kids.flat().filter(k => k != null));
    return el;
  };
  // Split text into per-character spans so each letter can move on its own.
  let charIndex = 0;
  const letters = (text, cls = "") =>
    h(
      "span",
      { class: `line ${cls}` },
      [...text].map((c, j) => (c === " " ? " " : h("span", { class: "ch", "--i": charIndex++, "--bx": `${-j * 120}px` }, c)))
    );
  const resetIndex = () => (charIndex = 0);

  // Fallbacks for decks that omit a theme, band label or sound service.
  const DEFAULT_THEME = { base: [13, 17, 23], pal: ["#0d419d", "#5a32a3", "#196c2e", "#0a8a2e"], grid: [240, 246, 252, 0.028, 0.07], ang: 0, cell: 32, accent: ["#79c0ff", "#4493f8", "#cae8ff", "#0d419d"], prog: [0] };
  /** @type {CracktroDeck.Sound} */
  const SILENT = { cut() {}, techno() {}, slide() {}, touch() {}, step() {}, toggle: () => true, muted: true };

  // ---------- runtime styles ----------
  const runtimeCss = (W, H) => `
  /* safe-area insets (notch, home indicator) are 0 unless the page is drawn edge to edge (viewport-fit=cover) */
  :root { --mono: ui-monospace, Menlo, monospace; --sb: env(safe-area-inset-bottom, 0px); --sl: env(safe-area-inset-left, 0px); --sr: env(safe-area-inset-right, 0px); }
  *, *::before, *::after { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; overflow: hidden; overscroll-behavior: none; background: #0d1117; color: #f0f6fc;
    font-family: -apple-system, "Segoe UI", sans-serif; -webkit-font-smoothing: antialiased; }
  canvas.bg { position: fixed; inset: 0; width: 100vw; height: 100vh; }
  .stage { position: absolute; left: 50%; top: 50%; width: ${W}px; height: ${H}px; transform-origin: 0 0; }
  .slide { position: absolute; inset: 0; visibility: hidden; pointer-events: none; }
  /* siblings in a group: reveal-style horizontal push; --dx is +100% going forward, -100% going back, 0 on first load.
     crossing groups: vertical push, up going forward and down going back (same duration and easing) */
  .slide.active { visibility: visible; pointer-events: auto; animation: slide-in .8s cubic-bezier(.26,.86,.44,.985) both; }
  .slide.leaving { visibility: visible; animation: slide-out .8s cubic-bezier(.26,.86,.44,.985) both; }
  /* hidden slides stay in the DOM: pause their (often infinite) animations so off-screen loops cost no frames until shown */
  .slide:not(.active):not(.leaving) *, .slide:not(.active):not(.leaving) *::before, .slide:not(.active):not(.leaving) *::after { animation-play-state: paused !important; }
  @keyframes slide-in { from { transform: translateX(var(--dx, 100%)); } to { transform: none; } }
  @keyframes slide-out { to { transform: translateX(calc(-1 * var(--dx, 100%))); } }
  .slide.active.up-in { animation: up-in .8s cubic-bezier(.26,.86,.44,.985) both; }
  .slide.leaving.up-out { animation: up-out .8s cubic-bezier(.26,.86,.44,.985) both; }
  .slide.active.down-in { animation: down-in .8s cubic-bezier(.26,.86,.44,.985) both; }
  .slide.fadeout { opacity: 0; transition: opacity .6s ease-in-out; }
  .slide.leaving.down-out { animation: down-out .8s cubic-bezier(.26,.86,.44,.985) both; }
  /* full-screen red wash for bands whose theme sets red: fades over the previous slide, then sits behind the stage */
  .wash { position: fixed; inset: 0; pointer-events: none; opacity: 0; transition: opacity .9s ease-in-out;
    background: radial-gradient(ellipse 80% 75% at 50% 55%, #4a070c 0%, #270306 55%, #0c0102 100%); }
  /* blueprint grid on its own layer above the wash, so it stays visible (and turns with the band theme) over the red */
  canvas.gridc { position: fixed; inset: 0; width: 100vw; height: 100vh; pointer-events: none; }
  .wash.cover + canvas.gridc { z-index: 4; }
  .wash.on { opacity: 1; }
  .wash::after { content: ""; position: absolute; inset: 0;
    background: radial-gradient(ellipse 55% 60% at 50% 58%, transparent 30%, #1c0204b0 70%, #050000f0 100%); }
  .wash.on::after { animation: omen 7s ease-in-out infinite; }
  .wash.cover { z-index: 4; }
  @keyframes up-in { from { transform: translateY(100%); } to { transform: none; } }
  @keyframes up-out { to { transform: translateY(-100%); } }
  @keyframes down-in { from { transform: translateY(-100%); } to { transform: none; } }
  @keyframes down-out { to { transform: translateY(100%); } }
  .kicker { font: 500 24px/1 var(--mono); letter-spacing: .12em; text-transform: uppercase; color: #9198a1; font-stretch: 100%; }
  .active .kicker { animation: rise .9s cubic-bezier(.2,.9,.2,1) both .1s; }
  .ch { display: inline-block; font-variation-settings: "wght" 700, "wdth" 100; }
  .l-grad .ch { color: transparent; -webkit-background-clip: text; background-clip: text;
    background-image: linear-gradient(90deg, #f0f6fc, var(--lg, #f0f6fc), #f0f6fc, var(--lg, #f0f6fc), #f0f6fc);
    background-size: 1800px 100%; background-position: var(--bx) 0;
    padding-bottom: .14em; margin-bottom: -.14em; }
  @keyframes settle { from { opacity: 0; transform: translateY(.25em); filter: blur(4px); } to { opacity: 1; transform: none; filter: none; } }
  @keyframes breathe { 0%, 100% { font-variation-settings: "wght" 700, "wdth" 100; } 50% { font-variation-settings: "wght" 700, "wdth" 100; } }
  .l-out .ch { color: transparent; -webkit-text-stroke: 3px #f0f6fc; }
  @keyframes rise { from { opacity: 0; transform: translateY(.3em) scale(.94); filter: blur(6px); } to { opacity: 1; transform: none; filter: none; } }
  @keyframes flex { 0%, 100% { font-variation-settings: "wght" 700, "wdth" 100; } 50% { font-variation-settings: "wght" 700, "wdth" 100; } }
  @keyframes hue { from { background-position: var(--bx) 0; } to { background-position: calc(var(--bx) - 1800px) 0; } }
  body.light .kicker { color: #424a53; }
  body.light .dots button { background: #1f232833; }
  body.light .dots button.hd { background: transparent; box-shadow: inset 0 0 0 2px #1f232859; }
  body.light .dots button.on { background: #1f2328; box-shadow: none; }
  body.light .snd, body.light .fzb, body.light .smb, body.light .ntb, body.light .ksb, body.light .arrows button { border-color: #9ea7b3; background: #e6eaefcc; color: #424a53; }
  body.light .snd:hover, body.light .fzb:hover, body.light .smb:hover, body.light .ntb:hover, body.light .ksb:hover, body.light .arrows button:hover { color: #1f2328; border-color: #818b98; }
  /* research prototype badge on every gh cao slide (top right, clear of the chrome) */
  .proto { position: absolute; right: 110px; top: 76px; display: flex; align-items: center; gap: 14px; padding: 12px 22px; border: 2px dashed #e3b341; border-radius: 999px; background: #0d1117cc; font: 600 20px/1 var(--mono); letter-spacing: .12em; text-transform: uppercase; color: #e3b341; z-index: 5; }
  .proto i { width: 12px; height: 12px; border-radius: 50%; background: #e3b341; box-shadow: 0 0 12px #e3b341; animation: proto-blink 1.6s ease-in-out infinite; }
  .proto span { color: #56d4dd; letter-spacing: .04em; text-transform: none; padding-left: 14px; border-left: 1px solid #3d444d; }
  @keyframes proto-blink { 50% { opacity: .25; } }
  .bh .proto { top: 96px; right: 140px; }
  /* experimental badge on every slide that sets experimental (same spot as .proto) */
  .xbadge { position: absolute; right: 110px; top: 76px; display: flex; align-items: center; gap: 12px; padding: 12px 22px; border: 2px dashed #f0883e; border-radius: 999px; background: #0d1117cc; font: 600 18px/1 var(--mono); letter-spacing: .12em; text-transform: uppercase; color: #f0883e; z-index: 5; }
  .xbadge span { color: #9198a1; letter-spacing: .04em; text-transform: none; padding-left: 12px; border-left: 1px solid #3d444d; }
  /* --- chrome --- */
  .progress { position: fixed; left: 0; bottom: 0; height: 5px; width: 100%; transform-origin: 0 50%;
    background: #3fb950; transition: transform .8s cubic-bezier(.7,0,.2,1); z-index: 5; }
  /* dot map: one row per band, header dot outlined; the band you are in is bright, others dim until hovered */
  .dots { position: fixed; left: calc(28px + var(--sl)); bottom: calc(22px + var(--sb)); display: flex; flex-direction: column; gap: 5px; z-index: 5; opacity: .25; transition: opacity .3s; }
  .dots:hover, .dots:focus-within { opacity: 1; }
  .dots .drow { display: flex; gap: 5px; opacity: .45; transition: opacity .3s; }
  .dots .drow.on, .dots:hover .drow { opacity: 1; }
  .dots button { width: 8px; height: 8px; padding: 0; border: 0; border-radius: 999px; background: #ffffff40; cursor: pointer;
    transition: width .5s cubic-bezier(.3,1.6,.4,1), background .3s; }
  .dots button.hd { background: transparent; box-shadow: inset 0 0 0 2px #ffffff60; }
  .dots button.on { width: 24px; background: #f0f6fc; box-shadow: none; }
  .dots button.col { box-shadow: 0 3px 0 -1px #ffffff80; }
  .dots button.sub { width: 4px; height: 6px; margin: 1px -1px 0; border-radius: 2px; background: #ffffff26; }
  .dots button.sub.on { width: 12px; background: #f0f6fc; }
  /* --- example column button (dock): only on a column's head and its examples --- */
  .vnb { position: fixed; right: calc(224px + var(--sr)); bottom: calc(10px + var(--sb)); height: 34px; padding: 0 10px 0 6px; display: none; align-items: center; gap: 4px; border: 1px solid #3d444d; border-radius: 8px;
    background: #151b23cc; color: #9198a1; font: 600 13px/1 var(--mono); cursor: pointer; z-index: 5; transition: color .2s, border-color .2s, background .2s; touch-action: manipulation; }
  .vnb.on { display: flex; }
  .vnb:hover { color: #f0f6fc; border-color: #9198a1; }
  .vnb svg { width: 20px; height: 20px; display: block; flex: none; }
  .vnb.head { color: #0d1117; background: #f0f6fc; border-color: #f0f6fc; }
  .vnb.head svg { animation: vnudge 1.6s ease-in-out infinite; }
  @keyframes vnudge { 0%, 100% { transform: translateY(-2px); } 50% { transform: translateY(3px); } }
  @media (prefers-reduced-motion: reduce) { .vnb.head svg { animation: none; } }
  body.light .vnb { border-color: #9ea7b3; background: #e6eaefcc; color: #424a53; }
  body.light .vnb:hover { color: #1f2328; border-color: #818b98; }
  body.light .vnb.head { color: #ffffff; background: #1f2328; border-color: #1f2328; }
  /* --- band header --- */
  .bh-head { position: absolute; left: 140px; top: 170px; width: 960px; }
  .bh-head h2 { margin: 26px 0 0; font-size: 150px; line-height: .95; }
  .bh-head h2 .line { display: inline-block; white-space: nowrap; }
  .bh .l-grad .ch { background-image: linear-gradient(90deg, var(--bh), var(--bh2), #f0f6fc, var(--bh2), var(--bh)); }
  .active.bh .ch { animation: settle .7s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 30ms + .15s); }
  .bh-sub { margin: 34px 0 0; font-size: 40px; line-height: 1.25; color: #c9d1d9; font-variation-settings: "wght" 500, "wdth" 100; }
  .active .bh-sub { animation: rise .9s cubic-bezier(.2,.9,.2,1) both .45s; }
  .bh-map { position: absolute; right: 140px; top: 182px; display: flex; flex-direction: column; gap: 9px; }
  .bh-mrow { display: flex; align-items: center; gap: 5px; opacity: .4; }
  .bh-mrow.on { opacity: 1; }
  .active .bh-mrow { animation: rise .7s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 45ms + .2s); }
  .active .bh-mrow:not(.on) { animation-name: dimrise; }
  @keyframes dimrise { from { opacity: 0; transform: translateY(.3em); } to { opacity: .4; transform: none; } }
  .bh-ml { width: 230px; font: 500 15px/1 var(--mono); letter-spacing: .08em; text-transform: uppercase; color: #9198a1; text-align: right; margin-right: 14px; }
  .bh-mrow.on .bh-ml { color: #f0f6fc; }
  .bh-mrow i { width: 16px; height: 12px; border-radius: 3px; background: #ffffff30; }
  .bh-mrow i.hd { background: transparent; box-shadow: inset 0 0 0 2px #ffffff60; }
  .bh-mrow.on i { background: var(--bh); box-shadow: 0 0 14px var(--bh); }
  .bh-mrow.on i.hd { background: transparent; box-shadow: inset 0 0 0 2px var(--bh2); }
  .bh-row { position: absolute; left: 140px; right: 140px; bottom: 190px; margin: 0; padding: 0; list-style: none; display: flex; gap: 22px; }
  .bh-row li { cursor: pointer; flex: 1; min-width: 0; height: 170px; padding: 26px 28px; border: 1px solid #3d444d; border-top: 4px solid var(--bh); border-radius: 14px;
    background: #151b23cc; display: flex; flex-direction: column; justify-content: space-between; }
  .bh-row li:hover { border-color: var(--bh2); background: #1f2630cc; }
  .active .bh-row li { animation: rowin .8s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 90ms + .5s); }
  @keyframes rowin { from { opacity: 0; transform: translateX(120px); } to { opacity: 1; transform: none; } }
  .bh-n { font: 600 22px/1 var(--mono); color: var(--bh2); letter-spacing: .1em; display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .bh-k { font-size: .8em; letter-spacing: .02em; padding: 3px 8px; border-radius: 999px; color: #0d1117; background: var(--bh2); }
  .bh-row.many { display: grid; grid-template-columns: repeat(7, 1fr); bottom: 140px; }
  .bh-row.many.lots { grid-template-columns: repeat(10, 1fr); gap: 14px; }
  .bh-row.many.lots li { padding: 16px 14px; }
  .bh-row.many.lots .bh-t { font-size: 21px; }
  .bh-row.many li { height: 150px; padding: 18px 22px; }
  .bh-row.many .bh-t { font-size: 25px; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; overflow: hidden; }
  .bh-t { font-size: 28px; line-height: 1.15; font-variation-settings: "wght" 600, "wdth" 100; }
  .arrows { position: fixed; left: calc(12px + var(--sl)); bottom: calc(10px + var(--sb)); display: none; gap: 6px; z-index: 5; touch-action: manipulation; -webkit-user-select: none; user-select: none; }
  .arrows button { width: 34px; height: 34px; padding: 7px; border: 1px solid #3d444d; border-radius: 8px; background: #151b23cc;
    color: #9198a1; cursor: pointer; transition: color .2s, border-color .2s; touch-action: manipulation; -webkit-tap-highlight-color: transparent; }
  .arrows button:hover { color: #f0f6fc; border-color: #9198a1; }
  .arrows button:disabled { opacity: .35; cursor: default; }
  .arrows svg { width: 100%; height: 100%; display: block; }
  body.compact-nav .dots { display: none; }
  body.compact-nav .arrows { display: flex; }
  /* compact dock: translucent after a few idle seconds (body.nav-idle); any touch, key or focus restores it.
     Idle fading preserves hit targets; keyboard focus keeps its control fully readable. */
  body.compact-nav :is(.arrows, .vnb, .snd, .fzb, .smb, .ntb, .ksb, .tourp) { transition: opacity .4s, color .2s, border-color .2s, background .2s,
    transform .45s cubic-bezier(.2,.9,.2,1), bottom .45s cubic-bezier(.2,.9,.2,1); }
  body.compact-nav.nav-idle :is(.arrows, .vnb, .snd, .fzb, .smb, .ntb, .ksb, .tourp) { opacity: .4; }
  body.compact-nav.nav-idle :is(.arrows:has(:focus-visible), .vnb:focus-visible, .snd:focus-visible, .fzb:focus-visible, .smb:focus-visible, .ntb:focus-visible, .ksb:focus-visible, .tourp:has(:focus-visible)) { opacity: 1; }
  @media (prefers-reduced-motion: reduce) { body.compact-nav :is(.arrows, .vnb, .snd, .fzb, .smb, .ntb, .ksb, .tourp) { transition: none; } }
  .snd, .fzb, .smb, .ntb, .ksb { position: fixed; right: calc(24px + var(--sr)); bottom: calc(10px + var(--sb)); width: 34px; height: 34px; padding: 6px; border: 1px solid #3d444d; border-radius: 8px;
    background: #151b23cc; color: #9198a1; cursor: pointer; z-index: 5; transition: color .2s, border-color .2s, background .2s; }
  .snd:hover, .fzb:hover, .smb:hover, .ntb:hover, .ksb:hover { color: #f0f6fc; border-color: #9198a1; }
  .snd svg, .fzb svg, .smb svg, .ntb svg, .ksb svg { width: 100%; height: 100%; display: block; }
  .fzb { right: calc(64px + var(--sr)); }
  .smb { right: calc(104px + var(--sr)); }
  .ntb { right: calc(144px + var(--sr)); }
  .ksb { right: calc(184px + var(--sr)); }
  .ntb[aria-pressed="true"] { color: #f0f6fc; border-color: currentColor; box-shadow: inset 0 -3px currentColor; }
  body.light .ntb[aria-pressed="true"] { color: #1f2328; }
  /* kiosk mode: slides rotate on their own; the chrome hides except the stop button and the sound toggle beside it,
     which fade (with the cursor) when idle */
  body.kiosk :is(.dots, .arrows, .vnb, .fzb, .smb, .ntb, .tourp, .notes, .progress) { display: none !important; }
  body.kiosk :is(.ksb, .snd) { opacity: 1; color: #f0f6fc; border-color: #f0f6fc; transition: opacity .4s, color .2s, border-color .2s; }
  body.kiosk .snd { right: calc(144px + var(--sr)); }
  body.kiosk.nav-idle { cursor: none; }
  body.kiosk.nav-idle :is(.ksb, .snd):not(:focus-visible) { opacity: 0; }
  body.light.kiosk :is(.ksb, .snd) { color: #1f2328; border-color: #1f2328; }
  .snd .waves { transition: opacity .2s; }
  .snd .x { opacity: 0; transition: opacity .2s; }
  .snd.off .waves { opacity: 0; }
  .snd.off .x { opacity: 1; }
  .snd.flash { color: #f0f6fc; border-color: #f0f6fc; }
  .notes { position: fixed; left: 24px; right: 24px; bottom: calc(50px + var(--sb)); max-width: 900px; margin: 0 auto; padding: 18px 22px;
    background: #151b23f0; border: 1px solid #3d444d; border-radius: 12px; color: #f0f6fc; font: 400 18px/1.55 "Mona Sans", -apple-system, "Segoe UI", sans-serif; z-index: 6; display: none;
    white-space: pre-line; overflow-wrap: anywhere; max-height: calc(100dvh - 140px); overflow-y: auto; overscroll-behavior: contain; }
  .notes.on { display: block; }
  body:not(.compact-nav) .notes { left: 330px; }
  /* Labelled cues ("What: ...") get a label column and a divider so each reads as its own block. */
  .notes .cue { display: grid; grid-template-columns: 5.5em 1fr; align-items: baseline; gap: 4px 16px; margin: 0; }
  .notes .cue + .cue { margin-top: 14px; padding-top: 14px; border-top: 1px solid #3d444d; }
  .notes .cue-k { font: 600 13px/1.4 var(--mono); letter-spacing: .08em; text-transform: uppercase; color: #79c0ff; white-space: nowrap; }
  @media (max-width: 640px) {
    .snd, .fzb, .smb, .ntb, .ksb, .vnb { bottom: calc(52px + var(--sb)); }
    /* Translate rather than switch anchors so the arrows can slide beneath the toolbar. */
    .arrows { transform: translateX(calc(100vw - 100% - 36px - var(--sl) - var(--sr))); }
    .notes { bottom: calc(94px + var(--sb)); padding: 14px 16px; font-size: 16px; line-height: 1.5; }
    .notes .cue { grid-template-columns: 1fr; }
    .notes .cue + .cue { margin-top: 12px; padding-top: 12px; }
  }

  /* --- slide search (full-screen dialog) --- */
  .fz { position: fixed; inset: 0; width: 100%; height: 100%; max-width: none; max-height: none; margin: 0; padding: 0; border: 0;
    background: #0d1117f7; -webkit-backdrop-filter: blur(8px); backdrop-filter: blur(8px); color: #f0f6fc; font-family: "Mona Sans", -apple-system, "Segoe UI", sans-serif; z-index: 20; }
  .fz[open] { display: flex; justify-content: center; }
  .fz::backdrop { background: transparent; }
  .fz-box { width: min(980px, calc(100% - 32px)); margin-top: max(16px, 7vh); max-height: calc(100% - max(32px, 14vh)); display: flex; flex-direction: column; gap: 12px; }
  .fz-bar { display: flex; gap: 10px; align-items: center; }
  .fz-in { flex: 1; min-width: 0; font: 500 clamp(20px, 3vw, 30px)/1.2 "Mona Sans", sans-serif; color: inherit; background: #151b23; border: 1px solid #3d444d;
    border-radius: 12px; padding: 14px 18px; outline: none; }
  .fz-in:focus { border-color: #ab7df8; box-shadow: 0 0 0 3px #ab7df840; }
  .fz-x { font: 600 14px/1 var(--mono); color: #9198a1; background: #151b23; border: 1px solid #3d444d; border-radius: 8px; padding: 10px 12px; cursor: pointer; }
  .fz-x:hover { color: #f0f6fc; border-color: #9198a1; }
  .fz-st { font: 500 14px/1 var(--mono); color: #9198a1; letter-spacing: .06em; }
  .fz-list { list-style: none; margin: 0; padding: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; }
  .fz-list li { display: grid; grid-template-columns: 3.2em 1fr; gap: 4px 12px; padding: 12px 16px; border: 1px solid transparent; border-radius: 10px; cursor: pointer; }
  .fz-list li:hover { background: #151b23; }
  .fz-list li[aria-selected="true"] { background: #1f2630; border-color: #ab7df8; }
  .fz-n { grid-row: span 2; font: 600 18px/1.4 var(--mono); color: #ab7df8; letter-spacing: .06em; }
  .fz-t { font-size: 20px; font-weight: 600; line-height: 1.3; }
  .fz-b { font: 500 13px/1 var(--mono); color: #9198a1; margin-left: 10px; letter-spacing: .04em; }
  .fz-k { font: 500 13px/1 var(--mono); color: #9198a1; margin-left: 8px; padding: 3px 6px; border: 1px solid #3d444d; border-radius: 6px; letter-spacing: .02em; white-space: nowrap; }
  .fz-s { font-size: 15px; line-height: 1.4; color: #c9d1d9; overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
  .fz mark { background: #ab7df859; color: inherit; border-radius: 3px; }
  body.light .fz { background: #f6f8faf5; color: #1f2328; }
  body.light .fz-in, body.light .fz-x { background: #ffffff; border-color: #9ea7b3; color: #1f2328; }
  body.light .fz-list li:hover { background: #eaeef2; }
  body.light .fz-list li[aria-selected="true"] { background: #ffffff; border-color: #8250df; }
  body.light .fz-n { color: #8250df; }
  body.light .fz-s { color: #424a53; }
  body.light .fz-st, body.light .fz-b, body.light .fz-k { color: #59636e; }
  body.light .fz-k { border-color: #d1d9e0; }
  body.light .fz mark { background: #8250df33; }

  /* --- slide map (full-screen overview: a header per band above a grid with one tile per slide) --- */
  .smp { position: fixed; inset: 0; width: 100%; height: 100%; max-width: none; max-height: none; margin: 0; padding: 0; border: 0;
    background: #0d1117f7; -webkit-backdrop-filter: blur(8px); backdrop-filter: blur(8px); color: #f0f6fc; font-family: "Mona Sans", -apple-system, "Segoe UI", sans-serif; z-index: 20; }
  .smp[open] { display: flex; flex-direction: column; }
  .smp::backdrop { background: transparent; }
  .smp-bar { display: flex; align-items: center; gap: 12px; padding: 16px 24px 8px; }
  .smp-h { flex: 1; margin: 0; font: 600 14px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: #9198a1; }
  .smp-body { flex: 1; min-height: 0; overflow-y: auto; padding: 8px 24px 24px; display: flex; flex-direction: column; gap: 18px; }
  .smp-row { display: flex; flex-direction: column; gap: 8px; }
  .smp-l { display: flex; align-items: baseline; gap: 10px; margin: 0; padding-bottom: 6px; border-bottom: 1px solid color-mix(in srgb, var(--c) 45%, transparent);
    font: 600 13px/1.3 var(--mono); letter-spacing: .06em; text-transform: uppercase; color: var(--c); }
  .smp-l small { font: 500 11px/1 var(--mono); letter-spacing: .04em; text-transform: none; color: #9198a1; }
  /* tiles fill the width: as many 150px+ columns as fit; a pattern's block reuses the same tracks (subgrid) so its examples align */
  .smp-tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 6px; align-items: stretch; }
  .smp-col { grid-column: var(--gc, 1 / -1); display: grid; grid-template-columns: subgrid; gap: 6px; align-items: start; }
  .smp-kids { grid-column: 2 / -1; display: grid; grid-template-columns: subgrid; gap: 6px; align-items: stretch; padding-left: 8px; border-left: 2px dotted var(--c); }
  .smp-t { min-width: 0; min-height: 58px; display: flex; flex-direction: column; gap: 4px; text-align: left; padding: 8px 10px; border: 1px solid #3d444d;
    border-left: 3px solid var(--c); border-radius: 8px; background: #151b23; color: inherit; cursor: pointer; font: 500 13px/1.3 "Mona Sans", sans-serif; }
  .smp-t b { display: flex; font: 600 11px/1 var(--mono); color: #9198a1; letter-spacing: .06em; }
  .smp-t b i { margin-left: auto; font-style: normal; color: var(--c); }
  .smp-t span { overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
  .smp-t.hd { background: transparent; border-style: dashed; border-left-style: solid; }
  .smp-t.sub { min-height: 44px; font-size: 12px; border-left-style: dotted; opacity: .85; }
  .smp-t:hover { background: #1f2630; border-color: #9198a1; border-left-color: var(--c); }
  .smp-t.on { box-shadow: 0 0 0 2px var(--c); }
  .smp-t:focus-visible { outline: 3px solid #ab7df8; outline-offset: 2px; }
  /* staggered entrance: the dialog is display:none while closed, so every open replays it; --d is set per tile, header and rule on open */
  @keyframes smp-in { from { opacity: 0; transform: translateY(16px) scale(.94); } }
  @keyframes smp-hd { from { opacity: 0; transform: translateX(-24px); } }
  @keyframes smp-fade { from { border-left-color: transparent; } }
  .smp[open] .smp-t { animation: smp-in .42s cubic-bezier(.2, .8, .2, 1) var(--d, 0ms) backwards; }
  .smp[open] .smp-l { animation: smp-hd .42s cubic-bezier(.2, .8, .2, 1) var(--d, 0ms) backwards; }
  .smp[open] .smp-kids { animation: smp-fade .42s ease-out var(--d, 0ms) backwards; }
  @media (prefers-reduced-motion: reduce) { .smp[open] .smp-t, .smp[open] .smp-l, .smp[open] .smp-kids { animation: none; } }
  @media (max-width: 640px) { .smp-tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } .smp-kids { grid-column: 1 / -1; } }
  body.light .smp { background: #f6f8faf5; color: #1f2328; }
  body.light .smp-t { background: #ffffff; border-color: #9ea7b3; border-left-color: var(--c); }
  body.light .smp-t.hd { background: transparent; }
  body.light .smp-t:hover { background: #eaeef2; }
  body.light .smp-h, body.light .smp-t b, body.light .smp-l small { color: #59636e; }
  body.light .smp-l { color: #1f2328; }
  /* tours: dimmed tiles are outside the current tour; the tours list sits at the very bottom of the map */
  .smp-t.out { opacity: .35; }
  .smp-tl { display: flex; flex-wrap: wrap; gap: 8px; }
  .smp-tb { display: flex; flex-direction: column; gap: 5px; min-width: 180px; max-width: 340px; text-align: left; padding: 10px 12px; border: 1px solid #3d444d;
    border-left: 3px solid var(--c); border-radius: 8px; background: #151b23; color: inherit; cursor: pointer; font: 500 13px/1.3 "Mona Sans", sans-serif; }
  .smp-tb b { font-weight: 600; font-size: 14px; }
  .smp-tb span { font: 500 11px/1.35 var(--mono); color: #9198a1; }
  .smp-tb:hover { background: #1f2630; border-color: #9198a1; border-left-color: var(--c); }
  .smp-tb[aria-pressed="true"] { box-shadow: 0 0 0 2px var(--c); }
  .smp-tb.design { border-style: dashed; border-left-style: solid; border-left-color: #db6d28; background: transparent; }
  .smp-tb:focus-visible, .smp-db:focus-visible, .smp-d input:focus-visible { outline: 3px solid #ab7df8; outline-offset: 2px; }
  /* design mode: tiles toggle (a check box drawn before the number), band headings get a whole-band checkbox, the bar below holds the link */
  .smp-bx { display: none; width: 16px; height: 16px; margin: 0; align-self: center; accent-color: #4493f8; cursor: pointer; }
  .smp.design .smp-bx { display: block; }
  .smp.design .smp-t { position: relative; padding-left: 30px; }
  .smp.design .smp-t::before { content: ""; position: absolute; left: 9px; top: 8px; width: 13px; height: 13px; border: 1.5px solid #9198a1; border-radius: 3px;
    font: 700 11px/13px var(--mono); text-align: center; color: #0d1117; }
  .smp.design .smp-t[aria-pressed="true"] { border-color: #4493f8; border-left-color: var(--c); background: #4493f81f; }
  .smp.design .smp-t[aria-pressed="true"]::before { content: "✓"; background: #4493f8; border-color: #4493f8; }
  .smp.design .smp-t[aria-pressed="false"] { opacity: .5; }
  /* an example whose pattern is not picked cannot be picked: dashed and faint, with a struck box */
  .smp.design .smp-t.off { opacity: .25; border-style: dashed; cursor: not-allowed; }
  .smp.design .smp-t.off::before { content: ""; background: linear-gradient(to top right, transparent 45%, #9198a1 45% 55%, transparent 55%); }
  .smp-d { display: none; flex-wrap: wrap; align-items: center; gap: 8px; padding: 12px 24px calc(14px + var(--sb)); border-top: 1px solid #3d444d; background: #0d1117; }
  .smp.design .smp-d { display: flex; }
  .smp-dc { font: 600 13px/1 var(--mono); color: #f0f6fc; margin-right: 6px; }
  .smp-db, .smp-d input { font: 600 13px/1 var(--mono); color: #c9d1d9; background: #151b23; border: 1px solid #3d444d; border-radius: 8px; padding: 9px 12px; }
  .smp-db { cursor: pointer; }
  .smp-db:hover:not(:disabled) { color: #f0f6fc; border-color: #9198a1; }
  .smp-db:disabled { opacity: .4; cursor: default; }
  .smp-db.go { color: #0d1117; background: #4493f8; border-color: #4493f8; }
  .smp-dt { width: 200px; }
  .smp-dl { flex: 1; min-width: 200px; font-weight: 500 !important; color: #9198a1 !important; }
  body.light .smp-tb { background: #ffffff; border-color: #9ea7b3; border-left-color: var(--c); }
  body.light .smp-tb.design { background: transparent; border-left-color: #bc4c00; }
  body.light .smp-tb span { color: #59636e; }
  body.light .smp-d { background: #f6f8fa; border-color: #9ea7b3; }
  body.light .smp-dc { color: #1f2328; }
  body.light .smp-db, body.light .smp-d input { background: #ffffff; border-color: #9ea7b3; color: #1f2328; }
  body.light .smp-db.go { background: #0969da; border-color: #0969da; color: #ffffff; }
  body.light .smp.design .smp-t[aria-pressed="true"] { border-color: #0969da; background: #0969da14; }
  body.light .smp.design .smp-t[aria-pressed="true"]::before { background: #0969da; border-color: #0969da; color: #ffffff; }
  /* during a tour the dots and band-header tiles keep only its slides; the pill (top right) names the tour */
  .dots .drow.out, .dots button.out { display: none; }
  .bh-row li.out { opacity: .3; }
  .tourp { position: fixed; top: 12px; right: calc(24px + var(--sr)); display: none; z-index: 5; font: 600 13px/1 var(--mono); }
  body.touring .tourp { display: flex; }
  .tourp button { border: 1px solid #3d444d; background: #151b23cc; color: #9198a1; cursor: pointer; font: inherit; transition: color .2s, border-color .2s; }
  .tourp button:hover { color: #f0f6fc; border-color: #9198a1; }
  .tourp-b { display: flex; align-items: center; gap: 10px; min-width: 0; max-width: min(60vw, 560px); padding: 9px 12px; border-radius: 8px 0 0 8px; }
  .tourp-b i { font-style: normal; font-size: 11px; letter-spacing: .12em; text-transform: uppercase; color: #4493f8; }
  .tourp-t { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #f0f6fc; font-family: "Mona Sans", sans-serif; font-size: 14px; }
  .tourp-x { padding: 0 12px; border-left: 0 !important; border-radius: 0 8px 8px 0; font-size: 18px !important; }
  body.light .tourp button { border-color: #9ea7b3; background: #e6eaefcc; color: #424a53; }
  body.light .tourp-t { color: #1f2328; }
  body.light .tourp-b i { color: #0969da; }
  @media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation-duration: .001s !important; animation-iteration-count: 1 !important; } }
  `;

  // ---------- create ----------
  // config: { slides (array, or factory receiving services), themes, bands, solo, prototypes, tours, last, sound, css, width, height }.
  // Returns a controller: { slides, rows, go, next, prev, band, current }.
  /** @param {CracktroDeck.Config} config */
  function create(config = {}) {
    const { width: W = 1920, height: H = 1080, css = "", bands: BANDS = {}, solo: SOLO = {}, prototypes: PROTO = {}, tours: TOURS = {}, last = null } = config;
    const sfx = config.sound || SILENT;
    const services = { h, letters, resetIndex, sound: sfx, current: () => slides[cur] };
    const slides = typeof config.slides === "function" ? config.slides(services) : [...(config.slides || [])];
    if (!slides.length) throw new Error("CracktroDeck.create: no slides");
    const THEME = {};
    for (const s of slides) THEME[s.group] ??= { ...DEFAULT_THEME, ...config.themes?.[s.group] };
    // A row's display name: its band title, its solo label, or the raw group id.
    const rowName = g => BANDS[g]?.[0] || SOLO[g] || g;
    document.head.append(Object.assign(document.createElement("style"), { textContent: runtimeCss(W, H) }));
    if (css) document.head.append(Object.assign(document.createElement("style"), { textContent: css }));
    const canvas = h("canvas", { class: "bg" });

    // Rows follow each group's first appearance, so a slide joins its band wherever its code sits.
    /** @type {CracktroDeck.Row[]} */
    const rows = [];
    for (const s of slides) {
      const row = rows.find(r => r.group === s.group);
      if (row) row.items.push(s);
      else {
        /** @type {any} */
        const newRow = { group: s.group, items: [s] };
        rows.push(newRow);
      }
    }
    // config.last names the group that closes the deck (e.g. a finale), wherever its slides sit.
    const lastAt = last == null ? -1 : rows.findIndex(r => r.group === last);
    if (lastAt >= 0) rows.push(...rows.splice(lastAt, 1));
    // Columns (third level): a slide with `key` heads a vertical column of example slides that set `under: key`
    // (same group). Examples are numbered right after their parent; Up/Down walk the column, Left/Right the band.
    for (const r of rows) {
      const kids = r.items.filter(s => s.under);
      if (!kids.length) continue;
      r.items = r.items.flatMap(s => {
        if (s.under) return [];
        if (!s.key) return [s];
        const ks = kids.filter(k => k.under === s.key);
        if (!ks.length) return [s];
        s.kids = ks;
        ks.forEach(k => (k.parent = s));
        return [s, ...ks];
      });
      const lost = kids.find(k => !k.parent);
      if (lost) throw new Error(`slide under "${lost.under}" has no parent with that key in group "${r.group}"`);
    }
    // the column a slide belongs to (its head), or null
    const colOf = s => s.parent || (s.kids?.length ? s : null);
    // A slide's heading as one readable line (per-letter lines joined by spaces); the search index reuses it.
    const headingText = s => {
      const hd = s.el.querySelector("h1, h2, h3");
      const ls = hd ? [...hd.querySelectorAll(".line")] : [];
      return (ls.length ? ls.map(l => l.textContent).join(" ") : hd?.textContent || "")
        .replace(/\s+/g, " ")
        .replace(/ ([,.])/g, "$1")
        .trim();
    };
    // A short tile label: the heading, or the kicker when the heading is long; band headers and the slide map reuse it.
    const label = s => {
      const t = headingText(s);
      const k = s.el.querySelector(".kicker")?.textContent.trim();
      return t.length > 32 && k ? k : t;
    };
    {
      const parts = rows.filter(r => BANDS[r.group]);
      let num = 1;
      for (const r of rows) {
        if (BANDS[r.group]) num++;
        r.first = num;
        num += r.items.length;
      }
      for (const r of parts) {
        resetIndex();
        const [title, sub] = BANDS[r.group],
          part = parts.indexOf(r) + 1,
          c = THEME[r.group].accent,
          tops = r.items.filter(s => !s.parent);
        const map = h(
          "div",
          { class: "bh-map", "aria-hidden": "true" },
          rows.map((o, i) =>
            h(
              "div",
              { class: `bh-mrow${o === r ? " on" : ""}`, "--i": i },
              h("span", { class: "bh-ml" }, rowName(o.group)),
              BANDS[o.group] ? h("i", { class: "hd" }) : null,
              o.items.filter(s => !s.parent).map(() => h("i"))
            )
          )
        );
        r.header = {
          group: r.group,
          header: true,
          notes: `What: ${PROTO[r.group] ? `Research prototype: ${PROTO[r.group]}. ` : ""}The ${title} section map shows topic tiles; a down-arrow badge marks worked examples.\nWhy: ${sub}.\nWhen: Use this map to choose a topic, then open a tile or follow its examples.`,
          el: h(
            "section",
            { class: "slide bh", "--bh": c[0], "--bh2": c[2] },
            h(
              "div",
              { class: "bh-head" },
              h("div", { class: "kicker" }, `Part ${String(part).padStart(2, "0")} / ${String(parts.length).padStart(2, "0")}`),
              h(
                "h2",
                {},
                title.split(" ").flatMap((w, i) => (i ? [" ", letters(w, "l-grad")] : [letters(w, "l-grad")]))
              ),
              h("p", { class: "bh-sub" }, sub)
            ),
            map,
            h(
              "ol",
              { class: `bh-row${tops.length > 8 ? " many" : ""}${tops.length > 14 ? " lots" : ""}` },
              tops.map((s, i) => {
                const n = r.first + r.items.indexOf(s);
                return h(
                  "li",
                  { "--i": i, "data-go": n - 1, title: `${label(s)} (type ${n}, Enter)` },
                  h("span", { class: "bh-n" }, String(n).padStart(2, "0"), s.kids ? h("span", { class: "bh-k", title: `${s.kids.length} examples below` }, `↓ ${s.kids.length}`) : null),
                  h("span", { class: "bh-t" }, label(s))
                );
              })
            )
          ),
        };
      }
      slides.length = 0;
      rows.forEach((r, ri) => {
        r.start = slides.length;
        (r.header ? [r.header, ...r.items] : r.items).forEach((s, i) => {
          s.band = ri;
          s.step = i;
          s.red = !!THEME[s.group].red;
          if (PROTO[s.group]) s.el.append(h("div", { class: "proto", "aria-label": `Research prototype: ${PROTO[s.group]}` }, h("i"), h("b", {}, "Research prototype"), h("span", {}, PROTO[s.group])));
          else if (s.experimental) s.el.append(h("div", { class: "xbadge", "aria-label": `Experimental: ${s.experimental}` }, "Experimental", h("span", {}, s.experimental)));
          slides.push(s);
        });
      });
    }
    // Stable link ids (#id): a slide's own `id`, else a slug of its heading, so shared links survive reordering.
    // A rename changes the slug; pin the old link by setting `id` on the slide. Duplicates get -2, -3, ...
    {
      const slug = t =>
        t
          .normalize("NFKD")
          .replace(/[\u0300-\u036f]/g, "")
          .toLowerCase()
          .replace(/ı/g, "i")
          .replace(/['’]/g, "")
          .replace(/&/g, " and ")
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-+|-+$/g, "");
      const seen = new Set();
      for (const s of slides) {
        const base = s.id || slug(headingText(s)) || slug(String(s.group)) || "slide";
        let id = base;
        for (let k = 2; seen.has(id); k++) id = `${base}-${k}`;
        seen.add(id);
        s.id = id;
      }
    }

    const stage = h(
      "div",
      { class: "stage" },
      slides.map(s => s.el)
    );
    const progress = h("div", { class: "progress" });
    // the dots are a mini-map of the table: one row of dots per band
    const dotBtns = slides.map((s, i) =>
      h("button", {
        class: s.header ? "hd" : s.parent ? "sub" : s.kids ? "col" : "",
        "aria-label": `Slide ${i + 1}`,
        title: `Slide ${i + 1} (type ${i + 1}, Enter)`,
        onclick: e => {
          e.stopPropagation();
          go(i);
        },
      })
    );
    const dots = h(
      "div",
      { class: "dots" },
      rows.map(r => h("div", { class: "drow", title: rowName(r.group) }, dotBtns.slice(r.start, r.start + (r.header ? 1 : 0) + r.items.length)))
    );
    const arrowBtn = (label, key, keys, d, fn) => {
      const b = h("button", {
        "aria-label": label,
        "aria-keyshortcuts": keys,
        title: `${label} (${key})`,
        onclick: e => {
          e.stopPropagation();
          fn();
        },
      });
      b.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="${d}"/></svg>`;
      b._nav = fn;
      return b;
    };
    const bandPrevBtn = arrowBtn("Previous band", "PgUp or Shift+↑", "PageUp Shift+ArrowUp", "M6 5v14M18 6l-7 6 7 6", () => band(-1));
    const prevBtn = arrowBtn("Previous slide", "←", "ArrowLeft Backspace", "M15 5l-7 7 7 7", () => prev());
    const nextBtn = arrowBtn("Next slide", "→", "ArrowRight Space Enter", "M9 5l7 7-7 7", () => next());
    const bandNextBtn = arrowBtn("Next band", "PgDn or Shift+↓", "PageDown Shift+ArrowDown", "M18 5v14M6 6l7 6-7 6", () => band(1));
    const arrows = h("div", { class: "arrows" }, bandPrevBtn, prevBtn, nextBtn, bandNextBtn);
    // Column button (dock): shown on a column's head and its examples. On the head it is lit with a bouncing
    // arrow and the example count, so the audience knows there is a line below; it steps down the column and,
    // on the last example, turns into an up arrow back to the pattern.
    const vnb = h("button", { class: "vnb" });
    vnb.addEventListener("click", e => {
      e.stopPropagation();
      vnb._nav?.();
    });
    for (const t of ["touchstart", "touchend"]) vnb.addEventListener(t, e => e.stopPropagation(), { passive: true });
    // Taps on the arrows navigate on touchend and cancel the default action, so Safari neither
    // double-tap zooms nor synthesizes a second click, and the window swipe handlers never see them.
    let arrowTouchAt = 0;
    arrows.addEventListener("touchstart", e => e.stopPropagation(), { passive: true });
    arrows.addEventListener("touchmove", e => e.stopPropagation(), { passive: true });
    arrows.addEventListener("touchcancel", e => e.stopPropagation());
    arrows.addEventListener(
      "touchend",
      e => {
        e.stopPropagation();
        if (e.cancelable) e.preventDefault();
        arrowTouchAt = Date.now();
        const b = e.target.closest?.("button"),
          t = e.changedTouches[0];
        if (!b || b.disabled || !t) return;
        const r = b.getBoundingClientRect();
        if (t.clientX >= r.left && t.clientX <= r.right && t.clientY >= r.top && t.clientY <= r.bottom) b._nav();
      },
      { passive: false }
    );
    arrows.addEventListener("click", e => Date.now() - arrowTouchAt < 700 && e.detail > 0 && (e.stopPropagation(), e.preventDefault()), true);
    arrows.addEventListener("click", e => e.stopPropagation());
    const snd = h("button", {
      class: "snd",
      "aria-label": "Toggle sound (M)",
      "aria-keyshortcuts": "M",
      "aria-pressed": "false",
      title: "Sound on/off (M)",
      onclick: e => {
        e.stopPropagation();
        toggleSound();
      },
    });
    snd.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
      '<path d="M4 9h4l5-4v14l-5-4H4z" fill="currentColor"/><g class="waves"><path d="M16 9a4 4 0 0 1 0 6"/><path d="M18.5 6.5a7.5 7.5 0 0 1 0 11"/></g>' +
      '<g class="x"><path d="M16.5 9.5l5 5M21.5 9.5l-5 5"/></g></svg>';
    // search sits left of the sound toggle and opens the full-screen slide search (see ---------- search ----------)
    const fzBtn = h("button", {
      class: "fzb",
      "aria-label": "Search slides (/)",
      "aria-keyshortcuts": "/ Control+K Meta+K",
      "aria-haspopup": "dialog",
      title: "Search slides (/)",
      onclick: e => {
        e.stopPropagation();
        openSearch();
      },
    });
    fzBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="10.5" cy="10.5" r="6"/><path d="M15 15l5.5 5.5"/></svg>';
    // the slide map sits left of search and opens the full-screen overview of every band (see ---------- slide map ----------)
    const smBtn = h("button", {
      class: "smb",
      "aria-label": "Slide map (G)",
      "aria-keyshortcuts": "G",
      "aria-haspopup": "dialog",
      title: "Slide map (G)",
      onclick: e => {
        e.stopPropagation();
        openMap();
      },
    });
    smBtn.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="4" width="5" height="4" rx="1"/><rect x="10" y="4" width="5" height="4" rx="1"/><rect x="16.5" y="4" width="4" height="4" rx="1"/><rect x="3.5" y="10" width="5" height="4" rx="1"/><rect x="10" y="10" width="5" height="4" rx="1"/><rect x="3.5" y="16" width="5" height="4" rx="1"/><rect x="10" y="16" width="5" height="4" rx="1"/><rect x="16.5" y="16" width="4" height="4" rx="1"/></svg>';
    const notes = h("aside", { class: "notes", id: "speaker-notes", "aria-label": "Speaker notes", "aria-live": "polite", tabindex: "0" });
    const notesBtn = h("button", {
      class: "ntb",
      type: "button",
      "aria-label": "Toggle speaker notes (N)",
      "aria-keyshortcuts": "N",
      "aria-controls": "speaker-notes",
      "aria-pressed": "false",
      title: "Speaker notes on/off (N)",
      onclick: e => {
        e.stopPropagation();
        toggleNotes();
      },
    });
    notesBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>';
    // Notes whose every line is "Label: text" (the What/Why/When convention) render as labelled cue blocks;
    // anything else stays plain text. Built with text nodes only, so notes are never parsed as HTML.
    function renderNotes(text) {
      const lines = text.split("\n").filter(l => l.trim());
      const cues = lines.map(l => /^([A-Z][A-Za-z]{0,15}):\s+(\S.*)$/.exec(l.trim()));
      if (!cues.length || cues.some(c => !c)) notes.textContent = text;
      else notes.replaceChildren(...cues.map(([, k, v]) => h("p", { class: "cue" }, h("span", { class: "cue-k" }, `${k}:`), " ", v)));
    }
    function toggleNotes() {
      const visible = notes.classList.toggle("on");
      notesBtn.setAttribute("aria-pressed", String(visible));
      if (!visible && notes.contains(document.activeElement)) notesBtn.focus();
    }
    for (const t of ["touchstart", "touchend"]) notes.addEventListener(t, e => e.stopPropagation(), { passive: true });
    // kiosk mode (K): rotates the visible main-line slides in fullscreen; the same dock button stops it
    const ksb = h("button", {
      class: "ksb",
      "aria-pressed": "false",
      onclick: e => {
        e.stopPropagation();
        setKiosk(!kiosk);
      },
    });
    // tour pill (top right, only during a tour): the tour title and position open the map; × shows every slide again
    const tourT = h("span", { class: "tourp-t" }),
      tourN = h("span", { class: "tourp-n" });
    const tourB = h(
      "button",
      {
        class: "tourp-b",
        type: "button",
        title: "Open the slide map (G)",
        "aria-keyshortcuts": "G",
        onclick: e => {
          e.stopPropagation();
          openMap();
        },
      },
      h("i", {}, "Tour"),
      tourT,
      tourN
    );
    const tourP = h(
      "div",
      { class: "tourp" },
      tourB,
      h(
        "button",
        {
          class: "tourp-x",
          type: "button",
          "aria-label": "Leave the tour and show all slides",
          title: "Show all slides (X)",
          "aria-keyshortcuts": "X",
          onclick: e => {
            e.stopPropagation();
            setView(null);
          },
        },
        "×"
      )
    );
    for (const t of ["touchstart", "touchend"]) tourP.addEventListener(t, e => e.stopPropagation(), { passive: true });
    const wash = h("div", { class: "wash" });
    const gridc = h("canvas", { class: "gridc" });
    document.body.append(canvas, wash, gridc, stage, progress, dots, arrows, vnb, ksb, notesBtn, smBtn, fzBtn, ...(config.sound ? [snd] : []), notes, tourP);
    // the pressed state (sound on) and the struck-out icon (.off) always follow the sound service
    const paintSound = () => {
      snd.classList.toggle("off", !!sfx.muted);
      snd.setAttribute("aria-pressed", String(!sfx.muted));
    };
    paintSound();

    // Dot map: left offset + the longest band of 8px dots with 5px gaps (example dots are 4px with 3px gaps) + 16px for the wide active dot; dock buttons need ~230px on the right.
    const dotsWidth = 28 + Math.max(...rows.map(r => r.items.reduce((w, s) => w + (s.parent ? 7 : 13), r.header ? 13 : 0))) + 16 + 400;
    const fit = () => {
      const s = Math.min(innerWidth / W, innerHeight / H);
      stage.style.transform = `scale(${s}) translate(-50%, -50%)`;
      document.body.classList.toggle("compact-nav", dotsWidth > innerWidth - 230);
    };
    addEventListener("resize", fit);
    fit();

    // Compact dock idle fade (see body.nav-idle in the runtime styles); mouse moves count only for a real mouse,
    // so a touch never relies on sticky hover to wake the controls.
    let idleT = null;
    const wake = e => {
      if (e?.type === "pointermove" && e.pointerType !== "mouse") return;
      document.body.classList.remove("nav-idle");
      clearTimeout(idleT);
      idleT = setTimeout(() => document.body.classList.add("nav-idle"), 2500);
    };
    for (const t of ["touchstart", "pointerdown", "pointermove", "keydown", "focusin"]) addEventListener(t, wake, { capture: true, passive: true });
    wake();

    // F toggles fullscreen where the browser allows it (standard or WebKit-prefixed API). iPhone Safari offers no
    // page fullscreen; there the deck draws edge to edge and runs without browser bars when added to the Home Screen.
    function toggleFullscreen() {
      /** @type {any} */
      const d = document;
      const p = d.fullscreenElement || d.webkitFullscreenElement ? (d.exitFullscreen || d.webkitExitFullscreen)?.call(d) : (d.documentElement.requestFullscreen || d.documentElement.webkitRequestFullscreen)?.call(d.documentElement);
      p?.catch?.(() => {});
    }

    function toggleSound() {
      if (!config.sound) return;
      sfx.toggle();
      paintSound();
      snd.classList.add("flash");
      setTimeout(() => snd.classList.remove("flash"), 400);
    }

    // ---------- kiosk ----------
    // Kiosk mode walks the visible main line (tour or view; column examples are skipped) in deck order, wrapping from
    // the last slide to the first, without running slide steps (onNext). It asks for fullscreen from the K key or dock
    // button (a user gesture) and, while sound is on, unlocks audio from that same gesture so band music plays on the
    // timed moves; the sound toggle stays beside the stop button. If the browser lacks or refuses fullscreen, slides
    // still rotate in the window. Escape, K, the button or leaving fullscreen stop it; a manual move restarts the
    // countdown. One timer at a time, cleared on every re-arm and stop.
    const KIOSK_MS = Math.max(1000, Number(config.kioskInterval) || 15000);
    /** @type {{ t: ReturnType<typeof setTimeout> | null, full: boolean, entered: boolean } | null} */
    let kiosk = null;
    /** @type {any} */
    const doc = document;
    const fsEl = () => doc.fullscreenElement || doc.webkitFullscreenElement || null;
    const exitFs = () => {
      try {
        (doc.exitFullscreen || doc.webkitExitFullscreen)?.call(doc)?.catch?.(() => {});
      } catch {}
    };
    const kioskNext = () => {
      for (let k = 1; k <= slides.length; k++) {
        const i = (cur + k) % slides.length;
        if (vis(i) && !slides[i].parent) return i;
      }
      return cur;
    };
    const kioskArm = () => {
      if (!kiosk) return;
      clearTimeout(kiosk.t);
      kiosk.t = setTimeout(() => {
        if (!kiosk) return;
        // hold the slide while any dialog (search, map, a deck's own modal) is open
        if (!document.querySelector("dialog[open], [aria-modal='true']")) go(kioskNext());
        kioskArm();
      }, KIOSK_MS);
    };
    const paintKiosk = () => {
      const label = kiosk ? "Stop kiosk mode" : "Start kiosk mode: rotate slides in fullscreen",
        keys = kiosk ? "K or Esc" : "K";
      ksb.setAttribute("aria-pressed", String(!!kiosk));
      ksb.setAttribute("aria-label", `${label} (${keys})`);
      ksb.setAttribute("aria-keyshortcuts", kiosk ? "K Escape" : "K");
      ksb.title = `${label} (${keys})`;
      ksb.innerHTML = kiosk
        ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><rect x="7" y="7" width="10" height="10" rx="1.5" fill="currentColor"/></svg>'
        : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 9V5h4M20 9V5h-4M4 15v4h4M20 15v4h-4"/><path d="M10 9l5 3-5 3z" fill="currentColor"/></svg>';
      document.body.classList.toggle("kiosk", !!kiosk);
    };
    function setKiosk(on) {
      on = !!on;
      if (on === !!kiosk) return on;
      if (on) {
        kiosk = { t: null, full: !!fsEl(), entered: false };
        const k = kiosk;
        if (config.sound && !sfx.muted) sfx.unlock?.();
        const de = doc.documentElement,
          req = de.requestFullscreen || de.webkitRequestFullscreen;
        if (!k.full && req) {
          k.entered = true;
          try {
            // each session owns its request: one that lands after its session stopped (and no newer one runs) is undone
            req.call(de)?.then?.(
              () => kiosk || exitFs(),
              () => (k.entered = false)
            );
          } catch {
            k.entered = false;
          }
        }
        kioskArm();
      } else {
        const k = kiosk;
        clearTimeout(k.t);
        kiosk = null;
        // leave the fullscreen kiosk mode asked for, not one the presenter already had
        if (k.entered && fsEl()) exitFs();
      }
      paintKiosk();
      return on;
    }
    const onFullscreen = () => {
      if (!kiosk) return;
      if (fsEl()) kiosk.full = true;
      else if (kiosk.full) setKiosk(false);
    };
    for (const t of ["fullscreenchange", "webkitfullscreenchange"]) document.addEventListener(t, onFullscreen);
    paintKiosk();

    // ---------- tours ----------
    // A tour is a filtered view of the deck: only its slides are walked, dotted and counted; jumping to any other slide
    // (search, map, band header) leaves it. config.tours holds named tours { title, bands?, slides? } (group and slide ids);
    // a view designed in the slide map travels as base64url JSON { t: title, b: bands, s: slide ids }.
    // Hash: #<slide-id>, #<slide-id>&tour=<name> or #<slide-id>&view=<payload>.
    const b64e = o =>
      btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(o))))
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "");
    const b64d = p => JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(p.replace(/-/g, "+").replace(/_/g, "/")), c => c.charCodeAt(0))));
    // An example (a slide hung `under` a pattern) rides with its pattern: without the pattern it drops out of the view.
    const resolveView = spec => {
      const bs = new Set(spec?.bands || spec?.b || []),
        ids = new Set(spec?.slides || spec?.s || []),
        set = new Set();
      slides.forEach((s, i) => (bs.has(s.group) || ids.has(s.id)) && set.add(i));
      for (const i of set) if (slides[i].parent && !set.has(slides.indexOf(slides[i].parent))) set.delete(i);
      return set.size ? set : null;
    };
    // a renamed slide id or band, or an example listed without its pattern, silently shrinks a tour: name it
    for (const [name, t] of Object.entries(TOURS)) {
      const miss = [...(t.bands || []).filter(g => !slides.some(s => s.group === g)), ...(t.slides || []).filter(id => !slides.some(s => s.id === id))];
      if (miss.length) console.warn(`CracktroDeck: tour "${name}" names unknown bands or slide ids: ${miss.join(", ")}`);
      const set = resolveView(t) || new Set(),
        orphans = (t.slides || []).filter(id => slides.some((s, i) => s.id === id && s.parent && !set.has(i)));
      if (orphans.length) console.warn(`CracktroDeck: tour "${name}" lists examples without their pattern slide: ${orphans.join(", ")}`);
    }
    const viewFrom = q => {
      if (q.tour && Object.hasOwn(TOURS, q.tour)) {
        const set = resolveView(TOURS[q.tour]);
        return set && { key: `tour=${q.tour}`, tour: q.tour, title: TOURS[q.tour].title || q.tour, set };
      }
      if (q.view)
        try {
          const spec = b64d(q.view),
            set = resolveView(spec);
          return set && { key: `view=${q.view}`, title: typeof spec.t === "string" && spec.t.trim() ? spec.t.trim().slice(0, 80) : "Custom view", set };
        } catch {}
      return null;
    };
    /** @type {{ key: string, tour?: string, title: string, set: Set<number>, order: number[] } | null} */
    let view = null;
    const vis = i => !view || view.set.has(i);
    const firstVis = () => slides.findIndex((s, i) => vis(i));
    const lastVis = () => {
      for (let i = slides.length - 1; i >= 0; i--) if (vis(i)) return i;
      return -1;
    };
    const hashFor = n => `#${slides[n].id}${view ? "&" + view.key : ""}`;

    // ---------- navigation ----------
    let cur = -1,
      redT = null,
      bgN = null; // slide whose THEME the background shows while a staged light/dark swap is pending
    // entering a "red" slide forward: the current slide first fades to red, then the red slide enters under the wash
    function go(n) {
      n = Math.max(0, Math.min(slides.length - 1, n));
      if (!vis(n)) setView(null); // jumping outside the tour leaves it
      if (n === cur || redT) return;
      if (cur >= 0 && n > cur && slides[n].red && !slides[cur].red && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
        wash.classList.add("on", "cover");
        redT = setTimeout(() => {
          redT = null;
          show(n, true);
        }, 900);
        return;
      }
      // light <-> dark band: stage it so nothing overlaps; old slide fades out, then the background swaps, then the new slide enters
      if (cur >= 0 && !!THEME[slides[n].group].light !== !!THEME[slides[cur].group].light && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
        const old = slides[cur].el;
        old.classList.add("fadeout");
        wash.style.transitionDuration = ".6s";
        wash.classList.remove("on");
        redT = setTimeout(() => {
          bgN = n;
          document.body.classList.toggle("light", !!THEME[slides[n].group].light);
          redT = setTimeout(() => {
            redT = null;
            show(n, true);
          }, 900);
        }, 600);
        return;
      }
      show(n);
    }
    // siblings in a group push left (right going back); crossing into another group slides up (down going back)
    function show(n, covered) {
      const dir = cur >= 0 && n < cur ? -1 : 1;
      wash.classList.toggle("on", !!slides[n].red);
      sfx.cut();
      sfx.techno(slides[n].techno || 0);
      const th = THEME[slides[n].group];
      document.body.classList.toggle("light", !!th.light);
      if (cur >= 0) sfx.slide(dir, th.music, th.prog[slides[n].step % th.prog.length]);
      const vert = cur >= 0 && slides[n].group !== slides[cur].group ? (dir > 0 ? "up" : "down") : cur >= 0 && colOf(slides[n]) && colOf(slides[n]) === colOf(slides[cur]) ? (dir > 0 ? "up" : "down") : null;
      const TR = ["up-in", "up-out", "down-in", "down-out"];
      if (cur >= 0 && covered) {
        // already hidden under the red wash: drop it instantly so it doesn't flash back when the wash moves behind the stage
        const old = slides[cur].el;
        clearTimeout(old._lt);
        old.classList.remove("active", "leaving", "fadeout", ...TR);
      } else if (cur >= 0) {
        const old = slides[cur].el;
        old.classList.remove("active", ...TR);
        old.style.setProperty("--dx", dir > 0 ? "100%" : "-100%");
        old.classList.add("leaving");
        if (vert) old.classList.add(vert + "-out");
        clearTimeout(old._lt);
        old._lt = setTimeout(() => old.classList.remove("leaving", ...TR), 800);
      }
      const el = slides[n].el;
      clearTimeout(el._lt);
      el.classList.remove(...TR);
      if (vert) el.classList.add(vert + "-in");
      wash.classList.remove("cover");
      wash.style.transitionDuration = "";
      bgN = null;
      el.style.setProperty("--dx", cur < 0 ? "0%" : dir > 0 ? "100%" : "-100%");
      el.classList.remove("leaving");
      void el.offsetWidth; // restart CSS animations on re-entry
      el.classList.add("active");
      cur = n;
      slides[n].enter?.(dir);
      chrome(n);
      kioskArm();
    }
    // progress, dots, dock buttons, notes and hash for slide n (also refreshed when a tour starts or ends)
    function chrome(n) {
      const at = view ? view.order.indexOf(n) : n,
        len = view ? view.order.length : slides.length;
      progress.style.transform = `scaleX(${(at + 1) / len})`;
      dotBtns.forEach((d, i) => d.classList.toggle("on", i === n));
      [...dots.children].forEach((r, i) => r.classList.toggle("on", i === slides[n].band));
      prevBtn.disabled = prevTop(n) == null;
      nextBtn.disabled = nextTop(n) == null;
      bandPrevBtn.disabled = bandTo(slides[n].band, -1) == null;
      bandNextBtn.disabled = bandTo(slides[n].band, 1) == null;
      paintColumn(n);
      renderNotes(slides[n].notes || "");
      notes.scrollTop = 0;
      if (view) tourN.textContent = `${at + 1}/${len}`;
      history.replaceState(null, "", hashFor(n));
    }
    // Left/right walk the main line of slides (column heads, never their examples) and wrap across bands;
    // up/down walk a column, and past its ends (or off any column) jump to the header of the band above/below.
    // In a tour only its slides count (an example is only ever in a tour together with its pattern).
    const pat = n => (slides[n].parent ? slides.indexOf(slides[n].parent) : -1);
    const onMain = i => vis(i) && pat(i) < 0;
    const top = n => (pat(n) >= 0 ? pat(n) : n);
    const nextTop = n => {
      for (let i = top(n) + 1; i < slides.length; i++) if (onMain(i)) return i;
      return null;
    };
    const prevTop = n => {
      for (let i = top(n) - 1; i >= 0; i--) if (onMain(i)) return i;
      return null;
    };
    // first visible slide of the nearest band above (d=-1) or below (d=1) that has one
    const bandTo = (from, d) => {
      for (let b = from + d; b >= 0 && b < rows.length; b += d) {
        const i = slides.findIndex((s, k) => s.band === b && vis(k));
        if (i >= 0) return i;
      }
      return null;
    };
    const band = d => {
      const i = bandTo(slides[cur].band, d);
      if (i != null) go(i);
    };
    // the visible part of a slide's column, pattern first, or null when there is nothing to walk
    const colList = s => {
      const col = colOf(s);
      if (!col || !vis(slides.indexOf(col))) return null;
      const list = [col, ...col.kids.filter(k => vis(slides.indexOf(k)))];
      return list.length > 1 ? list : null;
    };
    const vstep = d => {
      const list = colList(slides[cur]);
      if (!list) return band(d);
      const i = list.indexOf(slides[cur]) + d;
      if (i >= 0 && i < list.length) go(slides.indexOf(list[i]));
      else band(d);
    };
    function paintColumn(n) {
      const list = colList(slides[n]);
      vnb.classList.toggle("on", !!list);
      if (!list) return (vnb._nav = null);
      const col = list[0],
        N = list.length - 1,
        at = list.indexOf(slides[n]),
        back = at === N,
        label = at ? (back ? "Back to the pattern" : `Next example (${at + 1} of ${N})`) : `${N} example${N > 1 ? "s" : ""} below`;
      vnb.classList.toggle("head", at === 0);
      vnb.setAttribute("aria-label", label);
      vnb.setAttribute("aria-keyshortcuts", back ? "U" : "ArrowDown");
      vnb.title = `${label} (${back ? "U" : "↓"})`;
      vnb.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="${back ? "M12 19V5M5 12l7-7 7 7" : "M12 5v14M5 12l7 7 7-7"}"/></svg><span class="vnb-t">${at ? `${at}/${N}` : N}</span>`;
      vnb._nav = back ? () => go(slides.indexOf(col)) : () => vstep(1);
    }
    const next = () => {
        if (slides[cur].onNext?.()) (sfx.touch(), sfx.step(1));
        else {
          const i = nextTop(cur);
          if (i != null) go(i);
        }
      },
      prev = () => {
        if (slides[cur].onPrev?.()) (sfx.touch(), sfx.step(-1));
        else {
          const i = prevTop(cur);
          if (i != null) go(i);
        }
      };

    let jumpBuf = "",
      jumpT = null;
    addEventListener("keydown", e => {
      /** @type {any} */
      const target = e.target;
      if (fz.open || smp.open) return; // the search and map dialogs handle their own keys
      if (target.closest?.(".notes") && !["n", "N", "Escape"].includes(e.key)) return;
      if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        openSearch();
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "/" && !target.closest?.("input, textarea, select, [contenteditable]")) {
        e.preventDefault();
        openSearch();
        return;
      }
      if ((e.key === "Enter" || e.key === " ") && target.closest?.("a, button, input, textarea, select, [contenteditable]")) return;
      if (slides[cur].onKey?.(e)) {
        sfx.touch();
        e.preventDefault();
        return;
      }
      // typing a slide number then Enter jumps there (the dots' shortcut); the digits expire after 2 s
      const typed = jumpBuf;
      jumpBuf = "";
      clearTimeout(jumpT);
      if (/^[0-9]$/.test(e.key)) {
        jumpBuf = (typed + e.key).slice(-4);
        jumpT = setTimeout(() => (jumpBuf = ""), 2000);
        return;
      }
      if (e.key === "Enter" && +typed) {
        e.preventDefault();
        go(+typed - 1);
        return;
      }
      if (["ArrowRight", " ", "Enter"].includes(e.key)) {
        e.preventDefault();
        next();
      } else if (["ArrowLeft", "Backspace"].includes(e.key)) {
        e.preventDefault();
        prev();
      } else if (e.key === "PageDown" || e.key === "PageUp") {
        e.preventDefault();
        band(e.key === "PageDown" ? 1 : -1);
      } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        (e.shiftKey ? band : vstep)(e.key === "ArrowDown" ? 1 : -1);
      } else if (e.key === "u" || e.key === "U") {
        const col = colOf(slides[cur]);
        if (col) go(slides.indexOf(col));
      } else if (e.key === "Home") go(firstVis());
      else if (e.key === "End") go(lastVis());
      else if (e.key === "f" || e.key === "F") toggleFullscreen();
      else if ((e.key === "k" || e.key === "K") && !e.repeat && !target.closest?.("input, textarea, select, [contenteditable]")) setKiosk(!kiosk);
      else if (e.key === "Escape" && kiosk && !e.defaultPrevented) {
        e.preventDefault();
        setKiosk(false);
      } else if (e.key === "n" || e.key === "N") toggleNotes();
      else if (e.key === "Escape" && notes.classList.contains("on")) toggleNotes();
      else if (e.key === "m" || e.key === "M") toggleSound();
      else if (e.key === "g" || e.key === "G") openMap();
      else if ((e.key === "x" || e.key === "X") && view) setView(null);
    });
    addEventListener("click", e => {
      /** @type {any} */
      const target = e.target;
      if (fz.open || smp.open || target.closest(".fz, .smp, .notes, a, button")) return;
      const tile = target.closest("[data-go]");
      if (tile) return go(+tile.dataset.go);
      e.clientX < innerWidth / 3 ? prev() : next();
    });
    let tx = null,
      ty = null;
    addEventListener("touchstart", e => (fz.open || smp.open ? (tx = ty = null) : ((tx = e.touches[0].clientX), (ty = e.touches[0].clientY))), { passive: true });
    addEventListener("touchend", e => {
      if (tx == null || fz.open || smp.open) return;
      const dx = e.changedTouches[0].clientX - tx,
        dy = e.changedTouches[0].clientY - ty;
      tx = ty = null;
      if (Math.abs(dy) > Math.abs(dx)) Math.abs(dy) > 50 && vstep(dy < 0 ? 1 : -1);
      else if (Math.abs(dx) > 50) dx < 0 ? next() : prev();
    });
    // #id selects a slide; a legacy numeric #N still opens the Nth slide and is rewritten to its #id.
    // &tour=<name> or &view=<payload> after it starts (or keeps) a tour; a slide outside the tour opens its first slide.
    const parseHash = () => {
      const [head, ...rest] = location.hash.slice(1).split("&"),
        q = {};
      for (const p of head.includes("=") ? [head, ...rest] : rest) {
        const k = p.indexOf("=");
        if (k > 0)
          try {
            q[p.slice(0, k)] = decodeURIComponent(p.slice(k + 1));
          } catch {}
      }
      let id = head.includes("=") ? "" : head;
      try {
        id = decodeURIComponent(id);
      } catch {}
      return { id, q };
    };
    const followHash = () => {
      const { id, q } = parseHash(),
        v = viewFrom(q);
      if ((v?.key ?? null) !== (view?.key ?? null)) setView(v);
      let n = /^\d+$/.test(id) ? Math.max(0, Math.min(slides.length - 1, +id - 1)) : slides.findIndex(s => s.id === id);
      if (n < 0 || !vis(n)) n = firstVis();
      if (n === cur) history.replaceState(null, "", hashFor(n));
      else go(n);
    };
    addEventListener("hashchange", followHash);
    // Start, switch or (with null) leave a tour; dots, band-header tiles and the dock show only its slides.
    function setView(v) {
      view = v && { ...v, order: [...v.set].sort((a, b) => a - b) };
      document.body.classList.toggle("touring", !!view);
      dotBtns.forEach((d, i) => d.classList.toggle("out", !vis(i)));
      [...dots.children].forEach((r, b) => r.classList.toggle("out", !slides.some((s, i) => s.band === b && vis(i))));
      for (const li of stage.querySelectorAll(".bh-row li[data-go]")) li.classList.toggle("out", !vis(+li.dataset.go));
      tourT.textContent = view?.title || "";
      tourB.firstChild.textContent = view?.tour ? "Tour" : "View";
      tourB.title = view ? `${view.tour ? "Tour" : "Custom view"}: ${view.title}. Open the slide map (G)` : "Open the slide map (G)";
      tourB.setAttribute("aria-label", tourB.title);
      if (cur >= 0 && vis(cur)) chrome(cur);
      if (smpTiles) smpPaint();
    }

    // ---------- search ----------
    // "/" (or Ctrl/Cmd+K, or the dock's search button) opens a full-screen dialog that fuzzy-searches every slide.
    // The index is precomputed once: each slide's title, its slides.md summary and keywords (the JSON-LD hasPart
    // description and keywords the docs build injects; absent on a plain static preview) and its body text plus speaker notes.
    // Matching is bounds-based: each query term must occur, letters in order, inside one window of a field, and the
    // window is bounded (at most one and a half times the term), so letters scattered through long notes never match.
    // The tightest window wins: forward from each occurrence of the first letter to the earliest end, then backward to
    // the latest start. Its bounds score the match (term length / window width, word-start and field bonuses) and mark it.
    /** @type {[string, number][]} */
    const FZ_FIELDS = [
      ["title", 3],
      ["keywords", 2.5],
      ["summary", 2],
      ["body", 1],
    ];
    let fzIndex = null,
      fzHits = [],
      fzSel = 0;
    const fzNorm = t => t.replace(/\s+/g, " ").trim();
    // visible slide text, one space between text nodes; per-letter headings (the title) and decorative aria-hidden parts are skipped
    const fzText = el => {
      const out = [],
        walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, {
          acceptNode: n => (n.parentElement?.closest('.line, style, script, [aria-hidden="true"]') ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
        });
      while (walk.nextNode()) out.push(walk.currentNode.nodeValue);
      return out.join(" ");
    };
    const fzBuild = () => {
      let parts = [];
      for (const el of document.querySelectorAll('script[type="application/ld+json"]')) {
        try {
          const d = JSON.parse(el.textContent);
          if (d?.["@type"] === "PresentationDigitalDocument" && Array.isArray(d.hasPart)) parts = d.hasPart;
        } catch {}
      }
      return slides.map((s, i) => {
        const part = parts.find(p => typeof p?.url === "string" && p.url.endsWith(`#${s.id}`)) || parts.find(p => p?.position === i + 1),
          title = headingText(s) || part?.name || `Slide ${i + 1}`,
          summary = fzNorm(part?.description || ""),
          keywords = fzNorm(typeof part?.keywords === "string" ? part.keywords : ""),
          body = fzNorm(`${fzText(s.el)} ${s.notes || ""}`);
        return {
          n: i,
          band: BANDS[s.group]?.[0] || "",
          title,
          summary,
          keywords,
          body,
          notes: fzNorm(s.notes || ""),
          lc: { title: title.toLowerCase(), keywords: keywords.toLowerCase(), summary: summary.toLowerCase(), body: body.toLowerCase() },
        };
      });
    };
    // Tightest bounded window [start, end) of `text` holding the characters of `term` in order, or null.
    const fzBounds = (text, term) => {
      const n = term.length,
        max = n + Math.ceil(n / 2);
      /** @type {[number, number] | null} */
      let best = null;
      for (let i = text.indexOf(term[0]); i >= 0; i = text.indexOf(term[0], i + 1)) {
        let j = i,
          k = 0;
        for (; j < text.length && k < n && j - i < max; j++) if (text[j] === term[k]) k++;
        if (k < n) continue;
        let b = j - 1;
        for (k = n - 1; k >= 0; b--) if (text[b] === term[k]) k--;
        b++;
        if (!best || j - b < best[1] - best[0] || (j - b === best[1] - best[0] && /\W/.test(text[b - 1] || " ") && !/\W/.test(text[best[0] - 1] || " "))) best = [b, j];
        if (j - b === n && (b === 0 || /\W/.test(text[b - 1]))) break; // contiguous at a word start: cannot do better
        i = Math.max(i, b);
      }
      return best;
    };
    const fzScore = (text, [b, e], n) => n / (e - b) + (b === 0 || /\W/.test(text[b - 1]) ? 0.5 : 0) + 0.25 * (1 - Math.min(b, 400) / 400);
    const fzSearch = q => {
      const terms = q.toLowerCase().split(/\s+/).filter(Boolean).slice(0, 8);
      if (!terms.length) return fzIndex.map(r => ({ r, score: 0, marks: {} }));
      // "#12" jumps straight to slide 12; a bare "12" also matches text and ranks slide 12 first
      const num = /^(#?)(\d+)$/.exec(q.trim());
      if (num?.[1]) return fzIndex.filter(r => r.n + 1 === +num[2]).map(r => ({ r, score: 1, marks: {} }));
      const hits = [];
      for (const r of fzIndex) {
        let score = 0;
        const marks = { title: [], keywords: [], summary: [], body: [] };
        for (const term of terms) {
          let best = 0;
          for (const [f, w] of FZ_FIELDS) {
            const bd = r.lc[f] && fzBounds(r.lc[f], term);
            if (!bd) continue;
            marks[f].push(bd);
            best = Math.max(best, w * fzScore(r.lc[f], bd, term.length));
          }
          if (!best) {
            score = 0;
            break;
          }
          score += best;
        }
        if (num && +num[2] === r.n + 1) score += 100;
        if (score) hits.push({ r, score, marks });
      }
      return hits.sort((a, b) => b.score - a.score || a.r.n - b.r.n).slice(0, 60);
    };
    // text with <mark>s over the (merged) bounds, built as DOM nodes, never as HTML
    const fzMark = (text, bounds) => {
      const out = [];
      let at = 0;
      for (const [b, e] of [...bounds].sort((x, y) => x[0] - y[0])) {
        const s = Math.max(b, at);
        if (e <= s) continue;
        if (s > at) out.push(text.slice(at, s));
        out.push(h("mark", {}, text.slice(s, e)));
        at = e;
      }
      if (at < text.length) out.push(text.slice(at));
      return out;
    };
    // the line under the title: the summary when it matched (or nothing else did), else a body excerpt around the first match
    const fzLine = ({ r, marks }) => {
      if (r.summary && (marks.summary?.length || !marks.body?.length)) return fzMark(r.summary, marks.summary || []);
      if (!marks.body?.length) return r.notes.slice(0, 220);
      const first = Math.min(...marks.body.map(m => m[0])),
        from = Math.max(0, first - 60),
        to = Math.min(r.body.length, first + 160);
      const inner = marks.body.filter(([b, e]) => b >= from && e <= to).map(([b, e]) => [b - from, e - from]);
      return [from ? "…" : "", ...fzMark(r.body.slice(from, to), inner), to < r.body.length ? "…" : ""];
    };
    // one tag per keyword that holds a match, with the match marked; keywords are joined by ", " in the index
    const fzKeys = ({ r, marks }) => {
      const out = [];
      if (!marks.keywords?.length) return out;
      let at = 0;
      for (const key of r.keywords.split(", ")) {
        const end = at + key.length,
          inner = marks.keywords.filter(([b, e]) => b >= at && e <= end).map(([b, e]) => [b - at, e - at]);
        if (inner.length) out.push(h("span", { class: "fz-k" }, ...fzMark(key, inner)));
        at = end + 2;
      }
      return out;
    };
    const fzIn = h("input", {
      type: "search",
      class: "fz-in",
      placeholder: "Search slides",
      autocomplete: "off",
      spellcheck: false,
      "aria-label": "Search slides",
      role: "combobox",
      "aria-expanded": "true",
      "aria-controls": "fz-list",
      "aria-autocomplete": "list",
    });
    const fzList = h("ul", { class: "fz-list", id: "fz-list", role: "listbox", "aria-label": "Matching slides" });
    const fzSt = h("div", { class: "fz-st", "aria-live": "polite" });
    const fz = h(
      "dialog",
      { class: "fz", "aria-label": "Search slides" },
      h(
        "div",
        { class: "fz-box" },
        h("div", { class: "fz-bar" }, fzIn, h("button", { class: "fz-x", type: "button", "aria-label": "Close search (Esc)", "aria-keyshortcuts": "Escape", title: "Close search (Esc)", onclick: () => fz.close() }, "Esc")),
        fzSt,
        fzList
      )
    );
    document.body.append(fz);
    const fzPaint = () => {
      [...fzList.children].forEach((li, i) => li.setAttribute("aria-selected", i === fzSel ? "true" : "false"));
      const li = fzList.children[fzSel];
      if (li) {
        fzIn.setAttribute("aria-activedescendant", li.id);
        li.scrollIntoView({ block: "nearest" });
      } else fzIn.removeAttribute("aria-activedescendant");
    };
    const fzRender = () => {
      fzHits = fzSearch(fzIn.value);
      fzSel = 0;
      fzList.replaceChildren(
        ...fzHits.map((hit, i) =>
          h(
            "li",
            { id: `fz-o-${i}`, role: "option", onclick: () => fzPick(i), onmousemove: () => fzSel !== i && ((fzSel = i), fzPaint()) },
            h("span", { class: "fz-n" }, String(hit.r.n + 1).padStart(2, "0")),
            h(
              "div",
              { class: "fz-t" },
              ...fzMark(hit.r.title, hit.marks.title || []),
              hit.r.band && hit.r.band !== hit.r.title ? h("span", { class: "fz-b" }, hit.r.band) : null,
              // matched keywords explain hits whose title, summary and text do not show the query
              ...fzKeys(hit)
            ),
            h("div", { class: "fz-s" }, fzLine(hit))
          )
        )
      );
      fzSt.textContent = fzIn.value.trim()
        ? fzHits.length
          ? `${fzHits.length} matching slide${fzHits.length === 1 ? "" : "s"} · ↑↓ to choose, Enter to open`
          : "No slides match"
        : `${fzHits.length} slides · type to search titles, keywords, summaries and slide text`;
      fzPaint();
    };
    function fzPick(i) {
      const hit = fzHits[i];
      if (!hit) return;
      fz.close();
      go(hit.r.n);
    }
    function openSearch() {
      if (fz.open) return;
      fzIndex ??= fzBuild();
      fz.showModal();
      fzIn.select();
      fzRender();
    }
    fzIn.addEventListener("input", fzRender);
    fz.addEventListener("keydown", e => {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        if (fzHits.length) ((fzSel = (fzSel + (e.key === "ArrowDown" ? 1 : -1) + fzHits.length) % fzHits.length), fzPaint());
      } else if (e.key === "Enter" && e.target === fzIn) {
        e.preventDefault();
        fzPick(fzSel);
      } else if (e.key === "Escape") {
        // close explicitly: browsers may ignore a repeated Escape without fresh user activation
        e.preventDefault();
        fz.close();
      }
    });
    // a click on the dialog's own surface (outside the search box) closes it
    fz.addEventListener("click", e => e.target === fz && fz.close());
    // closing returns focus to the opener; drop it so Space and Enter go back to driving the deck
    fz.addEventListener("close", () => {
      /** @type {any} */
      const active = document.activeElement;
      active?.blur?.();
    });
    for (const t of ["touchstart", "touchend"]) fz.addEventListener(t, e => e.stopPropagation(), { passive: true });

    // ---------- slide map ----------
    // G (or the dock's map button) opens a full-screen overview: per band a header (title and slide range) above a grid of tiles,
    // one per slide, the current one ringed. Tiles fade in staggered by band and position (skipped under prefers-reduced-motion).
    // Click or Enter opens a slide; Left/Right step in slide order, Up/Down move to the nearest tile on the line above/below.
    // A column (a pattern slide with `kids`) gets its own grid line: the pattern tile, then its examples behind a dotted rule.
    const smpBody = h("div", { class: "smp-body" });
    const smpHead = h("h2", { class: "smp-h" }, "Slide map");
    // Tours (very bottom of the map): every slide, each config.tours entry, the custom view in use, and "Design a view".
    // Design mode turns tiles into toggles and band headings into whole-band checkboxes; the bar below the map shows
    // the count, an optional title and the shareable link (#<first slide>&view=<base64url { t, b, s }>).
    let smpDesign = false;
    const smpSel = new Set();
    const bandIdx = b => slides.flatMap((s, i) => (s.band === b ? [i] : []));
    const smpTourList = h("div", { class: "smp-tl", role: "group", "aria-label": "Tours" });
    const smpTours = h(
      "section",
      { class: "smp-row smp-tours", "--c": "#4493f8", "aria-labelledby": "smp-tours-h" },
      h("h3", { class: "smp-l", id: "smp-tours-h" }, "Tours", " ", h("small", {}, "filtered views of the deck, shareable by link")),
      smpTourList
    );
    const smpCount = h("span", { class: "smp-dc", "aria-live": "polite" });
    const smpTitle = h("input", { type: "text", class: "smp-dt", placeholder: "View title (optional)", maxLength: 80, "aria-label": "View title", oninput: () => smpPaint() });
    const smpLink = h("input", { type: "text", class: "smp-dl", readOnly: true, "aria-label": "Link to this view", onfocus: e => e.target.select() });
    const dbtn = (text, key, label, fn, cls = "") => h("button", { class: `smp-db ${cls}`, type: "button", title: `${text} (${label})`, "aria-keyshortcuts": key, onclick: fn }, text);
    const smpApply = dbtn("Apply view", "Control+Enter", "Ctrl+Enter", () => smpUse(viewFrom({ view: b64e(smpSpec()) }), false), "go");
    const smpCopy = dbtn("Copy link", "L", "L", () => {
      const done = () => ((smpCopy.textContent = "Copied"), setTimeout(() => (smpCopy.textContent = "Copy link"), 1500));
      navigator.clipboard?.writeText(smpLink.value).then(done, () => smpLink.select()) ?? smpLink.select();
    });
    const smpDBar = h(
      "div",
      { class: "smp-d", role: "group", "aria-label": "Design a view" },
      smpCount,
      dbtn("Select all", "A", "A", () =>
        smpPick(
          slides.map((s, i) => i),
          true
        )
      ),
      dbtn("Clear", "C", "C", () => smpPick([...smpSel], false)),
      smpTitle,
      smpLink,
      smpCopy,
      smpApply,
      dbtn("Cancel", "Escape", "Esc", () => smpDesignMode(false))
    );
    // the designed view: whole bands by group (later slides of the band join automatically), other slides by id
    const smpSpec = () => {
      const b = [],
        s = [];
      rows.forEach((r, bi) => {
        const idx = bandIdx(bi);
        if (idx.every(i => smpSel.has(i))) b.push(r.group);
        else for (const i of idx) if (smpSel.has(i)) s.push(slides[i].id);
      });
      const t = smpTitle.value.trim();
      return { ...(t && { t }), ...(b.length && { b }), ...(s.length && { s }) };
    };
    const smpPick = (idx, on) => {
      for (const i of idx) on ? smpSel.add(i) : smpSel.delete(i);
      // an example rides with its pattern: it cannot be picked alone and leaves with it
      for (const i of smpSel) if (pat(i) >= 0 && !smpSel.has(pat(i))) smpSel.delete(i);
      smpPaint();
    };
    function smpDesignMode(on) {
      smpDesign = on;
      if (on) {
        smpSel.clear();
        for (const i of view ? view.order : []) smpSel.add(i);
        smpTitle.value = view && !view.tour && view.title !== "Custom view" ? view.title : "";
      }
      smp.classList.toggle("design", on);
      smpHead.textContent = on ? "Design a view: pick slides or whole bands" : "Slide map";
      smpPaint();
      if (on) smpTiles[cur]?.focus({ preventScroll: true });
    }
    // start (or, with null, leave) a view from the map; a new tour opens its first slide
    function smpUse(v, restart = true) {
      smp.close();
      setView(v);
      go(!restart && vis(cur) ? cur : firstVis());
    }
    const smpTourBtn = (text, sub, pressed, fn) =>
      h("button", { class: "smp-tb", type: "button", "aria-pressed": String(pressed), title: `${text}: ${sub} (T to reach the tours, Tab, Enter)`, onclick: fn }, h("b", {}, text), h("span", {}, sub));
    function smpPaint() {
      smpTiles.forEach((t, i) => {
        t.classList.toggle("out", !smpDesign && !vis(i));
        const off = smpDesign && pat(i) >= 0 && !smpSel.has(pat(i));
        t.classList.toggle("off", off);
        if (off) t.setAttribute("aria-disabled", "true");
        else t.removeAttribute("aria-disabled");
        if (smpDesign) t.setAttribute("aria-pressed", String(smpSel.has(i)));
        else t.removeAttribute("aria-pressed");
      });
      smpBody.querySelectorAll(".smp-bx").forEach((x, b) => {
        const idx = bandIdx(b),
          k = idx.filter(i => smpSel.has(i)).length;
        x.checked = k === idx.length;
        x.indeterminate = k > 0 && k < idx.length;
      });
      const count = n => `${n} slide${n === 1 ? "" : "s"}`;
      smpTourList.replaceChildren(
        ...[
          smpTourBtn("All slides", count(slides.length), !view, () => smpUse(null, false)),
          ...Object.entries(TOURS).map(([name, t]) => {
            const set = resolveView(t);
            return set && smpTourBtn(t.title || name, t.summary ? `${count(set.size)} · ${t.summary}` : count(set.size), view?.tour === name, () => smpUse(viewFrom({ tour: name })));
          }),
          view && !view.tour ? smpTourBtn(view.title, `${count(view.set.size)} · custom view`, true, () => smpUse(view, false)) : null,
          h(
            "button",
            { class: "smp-tb design", type: "button", "aria-pressed": String(smpDesign), "aria-keyshortcuts": "D", title: "Design a view: pick slides or bands, share the link (D)", onclick: () => smpDesignMode(!smpDesign) },
            h("b", {}, "Design a view"),
            h("span", {}, "pick slides or bands, share the link")
          ),
        ].filter(Boolean)
      );
      if (!smpDesign) return;
      const spec = smpSpec(),
        first = slides.findIndex((s, i) => smpSel.has(i));
      smpCount.textContent = `${smpSel.size} of ${count(slides.length)}`;
      smpLink.value = smpSel.size ? `${location.href.split("#")[0]}#${slides[first].id}&view=${b64e(spec)}` : "";
      smpApply.disabled = smpCopy.disabled = !smpSel.size;
    }
    const smp = h(
      "dialog",
      { class: "smp", "aria-label": "Slide map" },
      h("div", { class: "smp-bar" }, smpHead, h("button", { class: "fz-x", type: "button", "aria-label": "Close slide map (Esc)", "aria-keyshortcuts": "Escape G", title: "Close slide map (Esc or G)", onclick: () => smp.close() }, "Esc")),
      smpBody,
      smpDBar
    );
    document.body.append(smp);
    let smpTiles = null;
    const smpBuild = () => {
      smpTiles = [];
      smpBody.replaceChildren(
        ...rows.map((r, b) => {
          const c = THEME[r.group].accent[0],
            band = slides.filter(s => s.band === slides[r.start].band),
            first = slides.indexOf(band[0]) + 1,
            last = slides.indexOf(band[band.length - 1]) + 1,
            all = band.map(s => {
              const n = slides.indexOf(s),
                t = h(
                  "button",
                  {
                    class: `smp-t${s.header ? " hd" : s.parent ? " sub" : ""}`,
                    type: "button",
                    "aria-label": `Go to slide ${n + 1}: ${label(s) || rowName(r.group)}`,
                    title: `Go to slide ${n + 1}: ${label(s) || rowName(r.group)} (arrows to move, Enter)`,
                    onclick: () => (smpDesign ? smpPick([n], !smpSel.has(n)) : (smp.close(), go(n))),
                  },
                  h("b", {}, String(n + 1).padStart(2, "0"), s.kids ? h("i", { title: `${s.kids.length} examples below` }, `↓${s.kids.length}`) : null),
                  h("span", {}, label(s) || rowName(r.group))
                );
              smpTiles[n] = t;
              return t;
            });
          // a column (pattern + its examples) takes its own grid line, the examples after the pattern tile
          const tiles = all.flatMap(t => {
            const s = slides[smpTiles.indexOf(t)];
            if (s.parent) return [];
            if (!s.kids) return [t];
            return [
              h(
                "div",
                { class: "smp-col", "data-n": s.kids.length + 1 },
                t,
                h(
                  "div",
                  { class: "smp-kids", role: "group", "aria-label": `${label(s)} examples` },
                  s.kids.map(k => smpTiles[slides.indexOf(k)])
                )
              ),
            ];
          });
          const id = `smp-b${b}`;
          return h(
            "section",
            { class: "smp-row", "--c": c, "aria-labelledby": id },
            h(
              "h3",
              { class: "smp-l", id, "data-n": r.start },
              h("input", {
                type: "checkbox",
                class: "smp-bx",
                "aria-label": `Include the whole ${rowName(r.group)} band`,
                onchange: e => smpPick(bandIdx(b), e.target.checked),
              }),
              rowName(r.group),
              " ",
              h("small", {}, first === last ? `#${first}` : `#${first}–${last}`)
            ),
            h("div", { class: "smp-tiles" }, tiles)
          );
        }),
        smpTours
      );
    };
    // nearest tile on the next visual line above or below (tiles wrap, so bands can span several lines)
    const smpVert = (from, down) => {
      const a = from.getBoundingClientRect(),
        cx = a.left + a.width / 2,
        cy = a.top + a.height / 2;
      let best = null,
        bd = Infinity;
      for (const t of smpTiles) {
        const b = t.getBoundingClientRect(),
          dy = b.top + b.height / 2 - cy;
        if (down ? dy <= 4 : dy >= -4) continue;
        const d = Math.abs(dy) * 100 + Math.abs(b.left + b.width / 2 - cx);
        if (d < bd) ((bd = d), (best = t));
      }
      return best;
    };
    // a column spans its pattern plus examples, capped at the grid's track count, so short columns share a line with other tiles
    const smpFit = () => {
      const grid = smpBody.querySelector(".smp-tiles");
      if (!grid || !smp.open) return;
      const cols = getComputedStyle(grid).gridTemplateColumns.split(" ").length;
      for (const c of smpBody.querySelectorAll(".smp-col")) c.style.setProperty("--gc", `span ${Math.min(cols, +c.dataset.n)}`);
    };
    new ResizeObserver(smpFit).observe(smpBody);
    // staggered entrance rippling out from the current slide, capped so far tiles do not lag
    const smpDelay = n => `${Math.min(600, Math.abs(n - cur) * 18)}ms`;
    function openMap() {
      if (smp.open) return;
      if (fz.open) fz.close();
      if (!smpTiles) smpBuild();
      smpTiles.forEach((t, i) => {
        t.classList.toggle("on", i === cur);
        if (i === cur) t.setAttribute("aria-current", "true");
        else t.removeAttribute("aria-current");
        t.style.setProperty("--d", smpDelay(i));
      });
      // band headers enter with their first tile; a column's dotted rule with its first example
      for (const l of smpBody.querySelectorAll(".smp-l")) l.style.setProperty("--d", smpDelay(+l.dataset.n));
      for (const k of smpBody.querySelectorAll(".smp-kids")) k.style.setProperty("--d", k.firstChild.style.getPropertyValue("--d"));
      smpPaint();
      smp.showModal();
      smpFit();
      const t = smpTiles[cur];
      t?.focus({ preventScroll: true });
      t?.scrollIntoView({ block: "center" });
    }
    smp.addEventListener("keydown", e => {
      /** @type {any} */
      const target = e.target;
      // typing a view title, or toggling a band checkbox, keeps its own keys
      if (target.matches?.("input") && e.key !== "Escape" && !(target.type === "checkbox" && e.key.startsWith("Arrow"))) return;
      // settle the entrance so Up/Down measure final tile positions
      for (const a of smp.getAnimations?.({ subtree: true }) || []) a.finish();
      const i = smpTiles.indexOf(document.activeElement);
      let to = null;
      if (e.key === "ArrowRight" || e.key === "ArrowLeft") to = smpTiles[Math.min(slides.length - 1, Math.max(0, (i < 0 ? cur : i) + (e.key === "ArrowRight" ? 1 : -1)))];
      else if (e.key === "ArrowDown" || e.key === "ArrowUp") to = smpVert(smpTiles[i < 0 ? cur : i], e.key === "ArrowDown");
      else if (e.key === "Home") to = smpTiles[0];
      else if (e.key === "End") to = smpTiles[slides.length - 1];
      else if (e.key === "Escape" && smpDesign) {
        e.preventDefault();
        smpDesignMode(false);
        return;
      } else if (/^[tdaclTDACL]$/.test(e.key) || (e.key === "Enter" && (e.ctrlKey || e.metaKey))) {
        // map shortcuts: T tours, D design mode; in design mode A select all, C clear, L copy link, Ctrl+Enter apply
        const k = e.key.toLowerCase();
        const btn =
          k === "t"
            ? smpTourList.querySelector('[aria-pressed="true"]') || smpTourList.firstElementChild
            : k === "d"
              ? smpTourList.querySelector(".design")
              : smpDesign && { a: smpDBar.children[1], c: smpDBar.children[2], l: smpCopy, enter: smpApply }[k];
        if (!btn) return;
        e.preventDefault();
        if (k === "t") (btn.focus({ preventScroll: true }), btn.scrollIntoView({ block: "nearest" }));
        else if (!btn.disabled) btn.click();
        return;
      } else if (e.key === "Escape" || e.key === "g" || e.key === "G") {
        e.preventDefault();
        smp.close();
        return;
      } else return;
      e.preventDefault();
      if (to) (to.focus({ preventScroll: true }), to.scrollIntoView({ block: "nearest" }));
    });
    smp.addEventListener("click", e => e.target === smp && smp.close());
    smp.addEventListener("close", () => {
      if (smpDesign) smpDesignMode(false);
      /** @type {any} */
      const active = document.activeElement;
      active?.blur?.();
    });
    for (const t of ["touchstart", "touchend"]) smp.addEventListener(t, e => e.stopPropagation(), { passive: true });

    // ---------- animated background ----------
    const ctx = canvas.getContext("2d");
    const hex = c => [1, 3, 5].map(i => parseInt(c.slice(i, i + 2), 16));
    // Each band's THEME sets the base fill, glow palette, grid colour, angle and cell size; values ease between bands.
    const bgOf = th => [...th.base, ...th.grid, th.cell];
    let bgv = bgOf(THEME[slides[0].group]);
    let colors = THEME[slides[0].group].pal.map(hex),
      gAng = THEME[slides[0].group].ang;
    const blobs = [0, 1, 2, 3].map(i => ({ a: Math.random() * 6.28, b: Math.random() * 6.28, s: 0.00007 + i * 0.00003 }));
    const rmq = matchMedia("(prefers-reduced-motion: reduce)");
    let REDUCED = rmq.matches;
    rmq.addEventListener?.("change", e => {
      REDUCED = e.matches;
    });
    // The base fill and soft glows are drawn at a quarter of the CSS size and scaled up by the browser (no visible
    // difference, a fraction of the fill cost); the sharp grid repaints only when its size, angle or colour changes.
    const BG_SCALE = 0.25;
    let gridKey = "";

    function frame(t) {
      const dpr = Math.min(devicePixelRatio || 1, 2);
      const cw = innerWidth,
        ch = innerHeight;
      const bw = Math.round(cw * dpr),
        bh = Math.round(ch * dpr);
      const sw = Math.max(1, Math.round(cw * BG_SCALE)),
        sh = Math.max(1, Math.round(ch * BG_SCALE));
      if (canvas.width !== sw || canvas.height !== sh) {
        canvas.width = sw;
        canvas.height = sh;
      }
      ctx.setTransform(sw / cw, 0, 0, sh / ch, 0, 0);

      const cs = slides[bgN ?? cur] || slides[0],
        th = THEME[cs.group],
        target = th.pal.map(hex),
        bgt = bgOf(th);
      // light <-> dark swaps the base quickly so text never sits on the wrong background for long
      bgv = REDUCED ? bgt : bgv.map((v, i) => v + (bgt[i] - v) * (i < 3 && Math.abs(bgt[0] - bgv[0]) > 90 ? 0.07 : 0.04));
      colors = REDUCED ? target : colors.map((c, i) => c.map((v, k) => v + (target[i][k] - v) * 0.03));

      ctx.globalCompositeOperation = "source-over";
      ctx.fillStyle = `rgb(${bgv[0] | 0},${bgv[1] | 0},${bgv[2] | 0})`;
      ctx.fillRect(0, 0, cw, ch);
      ctx.globalCompositeOperation = "lighter";
      const R = Math.max(cw, ch) * 0.55,
        bt = REDUCED ? 0 : t;
      blobs.forEach((b, i) => {
        const x = cw * (0.5 + 0.3 * Math.sin(bt * b.s * 3 + b.a));
        const y = ch * (0.5 + 0.26 * Math.cos(bt * b.s * 2.5 + b.b));
        const [r, g, bl] = colors[i];
        const grd = ctx.createRadialGradient(x, y, 0, x, y, R);
        grd.addColorStop(0, `rgba(${r | 0},${g | 0},${bl | 0},0.1)`);
        grd.addColorStop(1, "rgba(0,0,0,0)");
        ctx.fillStyle = grd;
        ctx.fillRect(0, 0, cw, ch);
      });

      // Blueprint grid: `cell` px minor lines, a major line every 5, anchored centre-top, turned (around the screen centre) to the band's `ang`.
      const gTarget = th.ang;
      gAng = REDUCED ? gTarget : gAng + (gTarget - gAng) * 0.035;
      if (Math.abs(gTarget - gAng) < 1e-4) gAng = gTarget;
      const k = Math.min(cw / W, ch / H),
        minor = bgv[8] * k,
        D = Math.hypot(cw, ch) / 2 + minor;
      const gc = `${bgv[3] | 0},${bgv[4] | 0},${bgv[5] | 0}`,
        lmin = `rgba(${gc},${bgv[6]})`,
        lmaj = `rgba(${gc},${bgv[7]})`;
      // eased values settle asymptotically, so the key rounds them: the grid stops repainting once a band change settles
      const key = [bw, bh, gAng, gc, bgv[6].toFixed(4), bgv[7].toFixed(4), minor.toFixed(2)].join();
      if (key !== gridKey) {
        gridKey = key;
        if (gridc.width !== bw || gridc.height !== bh) {
          gridc.width = bw;
          gridc.height = bh;
        }
        const g = gridc.getContext("2d");
        g.setTransform(dpr, 0, 0, dpr, 0, 0);
        g.clearRect(0, 0, cw, ch);
        g.translate(cw / 2, ch / 2);
        g.rotate(gAng);
        const line = i => (((i % 5) + 5) % 5 ? lmin : lmaj);
        const snap = gAng ? v => v : v => Math.round(v);
        for (let i = -Math.ceil(D / minor); i * minor <= D; i++) {
          g.fillStyle = line(i);
          g.fillRect(snap(i * minor), -D, 1, 2 * D);
        }
        const oy = -ch / 2; // horizontal lines stay anchored to the top edge, as before
        for (let j = Math.floor((-D - oy) / minor); j * minor + oy <= D; j++) {
          g.fillStyle = line(j);
          g.fillRect(-D, snap(j * minor + oy), 2 * D, 1);
        }
      }

      slides[cur]?.tick?.(t);
      requestAnimationFrame(frame);
    }

    followHash();
    requestAnimationFrame(frame);
    // tour(name) starts a config.tours entry at its first slide; tour(null) shows every slide again
    const tour = name => {
      const v = name == null ? null : viewFrom({ tour: name });
      if (name != null && !v) throw new Error(`CracktroDeck: unknown or empty tour "${name}"`);
      setView(v);
      if (v) go(firstVis());
    };
    return { slides, rows, go, next, prev, band, current: () => cur, tour, tours: () => Object.keys(TOURS), kiosk: on => setKiosk(on ?? !kiosk) };
  }

  window.CracktroDeck = { h, letters, resetIndex, create };
})();
