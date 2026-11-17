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
  const SILENT = { cut() {}, techno() {}, slide() {}, touch() {}, step() {}, toggle: () => true, muted: true };

  // ---------- runtime styles ----------
  const runtimeCss = (W, H) => `
  :root { --mono: ui-monospace, Menlo, monospace; }
  *, *::before, *::after { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; overflow: hidden; background: #0d1117; color: #f0f6fc;
    font-family: -apple-system, "Segoe UI", sans-serif; -webkit-font-smoothing: antialiased; }
  canvas.bg { position: fixed; inset: 0; width: 100vw; height: 100vh; }
  .stage { position: absolute; left: 50%; top: 50%; width: ${W}px; height: ${H}px; transform-origin: 0 0; }
  .slide { position: absolute; inset: 0; visibility: hidden; pointer-events: none; }
  /* siblings in a group: reveal-style horizontal push; --dx is +100% going forward, -100% going back, 0 on first load.
     crossing groups: vertical push, up going forward and down going back (same duration and easing) */
  .slide.active { visibility: visible; pointer-events: auto; animation: slide-in .8s cubic-bezier(.26,.86,.44,.985) both; }
  .slide.leaving { visibility: visible; animation: slide-out .8s cubic-bezier(.26,.86,.44,.985) both; }
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
    background-image: linear-gradient(90deg, #ab7df8, #4493f8, #3fb950, #4493f8, #ab7df8);
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
  body.light .snd, body.light .fzb, body.light .smb, body.light .arrows button { border-color: #9ea7b3; background: #e6eaefcc; color: #424a53; }
  body.light .snd:hover, body.light .fzb:hover, body.light .smb:hover, body.light .arrows button:hover { color: #1f2328; border-color: #818b98; }
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
    background: linear-gradient(90deg, #ab7df8, #4493f8, #3fb950); transition: transform .8s cubic-bezier(.7,0,.2,1); z-index: 5; }
  /* dot map: one row per band, header dot outlined; the band you are in is bright, others dim until hovered */
  .dots { position: fixed; left: 28px; bottom: 22px; display: flex; flex-direction: column; gap: 5px; z-index: 5; opacity: .25; transition: opacity .3s; }
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
  .vnb { position: fixed; right: 144px; bottom: 10px; height: 34px; padding: 0 10px 0 6px; display: none; align-items: center; gap: 4px; border: 1px solid #3d444d; border-radius: 8px;
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
  .arrows { position: fixed; left: 12px; bottom: 10px; display: none; gap: 6px; z-index: 5; touch-action: manipulation; -webkit-user-select: none; user-select: none; }
  .arrows button { width: 34px; height: 34px; padding: 7px; border: 1px solid #3d444d; border-radius: 8px; background: #151b23cc;
    color: #9198a1; cursor: pointer; transition: color .2s, border-color .2s; touch-action: manipulation; -webkit-tap-highlight-color: transparent; }
  .arrows button:hover { color: #f0f6fc; border-color: #9198a1; }
  .arrows button:disabled { opacity: .35; cursor: default; }
  .arrows svg { width: 100%; height: 100%; display: block; }
  body.compact-nav .dots { display: none; }
  body.compact-nav .arrows { display: flex; }
  .snd, .fzb, .smb { position: fixed; right: 24px; bottom: 10px; width: 34px; height: 34px; padding: 6px; border: 1px solid #3d444d; border-radius: 8px;
    background: #151b23cc; color: #9198a1; cursor: pointer; z-index: 5; transition: color .2s, border-color .2s, background .2s; }
  .snd:hover, .fzb:hover, .smb:hover { color: #f0f6fc; border-color: #9198a1; }
  .snd svg, .fzb svg, .smb svg { width: 100%; height: 100%; display: block; }
  .fzb { right: 64px; }
  .smb { right: 104px; }
  .snd .waves { transition: opacity .2s; }
  .snd .x { opacity: 0; transition: opacity .2s; }
  .snd.off .waves { opacity: 0; }
  .snd.off .x { opacity: 1; }
  .snd.flash { color: #f0f6fc; border-color: #f0f6fc; }
  .notes { position: fixed; left: 24px; right: 24px; bottom: 50px; max-width: 900px; margin: 0 auto; padding: 18px 22px;
    background: #151b23f0; border: 1px solid #3d444d; border-radius: 12px; color: #f0f6fc; font: 400 18px/1.45 "Mona Sans"; z-index: 6; display: none; }
  .notes.on { display: block; }
  body:not(.compact-nav) .notes { left: 330px; }

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
  .fz-s { font-size: 15px; line-height: 1.4; color: #c9d1d9; overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
  .fz mark { background: #ab7df859; color: inherit; border-radius: 3px; }
  body.light .fz { background: #f6f8faf5; color: #1f2328; }
  body.light .fz-in, body.light .fz-x { background: #ffffff; border-color: #9ea7b3; color: #1f2328; }
  body.light .fz-list li:hover { background: #eaeef2; }
  body.light .fz-list li[aria-selected="true"] { background: #ffffff; border-color: #8250df; }
  body.light .fz-n { color: #8250df; }
  body.light .fz-s { color: #424a53; }
  body.light .fz-st, body.light .fz-b { color: #59636e; }
  body.light .fz mark { background: #8250df33; }

  /* --- slide map (full-screen overview: one row per band, one tile per slide) --- */
  .smp { position: fixed; inset: 0; width: 100%; height: 100%; max-width: none; max-height: none; margin: 0; padding: 0; border: 0;
    background: #0d1117f7; -webkit-backdrop-filter: blur(8px); backdrop-filter: blur(8px); color: #f0f6fc; font-family: "Mona Sans", -apple-system, "Segoe UI", sans-serif; z-index: 20; }
  .smp[open] { display: flex; flex-direction: column; }
  .smp::backdrop { background: transparent; }
  .smp-bar { display: flex; align-items: center; gap: 12px; padding: 16px 24px 8px; }
  .smp-h { flex: 1; margin: 0; font: 600 14px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: #9198a1; }
  .smp-body { flex: 1; min-height: 0; overflow-y: auto; padding: 8px 24px 24px; display: flex; flex-direction: column; gap: 14px; }
  .smp-row { display: grid; grid-template-columns: 170px 1fr; gap: 12px; align-items: start; }
  .smp-l { font: 600 13px/1.3 var(--mono); letter-spacing: .06em; text-transform: uppercase; color: var(--c); padding-top: 10px; }
  .smp-tiles { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 6px; }
  .smp-col { flex: 0 0 100%; display: flex; align-items: flex-start; gap: 6px; }
  .smp-kids { flex: 1; display: flex; flex-wrap: wrap; gap: 6px; padding-left: 10px; border-left: 2px dotted var(--c); }
  .smp-t { width: 168px; min-height: 58px; display: flex; flex-direction: column; gap: 4px; text-align: left; padding: 8px 10px; border: 1px solid #3d444d;
    border-left: 3px solid var(--c); border-radius: 8px; background: #151b23; color: inherit; cursor: pointer; font: 500 13px/1.3 "Mona Sans", sans-serif; }
  .smp-t b { display: flex; font: 600 11px/1 var(--mono); color: #9198a1; letter-spacing: .06em; }
  .smp-t b i { margin-left: auto; font-style: normal; color: var(--c); }
  .smp-t span { overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
  .smp-t.hd { background: transparent; border-style: dashed; border-left-style: solid; }
  .smp-t.sub { width: 146px; min-height: 44px; font-size: 12px; border-left-style: dotted; opacity: .85; }
  .smp-t:hover { background: #1f2630; border-color: #9198a1; border-left-color: var(--c); }
  .smp-t.on { box-shadow: 0 0 0 2px var(--c); }
  .smp-t:focus-visible { outline: 3px solid #ab7df8; outline-offset: 2px; }
  @media (max-width: 640px) { .smp-row { grid-template-columns: 1fr; gap: 6px; } .smp-l { padding-top: 0; } .smp-t { width: calc(50% - 3px); } .smp-col { flex-direction: column; } .smp-col > .smp-t { width: calc(50% - 3px); } .smp-kids { width: 100%; box-sizing: border-box; } .smp-kids .smp-t.sub { width: calc(50% - 3px); } }
  body.light .smp { background: #f6f8faf5; color: #1f2328; }
  body.light .smp-t { background: #ffffff; border-color: #9ea7b3; border-left-color: var(--c); }
  body.light .smp-t.hd { background: transparent; }
  body.light .smp-t:hover { background: #eaeef2; }
  body.light .smp-h, body.light .smp-t b { color: #59636e; }
  body.light .smp-l { color: #1f2328; }
  @media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation-duration: .001s !important; animation-iteration-count: 1 !important; } }
  `;

  // ---------- create ----------
  // config: { slides (array, or factory receiving services), themes, bands, solo, prototypes, last, sound, css, width, height }.
  // Returns a controller: { slides, rows, go, next, prev, band, current }.
  function create(config = {}) {
    const { width: W = 1920, height: H = 1080, css = "", bands: BANDS = {}, solo: SOLO = {}, prototypes: PROTO = {}, last = null } = config;
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
    const rows = [];
    for (const s of slides) {
      const row = rows.find(r => r.group === s.group);
      if (row) row.items.push(s);
      else rows.push({ group: s.group, items: [s] });
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
          notes: `Part ${part} of ${parts.length}: ${title}. ${sub}. This band covers ${tops.map(s => label(s) + (s.kids ? ` (with ${s.kids.length} example${s.kids.length > 1 ? "s" : ""} below)` : "")).join(", ")}.`,
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
                  { "--i": i, "data-go": n - 1 },
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
    const arrowBtn = (label, key, d, fn) => {
      const b = h("button", {
        "aria-label": label,
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
    const bandPrevBtn = arrowBtn("Previous band", "↑", "M6 5v14M18 6l-7 6 7 6", () => band(-1));
    const prevBtn = arrowBtn("Previous slide", "←", "M15 5l-7 7 7 7", () => prev());
    const nextBtn = arrowBtn("Next slide", "→", "M9 5l7 7-7 7", () => next());
    const bandNextBtn = arrowBtn("Next band", "↓", "M18 5v14M6 6l7 6-7 6", () => band(1));
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
      "aria-haspopup": "dialog",
      title: "Slide map (G)",
      onclick: e => {
        e.stopPropagation();
        openMap();
      },
    });
    smBtn.innerHTML =
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="4" width="5" height="4" rx="1"/><rect x="10" y="4" width="5" height="4" rx="1"/><rect x="16.5" y="4" width="4" height="4" rx="1"/><rect x="3.5" y="10" width="5" height="4" rx="1"/><rect x="10" y="10" width="5" height="4" rx="1"/><rect x="3.5" y="16" width="5" height="4" rx="1"/><rect x="10" y="16" width="5" height="4" rx="1"/><rect x="16.5" y="16" width="4" height="4" rx="1"/></svg>';
    const notes = h("div", { class: "notes" });
    const wash = h("div", { class: "wash" });
    const gridc = h("canvas", { class: "gridc" });
    document.body.append(canvas, wash, gridc, stage, progress, dots, arrows, vnb, smBtn, fzBtn, ...(config.sound ? [snd] : []), notes);
    snd.classList.toggle("off", sfx.muted);

    // Dot map: left offset + the longest band of 8px dots with 5px gaps (example dots are 4px with 3px gaps) + 16px for the wide active dot; map/search/sound need ~150px on the right.
    const dotsWidth = 28 + Math.max(...rows.map(r => r.items.reduce((w, s) => w + (s.parent ? 7 : 13), r.header ? 13 : 0))) + 16 + 400;
    const fit = () => {
      const s = Math.min(innerWidth / W, innerHeight / H);
      stage.style.transform = `scale(${s}) translate(-50%, -50%)`;
      document.body.classList.toggle("compact-nav", dotsWidth > innerWidth - 150);
    };
    addEventListener("resize", fit);
    fit();

    function toggleSound() {
      if (!config.sound) return;
      snd.classList.toggle("off", sfx.toggle());
      snd.classList.add("flash");
      setTimeout(() => snd.classList.remove("flash"), 400);
    }

    // ---------- navigation ----------
    let cur = -1,
      redT = null,
      bgN = null; // slide whose THEME the background shows while a staged light/dark swap is pending
    // entering a "red" slide forward: the current slide first fades to red, then the red slide enters under the wash
    function go(n) {
      n = Math.max(0, Math.min(slides.length - 1, n));
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
      progress.style.transform = `scaleX(${(n + 1) / slides.length})`;
      dotBtns.forEach((d, i) => d.classList.toggle("on", i === n));
      [...dots.children].forEach((r, i) => r.classList.toggle("on", i === slides[n].band));
      prevBtn.disabled = n === 0;
      nextBtn.disabled = nextTop(n) == null;
      bandPrevBtn.disabled = slides[n].band === 0;
      bandNextBtn.disabled = slides[n].band === rows.length - 1;
      paintColumn(n);
      notes.textContent = slides[n].notes || "";
      history.replaceState(null, "", `#${n + 1}`);
    }
    // Left/right walk the main line of slides (column heads, never their examples) and wrap across bands;
    // up/down walk a column, and past its ends (or off any column) jump to the header of the band above/below.
    const top = n => (slides[n].parent ? slides.indexOf(slides[n].parent) : n);
    const nextTop = n => {
      for (let i = top(n) + 1; i < slides.length; i++) if (!slides[i].parent) return i;
      return null;
    };
    const prevTop = n => {
      for (let i = top(n) - 1; i >= 0; i--) if (!slides[i].parent) return i;
      return null;
    };
    const band = d => {
      const b = slides[cur].band + d;
      if (b >= 0 && b < rows.length) go(rows[b].start);
    };
    const vstep = d => {
      const col = colOf(slides[cur]);
      if (!col) return band(d);
      const list = [col, ...col.kids],
        i = list.indexOf(slides[cur]) + d;
      if (i >= 0 && i < list.length) go(slides.indexOf(list[i]));
      else band(d);
    };
    function paintColumn(n) {
      const col = colOf(slides[n]);
      vnb.classList.toggle("on", !!col);
      if (!col) return (vnb._nav = null);
      const N = col.kids.length,
        at = [col, ...col.kids].indexOf(slides[n]),
        back = at === N,
        label = at ? (back ? "Back to the pattern" : `Next example (${at + 1} of ${N})`) : `${N} example${N > 1 ? "s" : ""} below`;
      vnb.classList.toggle("head", at === 0);
      vnb.setAttribute("aria-label", label);
      vnb.title = `${label} (${back ? "↑" : "↓"})`;
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

    addEventListener("keydown", e => {
      if (fz.open || smp.open) return; // the search and map dialogs handle their own keys
      if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        openSearch();
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === "/" && !e.target.closest?.("input, textarea, select, [contenteditable]")) {
        e.preventDefault();
        openSearch();
        return;
      }
      if ((e.key === "Enter" || e.key === " ") && e.target.closest?.("a, button, input, textarea, select, [contenteditable]")) return;
      if (slides[cur].onKey?.(e)) {
        sfx.touch();
        e.preventDefault();
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
        vstep(e.key === "ArrowDown" ? 1 : -1);
      } else if (e.key === "Home") go(0);
      else if (e.key === "End") go(slides.length - 1);
      else if (e.key === "f" || e.key === "F") document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen?.();
      else if (e.key === "n" || e.key === "N") notes.classList.toggle("on");
      else if (e.key === "m" || e.key === "M") toggleSound();
      else if (e.key === "g" || e.key === "G") openMap();
    });
    addEventListener("click", e => {
      if (fz.open || smp.open || e.target.closest(".fz, .smp, a, button")) return;
      const tile = e.target.closest("[data-go]");
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
    addEventListener("hashchange", () => go((parseInt(location.hash.slice(1), 10) || 1) - 1));

    // ---------- search ----------
    // "/" (or Ctrl/Cmd+K, or the dock's search button) opens a full-screen dialog that fuzzy-searches every slide.
    // The index is precomputed once: each slide's title, its slides.md summary (the JSON-LD hasPart description the
    // docs build injects; absent on a plain static preview) and its body text plus speaker notes.
    // Matching is bounds-based: each query term must occur, letters in order, inside one window of a field, and the
    // window is bounded (at most one and a half times the term), so letters scattered through long notes never match.
    // The tightest window wins: forward from each occurrence of the first letter to the earliest end, then backward to
    // the latest start. Its bounds score the match (term length / window width, word-start and field bonuses) and mark it.
    const FZ_FIELDS = [
      ["title", 3],
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
        const part = parts.find(p => p.position === i + 1),
          title = headingText(s) || part?.name || `Slide ${i + 1}`,
          summary = fzNorm(part?.description || ""),
          body = fzNorm(`${fzText(s.el)} ${s.notes || ""}`);
        return { n: i, band: BANDS[s.group]?.[0] || "", title, summary, body, notes: fzNorm(s.notes || ""), lc: { title: title.toLowerCase(), summary: summary.toLowerCase(), body: body.toLowerCase() } };
      });
    };
    // Tightest bounded window [start, end) of `text` holding the characters of `term` in order, or null.
    const fzBounds = (text, term) => {
      const n = term.length,
        max = n + Math.ceil(n / 2);
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
        const marks = { title: [], summary: [], body: [] };
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
      h("div", { class: "fz-box" }, h("div", { class: "fz-bar" }, fzIn, h("button", { class: "fz-x", type: "button", "aria-label": "Close search (Esc)", onclick: () => fz.close() }, "Esc")), fzSt, fzList)
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
            h("div", { class: "fz-t" }, ...fzMark(hit.r.title, hit.marks.title || []), hit.r.band && hit.r.band !== hit.r.title ? h("span", { class: "fz-b" }, hit.r.band) : null),
            h("div", { class: "fz-s" }, fzLine(hit))
          )
        )
      );
      fzSt.textContent = fzIn.value.trim()
        ? fzHits.length
          ? `${fzHits.length} matching slide${fzHits.length === 1 ? "" : "s"} · ↑↓ to choose, Enter to open`
          : "No slides match"
        : `${fzHits.length} slides · type to search titles, summaries and slide text`;
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
    fz.addEventListener("close", () => document.activeElement?.blur?.());
    for (const t of ["touchstart", "touchend"]) fz.addEventListener(t, e => e.stopPropagation(), { passive: true });

    // ---------- slide map ----------
    // G (or the dock's map button) opens a full-screen overview: one row per band, one tile per slide, the current one ringed.
    // Click or Enter opens a slide; Left/Right step in slide order, Up/Down move to the nearest tile on the line above/below.
    // A column (a pattern slide with `kids`) gets its own line: the pattern tile, then its examples in a horizontal row behind a dotted rule.
    const smpBody = h("div", { class: "smp-body" });
    const smp = h(
      "dialog",
      { class: "smp", "aria-label": "Slide map" },
      h("div", { class: "smp-bar" }, h("h2", { class: "smp-h" }, "Slide map"), h("button", { class: "fz-x", type: "button", "aria-label": "Close slide map (Esc)", onclick: () => smp.close() }, "Esc")),
      smpBody
    );
    document.body.append(smp);
    let smpTiles = null;
    const smpBuild = () => {
      smpTiles = [];
      smpBody.replaceChildren(
        ...rows.map(r => {
          const c = THEME[r.group].accent[0],
            all = slides
              .filter(s => s.band === slides[r.start].band)
              .map(s => {
                const n = slides.indexOf(s),
                  t = h(
                    "button",
                    { class: `smp-t${s.header ? " hd" : s.parent ? " sub" : ""}`, type: "button", "aria-label": `Go to slide ${n + 1}: ${label(s) || rowName(r.group)}`, onclick: () => (smp.close(), go(n)) },
                    h("b", {}, String(n + 1).padStart(2, "0"), s.kids ? h("i", { title: `${s.kids.length} examples below` }, `↓${s.kids.length}`) : null),
                    h("span", {}, label(s) || rowName(r.group))
                  );
                smpTiles[n] = t;
                return t;
              });
          // a column (pattern + its examples) takes its own line, the examples in a row after the pattern tile
          const tiles = all.flatMap(t => {
            const s = slides[smpTiles.indexOf(t)];
            if (s.parent) return [];
            if (!s.kids) return [t];
            return [
              h(
                "div",
                { class: "smp-col" },
                t,
                h(
                  "div",
                  { class: "smp-kids", role: "group", "aria-label": `${label(s)} examples` },
                  s.kids.map(k => smpTiles[slides.indexOf(k)])
                )
              ),
            ];
          });
          return h("section", { class: "smp-row", "--c": c }, h("div", { class: "smp-l" }, rowName(r.group)), h("div", { class: "smp-tiles" }, tiles));
        })
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
    function openMap() {
      if (smp.open) return;
      if (fz.open) fz.close();
      if (!smpTiles) smpBuild();
      smpTiles.forEach((t, i) => {
        t.classList.toggle("on", i === cur);
        if (i === cur) t.setAttribute("aria-current", "true");
        else t.removeAttribute("aria-current");
      });
      smp.showModal();
      const t = smpTiles[cur];
      t?.focus({ preventScroll: true });
      t?.scrollIntoView({ block: "center" });
    }
    smp.addEventListener("keydown", e => {
      const i = smpTiles.indexOf(document.activeElement);
      let to = null;
      if (e.key === "ArrowRight" || e.key === "ArrowLeft") to = smpTiles[Math.min(slides.length - 1, Math.max(0, (i < 0 ? cur : i) + (e.key === "ArrowRight" ? 1 : -1)))];
      else if (e.key === "ArrowDown" || e.key === "ArrowUp") to = smpVert(smpTiles[i < 0 ? cur : i], e.key === "ArrowDown");
      else if (e.key === "Home") to = smpTiles[0];
      else if (e.key === "End") to = smpTiles[slides.length - 1];
      else if (e.key === "Escape" || e.key === "g" || e.key === "G") {
        e.preventDefault();
        smp.close();
        return;
      } else return;
      e.preventDefault();
      if (to) (to.focus({ preventScroll: true }), to.scrollIntoView({ block: "nearest" }));
    });
    smp.addEventListener("click", e => e.target === smp && smp.close());
    smp.addEventListener("close", () => document.activeElement?.blur?.());
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

    function frame(t) {
      const dpr = Math.min(devicePixelRatio || 1, 2);
      const cw = innerWidth,
        ch = innerHeight;
      const bw = Math.round(cw * dpr),
        bh = Math.round(ch * dpr);
      if (canvas.width !== bw || canvas.height !== bh) {
        canvas.width = bw;
        canvas.height = bh;
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

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
      if (gridc.width !== bw || gridc.height !== bh) {
        gridc.width = bw;
        gridc.height = bh;
      }
      const g = gridc.getContext("2d");
      g.setTransform(dpr, 0, 0, dpr, 0, 0);
      g.clearRect(0, 0, cw, ch);
      const gTarget = th.ang;
      gAng = REDUCED ? gTarget : gAng + (gTarget - gAng) * 0.035;
      if (Math.abs(gTarget - gAng) < 1e-4) gAng = gTarget;
      const k = Math.min(cw / W, ch / H),
        minor = bgv[8] * k,
        D = Math.hypot(cw, ch) / 2 + minor;
      g.translate(cw / 2, ch / 2);
      g.rotate(gAng);
      const gc = `${bgv[3] | 0},${bgv[4] | 0},${bgv[5] | 0}`,
        lmin = `rgba(${gc},${bgv[6]})`,
        lmaj = `rgba(${gc},${bgv[7]})`;
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

      slides[cur]?.tick?.(t);
      requestAnimationFrame(frame);
    }

    go((parseInt(location.hash.slice(1), 10) || 1) - 1);
    requestAnimationFrame(frame);
    return { slides, rows, go, next, prev, band, current: () => cur };
  }

  window.CracktroDeck = { h, letters, resetIndex, create };
})();
