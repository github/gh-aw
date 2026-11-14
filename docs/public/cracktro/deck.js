// GitHub Agentic Workflows — a zero-dependency, all-JavaScript slide deck.
// Open index.html directly in a browser. Keys: ← → space, Home/End, F fullscreen, N notes.
(() => {
  const W = 1920,
    H = 1080;
  const DOCS_URL = "https://github.github.com/gh-aw/introduction/overview/";

  // ---------- styles ----------
  const css = `
  @font-face {
    font-family: "Mona Sans";
    src: url("../fonts/MonaSansVF.woff2") format("woff2");
    font-weight: 200 900; font-stretch: 100% 125%; font-display: block;
  }
  @font-face {
    font-family: "Monaspace Neon";
    src: url("fonts/MonaspaceNeonVF.woff") format("woff");
    font-weight: 200 800; font-display: block;
  }
  :root { --mono: "Monaspace Neon", ui-monospace, Menlo, monospace; }
  *, *::before, *::after { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; overflow: hidden; background: #0d1117; color: #f0f6fc;
    font-family: "Mona Sans", -apple-system, "Segoe UI", sans-serif; -webkit-font-smoothing: antialiased; }
  canvas.bg { position: fixed; inset: 0; width: 100vw; height: 100vh; }
  .stage { position: absolute; left: 50%; top: 50%; width: ${W}px; height: ${H}px; transform-origin: 0 0; }
  .slide { position: absolute; inset: 0; visibility: hidden; pointer-events: none; }
  /* reveal-style horizontal push: --dx is +100% going forward, -100% going back, 0 on first load */
  .slide.active { visibility: visible; pointer-events: auto; animation: slide-in .8s cubic-bezier(.26,.86,.44,.985) both; }
  .slide.leaving { visibility: visible; animation: slide-out .8s cubic-bezier(.26,.86,.44,.985) both; }
  @keyframes slide-in { from { transform: translateX(var(--dx, 100%)); } to { transform: none; } }
  @keyframes slide-out { to { transform: translateX(calc(-1 * var(--dx, 100%))); } }
  .slide.active.up-in { animation: up-in 1s cubic-bezier(.7,0,.2,1) both; }
  .slide.leaving.up-out { animation: up-out 1s cubic-bezier(.7,0,.2,1) both; }
  .slide.active.down-in { animation: down-in 1s cubic-bezier(.7,0,.2,1) both; }
  .slide.leaving.down-out { animation: down-out 1s cubic-bezier(.7,0,.2,1) both; }
  .slide.active.left-in { animation: left-in 1s cubic-bezier(.7,0,.2,1) both; }
  @keyframes left-in { from { transform: translateX(100%); } to { transform: none; } }
  /* full-screen red wash for "Safety and control": fades over the previous slide, then sits behind the stage */
  .wash { position: fixed; inset: 0; pointer-events: none; opacity: 0; transition: opacity .9s ease-in-out;
    background: radial-gradient(ellipse 80% 75% at 50% 55%, #6e0b12 0%, #3a0408 55%, #140103 100%); }
  /* blueprint grid on its own layer above the wash, so it can rotate to 45° as the red fades in */
  canvas.gridc { position: fixed; inset: 0; width: 100vw; height: 100vh; pointer-events: none; }
  .wash.cover + canvas.gridc { z-index: 4; }
  .wash.on { opacity: 1; }
  .wash::after { content: ""; position: absolute; inset: 0;
    background: radial-gradient(ellipse 55% 60% at 50% 58%, transparent 30%, #2a0306aa 70%, #050000f0 100%); }
  .wash.on::after { animation: omen 7s ease-in-out infinite; }
  .wash.cover { z-index: 4; }
  /* green wash for "Safe outputs" and "Private enclaves": the inverse of the red one, with the grid tilted the other way */
  .wash.green { background: radial-gradient(ellipse 80% 75% at 50% 55%, #0b4a2a 0%, #04261a 55%, #01100a 100%); }
  .wash.green::after { background: radial-gradient(ellipse 55% 60% at 50% 58%, transparent 30%, #031f13aa 70%, #000503f0 100%); }
  @keyframes up-in { from { transform: translateY(100%); } to { transform: none; } }
  @keyframes up-out { to { transform: translateY(-100%); } }
  @keyframes down-in { from { transform: translateY(-100%); } to { transform: none; } }
  @keyframes down-out { to { transform: translateY(100%); } }

  .kicker { font: 500 24px/1 var(--mono); letter-spacing: .12em; text-transform: uppercase; color: #9198a1; font-stretch: 100%; }
  .active .kicker { animation: rise .9s cubic-bezier(.2,.9,.2,1) both .1s; }

  /* --- title --- */
  .title-wrap { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 36px; }
  .title { margin: 0; text-align: center; font-size: 210px; line-height: .9; letter-spacing: -.01em; }
  .title .line { display: block; white-space: nowrap; }
  .ch { display: inline-block; font-variation-settings: "wght" 700, "wdth" 100; }
  .l-grad .ch { color: transparent; -webkit-background-clip: text; background-clip: text;
    background-image: linear-gradient(90deg, #ab7df8, #4493f8, #3fb950, #4493f8, #ab7df8);
    background-size: 1800px 100%; background-position: var(--bx) 0; }
  @keyframes settle { from { opacity: 0; transform: translateY(.25em); filter: blur(4px); } to { opacity: 1; transform: none; filter: none; } }
  @keyframes breathe { 0%, 100% { font-variation-settings: "wght" 700, "wdth" 100; } 50% { font-variation-settings: "wght" 700, "wdth" 100; } }
  .l-out .ch { color: transparent; -webkit-text-stroke: 3px #f0f6fc; }
  /* Hero: GitHub brand "AI green" (brand.github.com Green 1–5) */
  .title .l-grad .ch { background-image: linear-gradient(90deg, #0fbf3e, #5fed83, #bfffd1, #8cf2a6, #0fbf3e); filter: drop-shadow(0 0 28px #5fed8355); padding-bottom: .14em; margin-bottom: -.14em; }
  .title .l-out .ch { -webkit-text-stroke-color: #5fed83; }
  @keyframes rise { from { opacity: 0; transform: translateY(.3em) scale(.94); filter: blur(6px); } to { opacity: 1; transform: none; filter: none; } }
  @keyframes flex { 0%, 100% { font-variation-settings: "wght" 700, "wdth" 100; } 50% { font-variation-settings: "wght" 700, "wdth" 100; } }
  @keyframes hue { from { background-position: var(--bx) 0; } to { background-position: calc(var(--bx) - 1800px) 0; } }

  .marquee { position: absolute; left: 0; right: 0; bottom: 70px; overflow: hidden; white-space: nowrap;
    mask-image: linear-gradient(90deg, transparent, #000 15%, #000 85%, transparent); }
  .marquee .track { display: inline-block; font: 500 30px/1 "Mona Sans"; letter-spacing: .24em; text-transform: uppercase;
    color: #ffffff60; animation: scroll 90s linear infinite; font-stretch: 100%; }
  .marquee .track b { color: #3fb950; font-weight: 400; margin: 0 .8em; }
  @keyframes scroll { to { transform: translateX(-50%); } }

  /* --- slide headings --- */
  .slide-head { position: absolute; left: 110px; top: 80px; }
  .slide-head h2 .line { display: inline; }
  .slide-head h2 { margin: 18px 0 0; font-size: 120px; line-height: .9; font-variation-settings: "wght" 700, "wdth" 100; }
  .legend { position: absolute; right: 110px; top: 110px; display: flex; flex-direction: column; gap: 14px; align-items: flex-end; }
  .legend span { font: 600 24px/1 var(--mono); display: flex; align-items: center; gap: 12px; color: #ffffffd0; }
  .legend i { width: 18px; height: 18px; border-radius: 50%; display: inline-block; box-shadow: 0 0 18px currentColor; }
  /* --- overview --- */
  .ga-bg { position: absolute; inset: 0; overflow: hidden; display: flex; flex-direction: column; justify-content: center; gap: 0; opacity: .5; }
  .ga-bg div { white-space: nowrap; font-size: 260px; line-height: .95; font-variation-settings: "wght" 700, "wdth" 100;
    color: transparent; -webkit-text-stroke: 2px #ffffff26; animation: scroll 30s linear infinite; }
  .ga-bg div:nth-child(2) { animation-direction: reverse; animation-duration: 36s; -webkit-text-stroke-color: #2ea04355; }
  .ga-copy { position: absolute; left: 110px; top: 0; bottom: 0; width: 640px; display: flex; flex-direction: column; justify-content: center; gap: 26px; }
  .ga-copy h2 { margin: 0; font-size: 106px; line-height: .92; font-variation-settings: "wght" 700, "wdth" 100; }
  .ga-copy h2 .l-grad .ch { background-image: linear-gradient(90deg, #3fb950, #4493f8, #ab7df8, #3fb950); }
  .badge { align-self: flex-start; font: 500 22px/1 "Mona Sans"; color: #3fb950; background: #2ea04326;
    border: 1px solid #2ea04366; padding: 10px 18px; border-radius: 2em; box-shadow: 0 0 30px #2ea04333; }
  .active .badge { animation: rise .7s both .1s, pulse 2.4s ease-in-out infinite 1s; }
  @keyframes pulse { 50% { box-shadow: 0 0 50px #2ea04388; } }
  .shot-wrap { position: absolute; right: 90px; top: 150px; width: 1080px; height: 780px; }
  .shot { position: absolute; inset: 0; border-radius: 12px; overflow: hidden; background: #0d1117; border: 1px solid #3d444d;
    box-shadow: 0 60px 140px -20px #000, 0 0 0 1px #000, 0 0 120px -30px #3fb950; }
  .active .shot { animation: shot-in 1.4s cubic-bezier(.2,.9,.2,1) both .35s, float 7s ease-in-out infinite 1.8s; }
  @keyframes shot-in { from { opacity: 0; transform: translateY(120px); } }
  @keyframes float { 0%, 100% { transform: translateY(0); } 50% { transform: translateY(-8px); } }
  .shot .bar { height: 48px; display: flex; align-items: center; gap: 10px; padding: 0 20px; background: #151b23; border-bottom: 1px solid #3d444d; }
  .shot .bar i { width: 14px; height: 14px; border-radius: 50%; }
  .shot .bar .url { margin-left: 18px; flex: 1; font: 500 17px/32px var(--mono); color: #9198a1; background: #0d1117;
    border-radius: 6px; padding: 0 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ga-svg { display: block; width: 100%; height: calc(100% - 48px); font-family: "Mona Sans", sans-serif; }
  .ga-svg text { fill: #e6edf3; }
  .ga-svg .ga-mono { font-family: var(--mono); white-space: pre; }
  .ga-svg .ga-hdot { fill: #6e7681; transition: fill .3s; }
  .ga-svg.ga-running .ga-hdot { fill: #d29922; animation: ga-blink 1s ease-in-out infinite; }
  .ga-svg.ga-done .ga-hdot { fill: #2ea043; }
  @keyframes ga-blink { 50% { opacity: .35; } }
  .ga-svg .ga-note { opacity: 0; transition: opacity .6s; }
  .ga-svg.ga-aw .ga-note { opacity: 1; }
  .ga-flip .ga-head { animation: ga-flip 1.6s ease-in-out; transform-box: fill-box; transform-origin: center; }
  @keyframes ga-flip { 50% { transform: scaleY(0); } }
  .ga-card .box { fill: #151b23; stroke: #3d444d; stroke-width: 2; transition: stroke .3s, fill .3s; }
  .ga-card.run .box { stroke: #d29922; }
  .ga-card.ok .box { stroke: #2ea043; }
  .ga-card.ag .box { fill: #1d1730; stroke: #8957e5; }
  .ga-card.ag.run .box { stroke: #ab7df8; animation: ga-glow 1.2s ease-in-out infinite; }
  @keyframes ga-glow { 50% { filter: drop-shadow(0 0 22px #a371f7); } }
  .ga-card .ga-tag { fill: #ab7df8; opacity: 0; transition: opacity .4s; }
  .ga-card.ag .ga-tag { opacity: 1; }
  .ga-card .ring { fill: none; stroke: #6e7681; stroke-width: 3; transform-box: fill-box; transform-origin: center; }
  .ga-card.run .ring { stroke: #d29922; stroke-dasharray: 46 23; animation: ga-spin 1s linear infinite; }
  .ga-card.ag.run .ring { stroke: #ab7df8; }
  .ga-card.ok .ring { fill: #238636; stroke: #238636; }
  .ga-card .chk { opacity: 0; }
  .ga-card.ok .chk { opacity: 1; }
  @keyframes ga-spin { to { transform: rotate(360deg); } }
  .ga-edge { stroke: #3d444d; stroke-width: 3; transition: stroke .3s; }
  .ga-edge.on { stroke: #2ea043; stroke-dasharray: 10 8; animation: ga-dash .6s linear infinite; }
  .ga-aw .ga-edge.on { stroke: #ab7df8; }
  @keyframes ga-dash { to { stroke-dashoffset: -18; } }
  .shot .glare { position: absolute; inset: 0; pointer-events: none;
    background: linear-gradient(115deg, transparent 30%, #ffffff14 45%, transparent 60%); background-size: 300% 100%;
    animation: glare 5s ease-in-out infinite; }
  @keyframes glare { from { background-position: 100% 0; } to { background-position: -100% 0; } }

  /* --- central agentic ops --- */
  .cao-steps { position: absolute; left: 114px; top: 330px; width: 560px; display: flex; flex-direction: column; gap: 18px; }
  .cao-step { display: grid; grid-template-columns: 64px 1fr; column-gap: 18px; opacity: 0; }
  .active .cao-step { animation: rise .8s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 160ms + 500ms); }
  .cao-step i { grid-row: span 2; font: 600 22px/1 var(--mono); color: var(--c); padding-top: 8px; font-style: normal; }
  .cao-step b { font-size: 40px; font-variation-settings: "wght" 700, "wdth" 100; }
  .cao-step span { font-size: 22px; color: #9198a1; line-height: 1.35; }
  .cao-canvas { position: absolute; left: 720px; top: 300px; width: 1140px; height: 640px; }
  .fm-quote { position: absolute; left: 114px; top: 318px; width: 600px; font: 700 44px/1.1 "Mona Sans"; font-stretch: 100%; color: #f0f6fc; opacity: 0; }
  .fm-quote em { font-style: normal; color: transparent; -webkit-background-clip: text; background-clip: text; background-image: linear-gradient(90deg, #f778ba, #ab7df8, #4493f8); }
  .active .fm-quote { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .4s; }
  .hap-quote { top: 288px; width: auto; white-space: nowrap; }
  .hap-svg { position: absolute; left: 110px; top: 390px; width: 1700px; height: 580px; overflow: visible; font-family: "Mona Sans"; }
  .fm-lane-t.hap-lt { text-anchor: start; }
  .hap-r { opacity: 0; }
  .active .hap-r { animation: rise .8s cubic-bezier(.2,.9,.2,1) both; transform-box: fill-box; transform-origin: left center; }
  .hap-row { fill: #ffffff08; stroke: #ffffff12; }
  .hap-name { font: 700 34px "Mona Sans"; font-stretch: 100%; }
  .hap-sub { fill: #9198a1; font: 500 20px var(--mono); }
  .hap-node { opacity: .4; transition: opacity .4s; }
  .hap-node.on { opacity: 1; }
  .hap-node.on rect { stroke: var(--c); fill: #161b22; filter: drop-shadow(0 0 16px var(--c)); }
  .hap-disc { fill: #0d1117; stroke: #9198a1; stroke-width: 2.5; }
  .hap-disc.h { stroke: #4493f8; stroke-width: 3; }
  .hap-node.on .hap-disc { stroke: var(--c); filter: drop-shadow(0 0 12px var(--c)); }
  .hap-node.on .hap-disc.h { stroke: #4493f8; filter: drop-shadow(0 0 16px #4493f8); }
  .fm-pill.g rect { stroke: #9198a1; filter: none; }
  .fmm-copy { position: absolute; left: 110px; top: 150px; width: 860px; }
  .fmm-copy h2 { margin: 0; font-variation-settings: "wght" 560, "wdth" 100; letter-spacing: -.02em; }
  .fmm-stop { margin-top: 30px !important; font-size: 80px; line-height: 1.04; color: #f0f6fc; }
  .fmm-go { margin-top: 44px !important; font-size: 100px; line-height: 1.02; }
  .fmm-go { color: #3fb950; }
  .fmm-grid { position: absolute; left: 1050px; top: 110px; width: 770px; height: 770px; display: grid; grid-template: repeat(7, 1fr) / repeat(7, 1fr);
    border-radius: 36px; overflow: hidden; box-shadow: 0 30px 120px #0008, 0 0 0 1px #ffffff14; }
  .fmm-cell { display: grid; place-items: center; transition: background .3s, transform .2s ease-in; color: #0d1117; background: #f6f8fa; }
  .active .fmm-cell { animation: fmm-in .6s cubic-bezier(.3,1.5,.4,1) both calc(var(--i) * 45ms + 300ms); }
  .fmm-cell.g { background: #f6f8fa; } .fmm-cell.g.grey { background: #d0d7de; }
  .fmm-cell b { font: 700 38px/1 var(--mono); }
  .fmm-cell svg { width: 38px; height: 38px; fill: #0d1117; }
  .fmm-cell.flip { transform: rotateY(90deg); }
  .fmm-cell.flop { transform: rotateY(0); transition: transform .26s cubic-bezier(.3,1.6,.4,1); }
  @keyframes fmm-in { from { transform: scale(0) rotate(-20deg); } }
  .fm-svg { position: absolute; left: 760px; top: 330px; width: 1100px; height: 640px; overflow: visible; font-family: "Mona Sans"; }
  .fm-cap { position: absolute; left: 762px; top: 272px; font: 500 30px/1 "Mona Sans"; color: #9198a1; }
  .fm-cap b { font-weight: 700; font-stretch: 100%; }
  .fm-cap.swap { animation: rise .7s cubic-bezier(.2,.9,.2,1) both; }
  .fm-lane { fill: #ffffff0d; }
  .fm-lane-t { fill: #9198a1; font: 600 20px var(--mono); letter-spacing: .2em; text-anchor: middle; }
  .fm-div { stroke: #ffffff1a; stroke-width: 1; }
  .fm-wire { fill: none; stroke-width: 2; opacity: .55; transition: opacity .4s; }
  .fm-wire.p { stroke: #ab7df8; } .fm-wire.b { stroke: #4493f8; }
  .fm-pill rect { fill: #0d1117; stroke: #ab7df8; stroke-width: 2; filter: drop-shadow(0 0 10px #8957e566); transition: all .4s; }
  .fm-pill.b rect { stroke: #4493f8; filter: drop-shadow(0 0 10px #1f6feb66); }
  .fm-pill text { fill: #f0f6fc; font: 600 22px "Mona Sans"; text-anchor: middle; }
  .fm-task, .fm-sched, .fm-maint { transition: opacity .4s; }
  .fm-task { opacity: .45; } .fm-task.on { opacity: 1; }
  .fm-task.on rect, .fm-sched.on rect { stroke: var(--c); fill: #161b22; filter: drop-shadow(0 0 18px var(--c)); }
  .fm-maint.on circle { filter: drop-shadow(0 0 18px #4493f8); }
  .fm-out rect { fill: #0d1117; stroke: #9198a1; stroke-width: 2; }
  .fm-out { opacity: .35; transform-box: fill-box; transform-origin: center; transition: opacity .3s; }
  .fm-out.on { opacity: 1; animation: fm-pop .5s cubic-bezier(.3,1.6,.4,1); }
  .fm-out.on rect { stroke: var(--c); filter: drop-shadow(0 0 14px var(--c)); }
  .mra-svg { left: 60px; top: 290px; width: 1800px; height: 566px; }
  .mra-svg .fm-pill text { font-size: 26px; }
  .rpa-svg { left: 150px; top: 300px; width: 1620px; height: 648px; }
  .rpa-code { opacity: .6; transition: opacity .4s; } .rpa-code.on { opacity: 1; }
  .rpa-code .card { fill: #0d1117e8; stroke: #3d444d; stroke-width: 2; transition: stroke .4s; }
  .rpa-code.on .card { stroke: #ab7df8; filter: drop-shadow(0 0 10px #8957e566); }
  .rpa-file { fill: #9198a1; font: 500 14px var(--mono); }
  .rpa-row { opacity: .45; transition: opacity .3s; } .rpa-row.seen { opacity: 1; }
  .rpa-dup, .rpa-link, .rpa-badge { opacity: 0; transition: opacity .4s; }
  .rpa-dup { fill: #d2992226; stroke: #d29922; stroke-width: 2; }
  .rpa-link { fill: none; stroke: #d29922; stroke-width: 2.5; stroke-dasharray: 5 4; } .rpa-link.dot { fill: #d29922; stroke: none; }
  .rpa-badge { fill: #d29922; font: 600 17px var(--mono); }
  .rpa-dup.on, .rpa-link.on, .rpa-badge.on { opacity: 1; }
  .rpa-scan { opacity: 0; } .rpa-scan line { stroke: #c297ff; stroke-width: 3; filter: drop-shadow(0 0 8px #ab7df8); }
  .fm-wire.day { stroke-dasharray: 10 8; }
  .rpa-day text { fill: #ab7df8; font: 600 19px var(--mono); letter-spacing: .12em; }
  .fm-wire.classic { stroke: #9198a1; stroke-dasharray: 6 8; opacity: .4; }
  .mra-l.classic { fill: #9198a1; font: 500 19px var(--mono); opacity: .8; }
  .prq-thr { fill: #d29922; font: 600 20px var(--mono); text-anchor: middle; opacity: 0; transition: opacity .4s, fill .4s; }
  .prq-thr.show { opacity: 1; } .prq-thr.ok { fill: #3fb950; }
  .rao-card { fill: #0d1117e8; stroke: #3d444d; stroke-width: 2; transition: stroke .4s, filter .4s; }
  .rao-card.on { stroke: var(--rc, #3fb950); filter: drop-shadow(0 0 12px var(--rc, #3fb950)); }
  .rao-h { fill: #f0f6fc; font: 700 22px "Mona Sans"; }
  .rao-sub, .rao-k { fill: #9198a1; font: 500 14px var(--mono); }
  .rao-v { fill: #e6edf3; font: 600 17px var(--mono); }
  .rao-v.warn, .rao-span .warn { fill: #d29922; } .rao-v.ok, .rao-span .ok { fill: #3fb950; }
  .rao-span { fill: #c9d1d9; font: 500 14px var(--mono); }
  .rao-span .id { fill: #6e7681; }
  .rao-spark { fill: none; stroke-width: 2; stroke-linejoin: round; transition: stroke .6s; }
  .rao-slo { stroke: #f85149; stroke-width: 1.5; stroke-dasharray: 5 5; opacity: .7; }
  .rao-breach { fill: #f85149; font: 700 14px var(--mono); transition: opacity .4s; }
  .rao-led { animation: rao-led 1.1s steps(2) infinite; }
  @keyframes rao-led { 50% { opacity: .25; } }
  .rao-pkt { opacity: 0; }
  .mra-repo { fill: #0d111799; stroke: #3d444d; stroke-width: 2; }
  .mra-repo-t { fill: #9198a1; font: 600 20px var(--mono); }
  .mra-row { transition: transform .6s cubic-bezier(.2,.9,.2,1); }
  .mra-row.new { animation: mra-in .6s cubic-bezier(.2,.9,.2,1); }
  @keyframes mra-in { from { opacity: 0; } }
  .mra-who { fill: #79c0ff; font: 600 19px var(--mono); }
  .mra-row.ag .mra-who { fill: #c297ff; }
  .mra-msg { fill: #e6edf3; font: 500 24px "Mona Sans"; }
  .mra-msg .pat { fill: #d29922; font-weight: 700; }
  .mra-beam { fill: none; stroke-width: 3; stroke-dasharray: 8 8; filter: drop-shadow(0 0 6px currentColor); animation: mra-beam 1.4s linear both; }
  @keyframes mra-beam { 0% { stroke-dashoffset: 0; opacity: 0; } 15% { opacity: 1; } 100% { stroke-dashoffset: -64; opacity: 0; } }
  .mra-seen { fill: #d29922; font: 700 20px var(--mono); opacity: 0; }
  .mra-seen.pop { animation: mra-pop 1.6s ease-out both; }
  @keyframes mra-pop { 0% { opacity: 0; } 15% { opacity: 1; } 70% { opacity: 1; } 100% { opacity: .5; } }
  #nMiner.blink circle { animation: mra-blink .7s ease-out; }
  @keyframes mra-blink { 30% { filter: drop-shadow(0 0 24px #c297ff) brightness(1.4); } }
  .mra-l { font: 600 21px "Mona Sans"; fill: #ab7df8; opacity: .55; transition: opacity .4s; }
  .mra-l.b { fill: #79c0ff; }
  .mra-lane { transition: fill .4s; } .mra-lane.on { fill: #f0f6fc; }
  .fm-wire.hot { opacity: 1; stroke-width: 3; }
  .fm-wire.hot + .mra-l { opacity: 1; }
  .mra-dev.on rect { stroke: #4493f8; filter: drop-shadow(0 0 18px #4493f8); }
  .sfl-svg { left: 150px; top: 300px; width: 1620px; height: 648px; }
  .sfl-svg .mra-l.sfl-ol { opacity: 0; } .sfl-svg .fm-wire.hot + .mra-l.sfl-ol { opacity: 1; }
  .sfl-box { opacity: .6; transition: opacity .4s; } .sfl-box.on { opacity: 1; }
  .sfl-card { fill: #0d1117e8; stroke: #3d444d; stroke-width: 2; transition: stroke .4s; }
  .sfl-box.on .sfl-card { stroke: #ab7df8; filter: drop-shadow(0 0 10px #8957e566); }
  .sfl-title { fill: #9198a1; font: 500 15px var(--mono); letter-spacing: .08em; }
  .sfl-row { opacity: .45; transition: opacity .3s; } .sfl-row.seen { opacity: 1; }
  .sfl-row text { fill: #c9d1d9; font: 500 17px var(--mono); }
  .sfl-row circle { fill: #3d444d; transition: fill .3s; } .sfl-row.seen circle { fill: #3fb950; }
  .sfl-row.seen.bad circle { fill: #f85149; filter: drop-shadow(0 0 6px #f85149); } .sfl-row.seen.bad text { fill: #ff7b72; }
  .sfl-row .ok { fill: #3fb950; } .sfl-row .ko { fill: #f85149; } .sfl-row .ed { fill: #d29922; }
  .sfl-badge { fill: #ff7b72; font: 600 17px var(--mono); opacity: 0; transition: opacity .4s; } .sfl-badge.on { opacity: 1; }
  .sfl-scan { opacity: 0; } .sfl-scan line { stroke: #c297ff; stroke-width: 3; filter: drop-shadow(0 0 8px #ab7df8); }
  .sfl-rule { fill: #ff7b72; font: 600 18px var(--mono); }
  #sflOut0.on rect { stroke: #3fb950; filter: drop-shadow(0 0 14px #3fb950); }
  #sflOut1.on rect { stroke: #d29922; filter: drop-shadow(0 0 14px #d29922); }
  .fm-dot { filter: drop-shadow(0 0 8px currentColor); opacity: 0; }
  @keyframes fm-pop { from { transform: scale(.4); } }
  .ra-flow { left: 60px; top: 340px; width: 880px; height: 512px; }
  .ra-chart { left: 990px; top: 340px; width: 880px; height: 512px; }
  .ra-cap-flow { left: 62px; top: 282px; }
  .ra-cap-chart { left: 992px; top: 282px; }
  .bd-grid { stroke: #ffffff12; stroke-width: 1; }
  .bd-yl { fill: #9198a1; font: 500 18px var(--mono); text-anchor: end; }
  .bd-xl { fill: #9198a1; font: 500 18px var(--mono); text-anchor: middle; }
  .bd-at { fill: #9198a1; font: 500 20px "Mona Sans"; text-anchor: middle; }
  .bd-note { fill: #6e7681; font: 500 15px var(--mono); text-anchor: middle; }
  .bd-line { fill: none; stroke: #c9d1d9; stroke-width: 1.6; opacity: .4; }
  .bd-line.d { stroke-dasharray: 7 6; opacity: .55; }
  .bd-med { fill: none; stroke: #3fb950; stroke-width: 4; filter: drop-shadow(0 0 8px #3fb950); }
  .bd-head { stroke: #3fb950; stroke-width: 2; opacity: 0; filter: drop-shadow(0 0 8px #3fb950); }
  .bd-adopt { stroke: #ab7df8; stroke-width: 3; stroke-dasharray: 1; stroke-dashoffset: 1; filter: drop-shadow(0 0 8px #8957e5); }
  .bd-pill { opacity: 0; transform-box: fill-box; transform-origin: center; }
  .bd-pill.on { opacity: 1; animation: fm-pop .5s cubic-bezier(.3,1.6,.4,1); }
  .bd-pill rect { fill: #0d1117; stroke: #ab7df8; stroke-width: 2; filter: drop-shadow(0 0 14px #8957e5aa); }
  .bd-pill text { fill: #f0f6fc; font: 600 22px "Mona Sans"; text-anchor: middle; }
  .bd-bot { opacity: 0; transition: opacity .4s; } .bd-bot.on { opacity: 1; }
  .bd-res { opacity: 0; transition: opacity .6s; } .bd-res.on { opacity: 1; }
  .bd-res text { fill: #3fb950; font: 800 34px "Mona Sans"; } .bd-res text.s { font: 600 18px var(--mono); fill: #9198a1; }
  .fm-marquee { position: absolute; left: 0; right: 0; bottom: 92px; overflow: hidden; white-space: nowrap;
    -webkit-mask-image: linear-gradient(90deg, transparent, #000 12%, #000 88%, transparent); mask-image: linear-gradient(90deg, transparent, #000 12%, #000 88%, transparent); }
  .fm-marquee div { display: inline-flex; gap: 14px; animation: fm-scroll 60s linear infinite; }
  .fm-marquee span { padding: 8px 18px; border-radius: 2em; border: 1px solid #3d444d; background: #151b23cc; font: 500 19px/1 var(--mono); color: #c9d1d9; }
  @keyframes fm-scroll { to { transform: translateX(-50%); } }
  .cao-tag { position: absolute; font: 600 16px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #9198a1; }
  .rollout { position: absolute; right: 110px; bottom: 70px; display: flex; gap: 14px; align-items: center; font: 600 20px/1 var(--mono); }
  .rollout em { font-style: normal; padding: 10px 18px; border: 1px solid #3d444d; border-radius: 999px; color: #9198a1; transition: all .4s; }
  .rollout em.on { color: #0d1117; background: #3fb950; border-color: #3fb950; box-shadow: 0 0 24px #3fb95080; }
  .rollout s { text-decoration: none; color: #3d444d; }
  .finale-canvas { position: absolute; left: 0; top: 0; width: 1920px; height: 1080px; }

  /* --- security architecture --- */
  .sec-box { position: absolute; top: 350px; width: 500px; height: 500px; padding: 34px 36px; border-radius: 22px;
    background: #161b22d0; border: 2px solid var(--c); box-shadow: 0 0 60px -20px var(--c), inset 0 0 80px -50px var(--c); opacity: 0; }
  .active .sec-box { animation: sec-in .9s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 800ms + 500ms); }
  @keyframes sec-in { from { opacity: 0; transform: translateX(-90px) scale(.9); filter: blur(10px); } to { opacity: 1; transform: none; filter: none; } }
  .sec-box li, .sec-box .sec-shield { opacity: 0; }
  .active .sec-box li { animation: rise .6s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 800ms + 850ms + var(--j) * 110ms); }
  .active .sec-box .sec-shield { animation: rise .5s cubic-bezier(.3,1.6,.4,1) both 1900ms; }
  .sec-box.sandbox { border-style: dashed; }
  .sec .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #ff7b72, #f85149, #d29922); }
  .sec-omen { position: absolute; inset: -40px; pointer-events: none;
    background: radial-gradient(ellipse 55% 60% at 50% 58%, transparent 30%, #2a0306aa 70%, #050000f0 100%); }
  .active .sec-omen { animation: omen 7s ease-in-out infinite; }
  .sec-omen { display: none; } /* the vignette lives on the full-screen .wash::after instead */
  @keyframes omen { 0%, 100% { opacity: .85; } 50% { opacity: 1; } 52% { opacity: .7; } 54% { opacity: 1; } }
  .sec-threats span { position: absolute; font: 500 22px/1 var(--mono); color: #f85149; opacity: 0; white-space: nowrap; filter: blur(.6px); text-shadow: 0 0 18px #f8514988; }
  .active .sec-threats span { animation: threat 9s ease-in-out infinite calc(var(--i) * -0.77s); }
  @keyframes threat { 0% { opacity: 0; transform: translate(0, 10px); } 20% { opacity: .38; } 23% { opacity: .08; } 25% { opacity: .42; }
    70% { opacity: .3; } 100% { opacity: 0; transform: translate(-30px, -16px); } }
  .sec-box:not(.sandbox) { background: #12080ae0; box-shadow: 0 0 50px -24px var(--c), inset 0 0 60px -50px var(--c); border-color: color-mix(in srgb, var(--c) 70%, transparent); }
  .sec-box.out { background: #0a1424f0; border-color: #4493f8; }
  .active .sec-box.out { animation: sec-in .9s cubic-bezier(.2,.9,.2,1) both 2100ms, calm 4s ease-in-out infinite 3s; }
  @keyframes calm { 0%, 100% { box-shadow: 0 0 60px -14px #4493f888, inset 0 0 80px -40px #4493f855; } 50% { box-shadow: 0 0 110px 0 #4493f855, inset 0 0 100px -40px #4493f877; } }
  .sec-box.sandbox { background: #0b1a10f0; border-width: 3px; z-index: 2; }
  .active .sec-box.sandbox { animation: sec-in .9s cubic-bezier(.2,.9,.2,1) both 1300ms, safe 3.2s ease-in-out infinite 2.2s; }
  @keyframes safe { 0%, 100% { box-shadow: 0 0 70px -10px #3fb95099, inset 0 0 90px -40px #3fb95066; } 50% { box-shadow: 0 0 140px 10px #3fb95066, inset 0 0 120px -40px #3fb95088; } }
  .sec-shield { position: absolute; right: 26px; top: 26px; font: 600 16px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #0d1117;
    background: #3fb950; padding: 8px 12px; border-radius: 99px; box-shadow: 0 0 24px #3fb950aa; }
  .sec-box.sandbox ul { gap: 10px; }
  .sec-box.sandbox li { font-size: 23px; }
  .sec-box i { font: 600 18px/1 var(--mono); letter-spacing: .18em; color: var(--c); font-style: normal; }
  .sec-box h3 { margin: 18px 0 26px; font-size: 54px; line-height: 1; font-variation-settings: "wght" 700, "wdth" 100; }
  .sec-box ul { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 14px; }
  .sec-box li { font-size: 25px; line-height: 1.3; color: #d1d7e0; padding-left: 30px; position: relative; }
  .sec-box li::before { content: ""; position: absolute; left: 0; top: 11px; width: 12px; height: 12px; border-radius: 3px; background: var(--c); box-shadow: 0 0 10px var(--c); }
  .sec-box code { font: 600 21px var(--mono); color: var(--c); }
  .sec-arrow { position: absolute; top: 570px; width: 100px; text-align: center; font-size: 60px; color: #3d444d; opacity: 0; }
  .active .sec-arrow { animation: sec-arr .5s ease-out both calc(var(--i) * 800ms + 1100ms), nudge 1.6s ease-in-out infinite calc(var(--i) * 800ms + 1600ms); }
  @keyframes sec-arr { from { opacity: 0; transform: translateX(-30px); } to { opacity: 1; transform: none; } }
  @keyframes nudge { 50% { transform: translateX(10px); color: #9198a1; } }
  .sec-ent { position: absolute; left: 110px; top: 268px; display: flex; align-items: center; gap: 14px; }
  .sec-ent i { font: 600 18px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: #d29922; font-style: normal; margin-right: 10px; }
  .sec-ent span { font-size: 24px; padding: 8px 20px; border-radius: 99px; border: 1px solid #d2992266; background: #d2992214; color: #f0f6fc; opacity: 0; }
  .active .sec-ent span { animation: rise .7s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 120ms + 2900ms); }
  .sec-lane { position: absolute; left: 0; top: 880px; width: 1920px; height: 120px; }

  /* --- safe outputs: boxed agent → threat detection → write job --- */
  .so .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #79c0ff, #4493f8, #3fb950); }
  .so-box { position: absolute; top: 300px; width: 500px; height: 620px; padding: 30px 34px; border-radius: 22px; border: 2px solid var(--c);
    background: #0d1117ee; box-shadow: 0 0 50px -24px var(--c), inset 0 0 60px -50px var(--c); opacity: 0; transition: box-shadow .5s, border-color .5s; }
  .active .so-box { animation: sec-in .9s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 500ms + 400ms); }
  .so-box.hot { box-shadow: 0 0 110px -10px var(--c), inset 0 0 90px -40px var(--c); }
  .so-box i { font: 600 17px/1 var(--mono); letter-spacing: .18em; color: var(--c); font-style: normal; }
  .so-box h3 { margin: 16px 0 22px; font-size: 50px; line-height: 1; font-variation-settings: "wght" 700, "wdth" 100; }
  .so-who { display: flex; align-items: center; gap: 16px; font-size: 25px; color: #d1d7e0; }
  .so-av { width: 62px; height: 62px; border-radius: 50%; display: grid; place-items: center; flex: none; background: radial-gradient(circle at 35% 30%, #c297ff, #8957e5 55%, #3b1d8f); }
  .so-av.det { background: radial-gradient(circle at 35% 30%, #ffc28a, #d29922 55%, #6e4a00); }
  .so-av svg { width: 36px; height: 36px; }
  .so-perm { margin-left: auto; font: 600 17px/1 var(--mono); padding: 8px 12px; border-radius: 99px; border: 1px solid var(--c); color: var(--c); background: color-mix(in srgb, var(--c) 12%, transparent); }
  /* the agent's box is a cage: bars that flash red when it tries to write directly */
  .so-cage { position: absolute; inset: 0; border-radius: 20px; pointer-events: none; opacity: .5; transition: opacity .3s;
    background: repeating-linear-gradient(90deg, transparent 0 46px, #3fb95033 46px 50px); }
  .so-cage::after { content: ""; position: absolute; inset: 0; border-radius: 20px; opacity: 0; background: repeating-linear-gradient(90deg, transparent 0 46px, #f85149aa 46px 50px); }
  .so.p-deny:not(.p-safe) .so-cage::after { animation: so-bars .9s ease-out both; }
  @keyframes so-bars { 0%, 40% { opacity: 1; } 20%, 60% { opacity: .3; } 100% { opacity: .9; } }
  .so.p-deny:not(.p-safe) .so-b0 { border-color: #f85149; box-shadow: 0 0 110px -10px #f85149, inset 0 0 90px -40px #f85149; }
  .so.p-deny:not(.p-safe) .so-b0 .so-shake { animation: so-shake .45s linear both; }
  @keyframes so-shake { 20% { transform: translateX(-10px); } 40% { transform: translateX(9px); } 60% { transform: translateX(-6px); } 80% { transform: translateX(4px); } }
  .so-term { margin-top: 22px; padding: 16px 14px; border-radius: 12px; background: #010409; border: 1px solid #30363d; font: 500 17px/1.55 var(--mono); color: #d1d7e0; min-height: 132px; }
  .so-term div { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .so-term .l2, .so-term .l3 { opacity: 0; transition: opacity .3s; }
  .so-term .l2 { color: #ff7b72; }
  .so-term .l3 { color: #79c0ff; }
  .so.p-deny .so-term .l2, .so.p-safe .so-term .l3 { opacity: 1; }
  .so-term .cur::after { content: "▍"; color: #3fb950; animation: blink 1s steps(1) infinite; }
  .so.p-deny .so-term .cur::after { content: none; }
  @keyframes blink { 50% { opacity: 0; } }
  .so-checks { margin: 22px 0 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 12px; }
  .so-checks li { font-size: 24px; color: #9198a1; padding-left: 40px; position: relative; transition: color .3s; }
  .so-checks li::before { content: "·"; position: absolute; left: 0; top: -3px; width: 28px; height: 28px; border-radius: 50%; display: grid; place-items: center;
    font: 700 18px/28px var(--mono); border: 2px solid #3d444d; color: #3d444d; transition: all .3s; }
  .so-checks li.ok { color: #f0f6fc; }
  .so-checks li.ok::before { content: "✓"; border-color: #3fb950; background: #3fb950; color: #0d1117; box-shadow: 0 0 16px #3fb950aa; }
  .so-arrow { position: absolute; top: 560px; width: 100px; text-align: center; font-size: 60px; color: #3d444d; opacity: 0; }
  .active .so-arrow { animation: sec-arr .5s ease-out both calc(var(--i) * 500ms + 900ms), nudge 1.6s ease-in-out infinite calc(var(--i) * 500ms + 1400ms); }
  .so-call { position: absolute; top: 508px; width: 100px; text-align: center; white-space: nowrap; font: 600 12px/1.2 var(--mono); color: #9198a1; opacity: 0; }
  .active .so-call { animation: rise .5s ease-out both calc(var(--i) * 500ms + 1100ms); }
  /* the proposed issue travels box to box */
  .so-card { position: absolute; top: 742px; width: 420px; height: 150px; padding: 18px 22px; border-radius: 14px; background: #161b22; border: 2px dashed #4493f8;
    box-shadow: 0 10px 40px -10px #000; opacity: 0; transform: scale(.85); overflow: hidden; z-index: 3;
    transition: left .9s cubic-bezier(.6,0,.2,1), opacity .4s, transform .5s cubic-bezier(.3,1.6,.4,1), border-color .4s, box-shadow .4s; }
  .so.p-safe .so-card { opacity: 1; transform: none; }
  .so.p-safe .so-card.moving { transform: translateY(-26px) rotate(-2deg); }
  .so-card .t { display: flex; align-items: center; gap: 12px; font: 600 25px/1.2 "Mona Sans"; color: #f0f6fc; }
  .so-card .t svg { width: 28px; height: 28px; flex: none; }
  .so-card .lb { margin-top: 12px; display: flex; gap: 10px; }
  .so-card .lb span { font: 600 16px/1 var(--mono); padding: 6px 12px; border-radius: 99px; border: 1px solid #d2992266; background: #d2992222; color: #e3b341; }
  .so-card .lb span + span { border-color: #ab7df866; background: #ab7df822; color: #d2b8ff; }
  .so-card .st { position: absolute; right: 18px; bottom: 16px; font: 600 16px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: #79c0ff; }
  .so.p-pass .so-card .st { color: #3fb950; }
  .so.p-done .so-card { border-style: solid; border-color: #3fb950; box-shadow: 0 0 60px -6px #3fb95088; }
  .so.p-done .so-card .st { color: #0d1117; background: #3fb950; padding: 7px 12px; border-radius: 99px; letter-spacing: .04em; }
  .so-beam { position: absolute; left: 0; right: 0; top: -40px; height: 40px; opacity: 0;
    background: linear-gradient(180deg, transparent, #d2992266); border-bottom: 2px solid #e3b341; box-shadow: 0 4px 18px #d29922; }
  .so.p-scan:not(.p-pass) .so-beam { opacity: 1; animation: so-scan 1s ease-in-out infinite alternate; }
  @keyframes so-scan { from { top: -40px; } to { top: 150px; } }
  .so-note { margin-top: 18px; font-size: 22px; line-height: 1.35; color: #9198a1; }
  .so-note code { font: 600 19px var(--mono); color: #79c0ff; }

  /* --- compiler hardening --- */
  .ch-slide .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #3fb950, #79c0ff, #ab7df8, #3fb950); }
  .ch-cv { position: absolute; left: 0; top: 0; width: 1920px; height: 1080px; pointer-events: none; }
  .ch-file { position: absolute; left: 110px; width: 520px; border-radius: 16px; background: #0d1117f2; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000, 0 0 50px -28px var(--c); overflow: hidden; opacity: 0; }
  .active .ch-file { animation: rise .8s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 500ms + 300ms); }
  .ch-file .cm-tab { height: 44px; font-size: 18px; }
  .ch-file .cm-tab .sv { color: var(--c); }
  .ch-file pre { margin: 0; padding: 12px 22px 16px; font: 500 18px/25px var(--mono); color: #d1d7e0; white-space: pre; }
  .ch-file pre span { display: block; }
  .ch-k { color: #ff7b72; } .ch-v { color: #a5d6ff; } .ch-c { color: #656c76; } .ch-p { color: #f0f6fc; font-family: "Mona Sans"; font-weight: 600; }
  .ch-file pre .ch-g { margin: 0 -22px; padding: 0 22px; background: #3fb95014; box-shadow: inset 3px 0 #3fb950; }
  .ch-cmd { position: absolute; left: 110px; top: 712px; width: 520px; display: flex; align-items: center; gap: 12px; font: 600 17px/1 var(--mono); color: #3fb950; opacity: 0; }
  .ch-cmd b { color: #9198a1; font-weight: 500; }
  .active .ch-cmd { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .7s; }
  .ch-legend { position: absolute; left: 1340px; top: 296px; width: 480px; display: flex; flex-direction: column; gap: 10px; }
  .ch-row { display: grid; grid-template-columns: 46px 1fr auto; column-gap: 14px; align-items: center; padding: 11px 18px; border-radius: 14px;
    background: #0d1117f8; border: 1px solid #3d444d; opacity: 0; transition: border-color .4s, box-shadow .4s, background .4s; }
  .active .ch-row { animation: rise .7s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 110ms + 600ms); }
  .ch-row.hit { border-color: var(--c); box-shadow: 0 0 34px -8px var(--c); background: color-mix(in srgb, var(--c) 12%, #0d1117e6); transition: none; }
  .ch-row i { grid-row: span 2; font: 700 22px/1 var(--mono); color: var(--c); font-style: normal; }
  .ch-row b { font-size: 25px; line-height: 1.15; font-variation-settings: "wght" 700, "wdth" 100; }
  .ch-row span { font: 500 14px/1.35 var(--mono); color: #9198a1; }
  .ch-row em { grid-row: span 2; grid-column: 3; font: 700 26px/1 var(--mono); font-style: normal; color: var(--c); font-variant-numeric: tabular-nums; min-width: 2.2em; text-align: right; }
  .ch-scan { position: absolute; left: 1340px; top: 928px; width: 480px; font: 500 15px/1 var(--mono); letter-spacing: .08em; color: #9198a1; text-align: center; opacity: 0; }
  .active .ch-scan { animation: rise .8s cubic-bezier(.2,.9,.2,1) both 1.4s; }

  /* --- turn intent to automation --- */
  .slide-head h2.pf-h { font-size: 100px; }
  .pf-card { position: absolute; top: 360px; border-radius: 18px; background: #161b22e8; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000, 0 0 50px -25px var(--c); opacity: 0; transform: translateY(30px) scale(.97); transition: opacity .6s, transform .8s cubic-bezier(.2,.9,.2,1); }
  .pf-card.on { opacity: 1; transform: none; }
  .pf-tta { position: absolute; left: 1330px; top: 112px; width: 480px; display: flex; align-items: center; gap: 26px; padding: 20px 28px; border-radius: 22px;
    background: #161b22e8; border: 1px solid #3d444d; transition: border-color .5s, box-shadow .5s; }
  .pf-tta svg { width: 96px; height: 96px; flex: none; transform: rotate(-90deg); }
  .pf-tta circle { fill: none; stroke-width: 9; }
  .pf-tta .bg { stroke: #ffffff14; }
  .pf-tta .fg { stroke: #3fb950; stroke-linecap: round; }
  .pf-tta i { display: block; font: 600 17px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: #9198a1; font-style: normal; margin-bottom: 10px; }
  .pf-tta b { font: 700 64px/1 var(--mono); color: #f0f6fc; font-variant-numeric: tabular-nums; letter-spacing: -.03em; }
  .pf-tta small { font: 500 26px/1 var(--mono); color: #9198a1; margin-left: 10px; }
  .pf-tta.done { border-color: #3fb950; box-shadow: 0 0 60px -10px #3fb95088; }
  .pf-tta.done b { color: #3fb950; }
  .pf-card .cap { font: 600 16px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: var(--c); position: absolute; top: -40px; left: 4px; }
  .pf-prompt { left: 130px; top: 318px; width: 380px; height: 500px; padding: 70px 22px 40px; --c: #ab7df8; border-radius: 58px; background: #0d1117; border: 12px solid #1c2128;
    box-shadow: 0 0 0 2px #3d444d, 0 0 0 4px #0d1117, 0 40px 90px -30px #000, 0 0 70px -25px var(--c); }
  .pf-prompt::before { content: ""; position: absolute; top: 14px; left: 50%; width: 110px; height: 32px; margin-left: -55px; border-radius: 99px; background: #000; }
  .pf-prompt::after { content: ""; position: absolute; bottom: 10px; left: 50%; width: 130px; height: 5px; margin-left: -65px; border-radius: 99px; background: #d1d7e0aa; }
  .pf-prompt .cap { top: -52px; left: -8px; }
  .pf-prompt .sb { position: absolute; top: 20px; left: 30px; right: 28px; display: flex; justify-content: space-between; font: 600 17px/1 var(--sans, inherit); color: #f0f6fc; }
  .pf-prompt .sb span { letter-spacing: .1em; font-size: 13px; }
  .pf-prompt .you { font-size: 18px; color: #9198a1; margin-bottom: 14px; text-align: center; padding-bottom: 14px; border-bottom: 1px solid #ffffff14; }
  .pf-prompt .msg { font-size: 28px; line-height: 1.28; min-height: 140px; margin-left: 28px; background: linear-gradient(135deg, #8957e5, #6e40c9); color: #fff; border-radius: 26px 26px 6px 26px; padding: 18px 22px; }
  .pf-prompt .reply { margin: 16px 28px 0 0; font-size: 20px; color: #d1d7e0; background: #21262d; border-radius: 26px 26px 26px 6px; padding: 16px 20px; opacity: 0; transition: opacity .5s; }
  .pf-prompt .reply.on { opacity: 1; }
  .pf-prompt .reply code, .pf-md .file { font-family: var(--mono); color: #d1d7e0; }
  .pf-caret { display: inline-block; width: 3px; height: 1em; margin-left: 2px; vertical-align: -3px; background: currentColor; animation: blink 1s steps(1) infinite; }
  @keyframes blink { 50% { opacity: 0; } }
  .pf-md { left: 570px; width: 480px; --c: #4493f8; }
  .pf-md .file { padding: 18px 26px; border-bottom: 1px solid #3d444d; font-size: 19px; }
  .pf-md pre { margin: 0; padding: 22px 24px; font: 500 20px/1.55 var(--mono); min-height: 300px; white-space: pre-wrap; }
  .pf-yml { left: 1090px; width: 300px; --c: #d29922; }
  .pf-yml .file { padding: 18px 20px; border-bottom: 1px solid #3d444d; font: 500 15px/1 var(--mono); color: #d1d7e0; }
  .pf-yml pre { margin: 0; padding: 18px 20px 14px; font: 500 14px/1.6 var(--mono); min-height: 250px; white-space: pre; overflow: hidden; }
  .pf-yml .go { margin: 0 16px 16px; padding: 10px 12px; border-radius: 10px; background: #d2992214; border: 1px solid #d2992255; font: 600 14px/1.35 var(--mono); color: #e3b341; opacity: 0; transition: opacity .4s; }
  .pf-yml .go.on { opacity: 1; }
  .pf-yml .go span { display: block; color: #9198a1; font-weight: 500; }
  .pf-issue { left: 1430px; width: 380px; padding: 30px; --c: #3fb950; }
  .pf-issue h4 { margin: 0 0 22px; font-size: 28px; line-height: 1.2; display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
  .pf-issue h4 small { color: #9198a1; font-weight: 400; }
  .pf-issue .open { font-size: 18px; padding: 6px 14px; border-radius: 99px; background: #238636; color: #fff; }
  .pf-issue ul { margin: 0; padding: 22px 0 0 26px; border-top: 1px solid #3d444d; display: flex; flex-direction: column; gap: 14px; font-size: 23px; color: #d1d7e0; }
  .pf-issue li { opacity: 0; transform: translateX(-12px); transition: opacity .4s, transform .5s; }
  .pf-issue li.on { opacity: 1; transform: none; }
  .pf-issue b { color: #f0f6fc; font-variant-numeric: tabular-nums; }
  .pf-link { position: absolute; top: 560px; height: 2px; background: linear-gradient(90deg, var(--a), var(--b)); transform-origin: left; transform: scaleX(0); transition: transform .8s cubic-bezier(.2,.9,.2,1); }
  .pf-link.on { transform: scaleX(1); }
  .pf-tag { position: absolute; top: 860px; font: 600 17px/1.3 var(--mono); color: #9198a1; text-align: center; width: 320px; opacity: 0; transition: opacity .5s; }
  .pf-tag.on { opacity: 1; }
  .pf-tag em { display: block; font-style: normal; color: #f0f6fc; font-size: 23px; margin-bottom: 6px; }

  /* --- anatomy of a workflow --- */
  .an .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #d29922, #4493f8, #39c5cf, #3fb950, #ab7df8); }
  .an-file { position: absolute; left: 200px; top: 270px; width: 780px; border-radius: 16px; background: #0d1117f0; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000; }
  .an-file .file { height: 56px; box-sizing: border-box; padding: 0 24px; display: flex; align-items: center; border-bottom: 1px solid #3d444d; font: 500 19px/1 var(--mono); color: #d1d7e0; }
  .an-code { position: relative; padding: 18px 0; font: 500 21px/30px var(--mono); white-space: pre; }
  .an-ln { position: relative; z-index: 1; padding: 0 26px; height: 30px; opacity: .38; transition: opacity .5s; }
  .an-ln.on { opacity: 1; }
  .an-band { position: absolute; left: 0; right: 0; background: color-mix(in srgb, var(--c) 9%, transparent); border-left: 4px solid var(--c); opacity: 0; transition: opacity .5s, background .5s, box-shadow .5s; }
  .an-band.on { opacity: 1; }
  .an-band.hot { background: color-mix(in srgb, var(--c) 22%, transparent); box-shadow: inset 0 0 40px -12px var(--c); }
  .an-tok { border-radius: 6px; transition: background .4s, box-shadow .4s; }
  .an-tok.hot { background: color-mix(in srgb, var(--c) 30%, transparent); box-shadow: 0 0 0 2px var(--c), 0 0 24px var(--c); }
  .an-bot { position: absolute; z-index: 2; right: 18px; display: flex; align-items: center; gap: 10px; height: 30px; padding: 0 14px 0 10px; border-radius: 99px; background: var(--c); color: #0d1117;
    font: 700 15px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; opacity: 0; transform: scale(.8); transition: opacity .5s, transform .6s cubic-bezier(.3,1.6,.4,1), box-shadow .5s; }
  .an-bot.on { opacity: 1; transform: none; }
  .an-bot svg { width: 22px; height: 22px; fill: #0d1117; }
  .an-bot.hot { box-shadow: 0 0 30px var(--c); }
  .an-brace { position: absolute; left: 150px; width: 26px; border: 3px solid var(--c); border-right: 0; border-radius: 14px 0 0 14px; opacity: 0; transform: translateX(-16px); transition: opacity .6s, transform .7s cubic-bezier(.2,.9,.2,1), box-shadow .5s; }
  .an-brace.on { opacity: 1; transform: none; }
  .an-brace.hot { box-shadow: -10px 0 30px -12px var(--c); }
  .an-brace span { position: absolute; left: -24px; top: 50%; transform: translate(-50%, -50%) rotate(-90deg); white-space: nowrap; font: 600 18px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: var(--c); }
  .an-wires { position: absolute; inset: 0; width: 1920px; height: 1080px; pointer-events: none; overflow: visible; }
  .an-wires path { fill: none; stroke: var(--c); stroke-width: 2.5; opacity: 0; stroke-dasharray: 6 8; transition: opacity .5s; }
  .an-wires path.on { opacity: .55; }
  .an-wires path.hot { opacity: 1; animation: an-flow .8s linear infinite; }
  .an-wires circle { fill: var(--c); opacity: 0; transition: opacity .5s; }
  .an-wires circle.on { opacity: 1; }
  @keyframes an-flow { to { stroke-dashoffset: -28; } }
  .an-call { position: absolute; left: 1140px; width: 670px; height: 126px; box-sizing: border-box; padding: 18px 24px 0 96px; border-radius: 16px; background: #161b22e8; border: 1px solid #3d444d;
    opacity: 0; transform: translateX(30px); transition: opacity .6s, transform .7s cubic-bezier(.2,.9,.2,1), border-color .5s, box-shadow .5s; }
  .an-call.on { opacity: 1; transform: none; }
  .an-call.hot { border-color: var(--c); box-shadow: 0 0 50px -16px var(--c); }
  .an-cc { position: absolute; left: 1210px; top: 140px; width: 600px; height: 96px; display: flex; align-items: center; gap: 14px; font: 500 20px/1 var(--mono); opacity: 0; transform: translateY(-12px); transition: opacity .6s, transform .7s cubic-bezier(.2,.9,.2,1); }
  .an-cc.on { opacity: 1; transform: none; }
  .an-cc .term { padding: 14px 18px; border-radius: 12px; background: #0d1117; border: 1px solid #3d444d; color: #f0f6fc; white-space: nowrap; }
  .an-cc .term b { color: #3fb950; font-weight: 500; }
  .an-cc .arr { flex: 1; height: 3px; min-width: 40px; position: relative; background: repeating-linear-gradient(90deg, #d29922 0 8px, transparent 8px 14px); background-size: 14px 3px; }
  .an-cc .arr::after { content: ""; position: absolute; right: -2px; top: -7px; border: 8px solid transparent; border-left: 12px solid #d29922; border-right: 0; }
  .an-cc.hot .arr { animation: an-arr .6s linear infinite; }
  @keyframes an-arr { to { background-position: 14px 0; } }
  .an-cc .lockf { padding: 10px 16px; border-radius: 12px; background: #161b22; border: 1px solid #d29922; color: #f0f6fc; white-space: nowrap; transition: box-shadow .5s; }
  .an-cc .lockf small { display: block; margin-top: 6px; font: 500 14px/1 var(--mono); color: #9198a1; }
  .an-cc.hot .lockf { box-shadow: 0 0 36px -8px #d29922; }
  .an-call .an-num { position: absolute; left: 24px; top: 22px; width: 52px; height: 52px; border-radius: 14px; display: grid; place-items: center; font: 700 22px/1 var(--mono); color: #0d1117; background: var(--c); }
  .an-call h3 { margin: 0 0 10px; display: flex; align-items: center; gap: 14px; font-size: 32px; line-height: 1; font-variation-settings: "wght" 700, "wdth" 100; }
  .an-call h3 code { font: 500 16px/1 var(--mono); color: var(--c); padding: 6px 10px; border-radius: 8px; border: 1px solid color-mix(in srgb, var(--c) 50%, transparent); }
  .an-call p { margin: 0; font-size: 21px; line-height: 1.3; color: #d1d7e0; }
  .an-call p code { font: 500 18px var(--mono); color: var(--c); }

  /* --- cost management --- */
  .cm .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #d29922, #3fb950, #4493f8, #d29922); }
  .cm-ed { position: absolute; left: 110px; top: 290px; width: 1010px; border-radius: 16px; background: #0d1117f0; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000; overflow: hidden; opacity: 0; }
  .active .cm-ed { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .3s; }
  .cm-tab { display: flex; align-items: center; gap: 12px; height: 50px; padding: 0 22px; background: #151b23; border-bottom: 1px solid #3d444d; font: 500 19px/1 var(--mono); color: #f0f6fc; }
  .cm-tab span { color: #656c76; }
  .cm-tab .sv { margin-left: auto; color: #9198a1; font-size: 16px; letter-spacing: .1em; }
  .cm-crumb { padding: 12px 22px 4px; font: 400 16px/1 var(--mono); color: #9198a1; }
  .cm-code { position: relative; padding: 8px 0 14px; font: 500 21px/28px var(--mono); }
  .cm-row { display: flex; height: 28px; white-space: pre; }
  .cm-row .no { width: 92px; padding-right: 30px; text-align: right; color: #656c76; flex: none; }
  .cm-row.new .tx { animation: cmIn .6s cubic-bezier(.2,.9,.2,1) both; }
  .cm-row.chg .v { animation: cmFlash 1.2s ease-out both; border-radius: 4px; }
  @keyframes cmIn { from { opacity: 0; transform: translateX(-24px); filter: blur(4px); } to { opacity: 1; transform: none; filter: none; } }
  @keyframes cmFlash { from { background: #4493f866; } to { background: transparent; } }
  .cm-hl { position: absolute; left: 78px; border: 2px solid var(--c); border-radius: 6px; pointer-events: none; opacity: .45; transition: opacity .5s, box-shadow .5s;
    animation: cmBox .6s cubic-bezier(.2,.9,.2,1) both; }
  .cm-hl.hot { opacity: 1; box-shadow: 0 0 30px -4px var(--c), inset 0 0 24px -10px var(--c); }
  @keyframes cmBox { from { opacity: 0; transform: scale(1.04); } }
  .cm-pill { position: absolute; display: flex; align-items: center; gap: 12px; height: 46px; padding: 0 22px 0 6px; border-radius: 99px; background: #161b22; border: 2px solid var(--c);
    font: 600 22px/1 "Mona Sans"; color: #f0f6fc; white-space: nowrap; opacity: .6; transition: opacity .5s, box-shadow .5s; animation: cmPop .7s cubic-bezier(.3,1.6,.4,1) both; }
  .cm-pill b { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 99px; background: var(--c); color: #0d1117; font: 700 15px/1 var(--mono); }
  .cm-pill.hot { opacity: 1; box-shadow: 0 0 40px -6px var(--c); }
  @keyframes cmPop { from { opacity: 0; transform: translateX(-20px) scale(.6); } }
  .cm-meter { position: absolute; left: 1180px; top: 290px; width: 630px; padding: 22px 30px 20px; border-radius: 18px; background: #161b22e8; border: 1px solid #3d444d; opacity: 0; }
  .active .cm-meter { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .5s; }
  .cm-gmo { display: flex; gap: 10px; margin-bottom: 16px; }
  .cm-gmo span { font: 600 16px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; padding: 9px 16px; border-radius: 99px; border: 1px solid #3d444d; color: #656c76; transition: all .5s; }
  .cm-gmo span.on { color: #f0f6fc; border-color: var(--c); background: color-mix(in srgb, var(--c) 18%, transparent); box-shadow: 0 0 24px -8px var(--c); }
  .cm-meter i { display: block; font: 600 16px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: #9198a1; font-style: normal; }
  .cm-big { display: flex; align-items: baseline; gap: 14px; margin: 8px 0 10px; }
  .cm-big b { font: 700 72px/1 var(--mono); letter-spacing: -.04em; font-variant-numeric: tabular-nums; color: #f0f6fc; transition: color .6s; }
  .cm-big small { font: 500 26px/1 var(--mono); color: #9198a1; }
  .cm-big em { margin-left: auto; font: 700 40px/1 var(--mono); font-style: normal; color: #3fb950; opacity: 0; transition: opacity .5s; }
  .cm-big em.on { opacity: 1; }
  .cm-bars { display: flex; align-items: flex-end; gap: 14px; height: 120px; border-bottom: 1px solid #3d444d; }
  .cm-bar { flex: 1; display: flex; flex-direction: column; justify-content: flex-end; align-items: center; height: 100%; position: relative; }
  .cm-bar div { width: 100%; border-radius: 6px 6px 0 0; background: var(--c); box-shadow: 0 0 20px -6px var(--c); height: 0; transition: height .9s cubic-bezier(.2,.9,.2,1); }
  .cm-bar span { font: 600 15px/1 var(--mono); color: #d1d7e0; margin-bottom: 6px; opacity: 0; transition: opacity .4s .4s; font-variant-numeric: tabular-nums; }
  .cm-bar.on span { opacity: 1; }
  .cm-bar small { position: absolute; bottom: -26px; font: 500 14px/1 var(--mono); color: #9198a1; white-space: nowrap; }
  .cm-guard { margin-top: 38px; display: flex; align-items: center; gap: 12px; font: 500 18px/1.3 var(--mono); color: #656c76; padding: 12px 16px; border-radius: 10px; border: 1px dashed #3d444d; transition: all .5s; }
  .cm-guard.on { color: #f0f6fc; border-color: #f85149; background: #f8514914; }
  .cm-route { position: absolute; left: 1180px; top: 730px; width: 630px; height: 264px; border-radius: 18px; background: #161b22e8; border: 1px solid #3d444d; overflow: hidden; opacity: 0; transition: border-color .6s, box-shadow .6s; }
  .active .cm-route { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .7s; }
  .cm-route.on { border-color: #ab7df8; box-shadow: 0 0 60px -20px #ab7df8; }
  .cm-route i { position: absolute; left: 30px; top: 22px; font: 600 16px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: #9198a1; font-style: normal; }
  .cm-route.on i { color: #d2a8ff; }
  .cm-route canvas { position: absolute; left: 0; top: -14px; width: 630px; height: 284px; }

  /* --- model routing --- */
  .mr .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #ab7df8, #4493f8, #3fb950, #ab7df8); }
  /* the run card starts centred, then glides left when the results arrive */
  .mr-hero { position: absolute; left: 110px; top: 350px; width: 1040px; height: 590px; transform: translateX(330px); transition: transform 1.1s cubic-bezier(.7,0,.2,1); }
  .mr.side .mr-hero { transform: none; }
  .mr-run { position: absolute; left: 0; top: 0; width: 1040px; height: 420px; border-radius: 18px; background: #0d1117f0; border: 1px solid #3d444d; overflow: hidden; opacity: 0;
    box-shadow: 0 30px 80px -30px #000, 0 0 70px -30px #ab7df8; }
  .active .mr-run { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .3s; }
  .mr-bar { display: flex; align-items: center; gap: 10px; height: 54px; padding: 0 16px 0 20px; background: #151b23; border-bottom: 1px solid #3d444d; font: 500 19px/1 var(--mono); color: #d1d7e0; }
  .mr-bar > i { width: 14px; height: 14px; border-radius: 50%; }
  .mr-wf { margin-left: 12px; }
  .mr-wf small { margin-left: 12px; font-size: 16px; color: #656c76; }
  .mr-gap { flex: 1; }
  .mr-model { font: 600 19px/1 var(--mono); padding: 8px 14px; border-radius: 8px; background: #0d1117; border: 1px solid #ab7df8; box-shadow: 0 0 30px -6px #ab7df8; color: #ff7b72; }
  .mr-model b { font-weight: 600; color: #d2a8ff; text-shadow: 0 0 12px #ab7df8; }
  /* run-card transitions run at the same slower pace as the router animation (SLOW in the slide) */
  .mr-text { position: absolute; left: 36px; top: 84px; width: 572px; font: 600 36px/1.3 "Mona Sans"; color: #e6edf3; text-wrap: balance; transition: opacity .44s; }
  .mr-hide { color: transparent; }
  .mr-w { border-radius: 6px; transition: color .53s, background .53s, box-shadow .53s; }
  .mr-w.k { --c: #f778ba; } .mr-w.s { --c: #39c5cf; } .mr-w.d { --c: #e3b341; }
  .mr-w.on { color: #fff; background: color-mix(in srgb, var(--c) 24%, transparent); box-shadow: inset 0 -3px 0 var(--c), 0 0 24px -8px var(--c); }
  .mr-beam { position: absolute; left: 30px; top: 70px; width: 3px; height: 190px; border-radius: 2px; background: #e2c5ff; box-shadow: 0 0 16px 4px #ab7df8; opacity: 0; pointer-events: none; transition: opacity .27s; }
  .mr-beam::before { content: ""; position: absolute; right: 3px; top: 0; width: 150px; height: 100%; background: linear-gradient(90deg, transparent, #ab7df838); }
  .mr-ghost { position: absolute; z-index: 3; font: 600 36px/1.3 "Mona Sans"; color: var(--c); text-shadow: 0 0 18px var(--c); white-space: nowrap; pointer-events: none; transform-origin: 0 0;
    transition: transform .89s cubic-bezier(.2,.9,.2,1), opacity .53s ease .44s; }
  .mr-chips { position: absolute; left: 36px; top: 300px; display: flex; gap: 12px; transition: opacity .44s; }
  .mr-chip { display: flex; align-items: baseline; gap: 8px; padding: 9px 15px; border-radius: 99px; border: 2px solid var(--c); background: color-mix(in srgb, var(--c) 14%, #0d1117);
    opacity: 0; transform: scale(.6); transition: opacity .53s, transform .89s cubic-bezier(.3,1.6,.4,1); }
  .mr-chip.on { opacity: 1; transform: none; box-shadow: 0 0 24px -8px var(--c); }
  .mr-chip i { font: 600 12px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: var(--c); font-style: normal; }
  .mr-chip b { font: 700 22px/1 "Mona Sans"; color: #f0f6fc; }
  .mr-out { opacity: 0 !important; }
  .mr-gauge { position: absolute; right: 26px; top: 76px; width: 380px; height: 270px; }
  .mr-track { fill: none; stroke: #ffffff12; stroke-width: 26; }
  .mr-z { fill: none; stroke-width: 26; opacity: .38; transition: opacity .71s, filter .71s; }
  .mr-z.on { opacity: 1; filter: drop-shadow(0 0 10px currentColor); }
  .mr-zl { font: 600 18px "Mona Sans"; text-anchor: middle; opacity: .45; transition: opacity .71s; }
  .mr-zl.on { opacity: 1; }
  .mr-needle path { fill: #f0f6fc; filter: drop-shadow(0 0 6px #ffffff88); }
  .mr-needle circle { fill: #0d1117; stroke: #f0f6fc; stroke-width: 3; }
  .mr-read { font: 700 22px var(--mono); letter-spacing: .12em; text-anchor: middle; fill: #9198a1; }
  .mr-read2 { font: 500 15px var(--mono); text-anchor: middle; fill: #9198a1; }
  .mr-why { position: absolute; right: 26px; top: 358px; width: 380px; text-align: center; font: 500 21px/1.2 "Mona Sans"; color: #d1d7e0; opacity: 0; transform: translateY(8px); transition: opacity .62s, transform .62s; }
  .mr-why.on { opacity: 1; transform: none; }
  /* split-flap board: every tile rattles through letters, then lands on the pick */
  .mr-flap { position: absolute; left: 0; top: 464px; display: flex; align-items: flex-end; gap: 24px; opacity: 0; }
  .active .mr-flap { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .5s; }
  .mr-arrow { font: 700 46px/64px var(--mono); color: #656c76; }
  .mr-field i { display: block; margin: 0 0 10px 2px; font: 700 18px/1 var(--mono); letter-spacing: .16em; text-transform: uppercase; color: #e6edf3; font-style: normal; }
  .mr-tiles { display: flex; gap: 4px; }
  .mr-t { position: relative; width: 44px; height: 64px; border-radius: 7px; background: linear-gradient(#262c36 0 50%, #151a21 50% 100%); box-shadow: inset 0 1px 0 #ffffff1a, 0 10px 20px -10px #000;
    font: 700 40px/64px var(--mono); text-align: center; color: #f0f6fc; white-space: pre; transition: opacity .4s; }
  .mr-t::after { content: ""; position: absolute; left: 0; right: 0; top: 31px; height: 2px; background: #05070a; }
  .mr-t.old { opacity: .25; }
  .mr-t.set { color: var(--c); text-shadow: 0 0 16px var(--c); animation: mr-thunk .44s cubic-bezier(.3,1.6,.4,1); }
  @keyframes mr-thunk { from { transform: scaleY(.3); } }
  .mr-proof, .mr-rep { position: absolute; left: 1210px; width: 600px; border-radius: 18px; background: #161b22e8; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000;
    opacity: 0; transform: translateY(40px); transition: opacity .6s, transform .9s cubic-bezier(.2,.9,.2,1); }
  .mr-proof.on, .mr-rep.on { opacity: 1; transform: none; }
  .mr-proof { top: 276px; padding: 24px 28px 22px; }
  .mr-pf-h { display: flex; align-items: center; gap: 14px; font: 700 26px/1 "Mona Sans"; color: #f0f6fc; }
  .mr-pf-h em { font: 600 13px/1 var(--mono); font-style: normal; letter-spacing: .16em; text-transform: uppercase; color: #3fb950; border: 1px solid #3fb95088; border-radius: 99px; padding: 6px 10px; }
  .mr-big { display: flex; align-items: center; gap: 22px; margin: 16px 0 8px; }
  .mr-big b { min-width: 280px; font: 700 96px/1 var(--mono); letter-spacing: -.05em; color: #3fb950; text-shadow: 0 0 40px #3fb95066; font-variant-numeric: tabular-nums; }
  .mr-big span { font: 600 26px/1.2 "Mona Sans"; color: #f0f6fc; }
  .mr-big small { display: block; margin-top: 8px; font: 500 18px/1 var(--mono); color: #9198a1; }
  .mr-strip { display: block; width: 544px; height: 150px; overflow: visible; }
  .mr-strip rect { transform: scaleY(0); transform-box: fill-box; transform-origin: 50% 100%; transition: transform .5s cubic-bezier(.2,.9,.2,1) calc(var(--i) * 45ms + 300ms); }
  .mr-proof.on .mr-strip rect { transform: none; }
  .mr-strip .pre { fill: #8957e5; } .mr-strip .post { fill: #3fb950; filter: drop-shadow(0 0 6px #3fb95088); }
  .mr-strip line { stroke: #ffffff40; stroke-width: 1.5; stroke-dasharray: 4 6; }
  .mr-strip line.mk { stroke: #d2a8ff; stroke-width: 2; stroke-dasharray: 6 5; }
  .mr-strip text { font: 600 14px var(--mono); fill: #9198a1; }
  .mr-strip text.mk { fill: #d2a8ff; }
  .mr-held { display: flex; gap: 26px; margin-top: 12px; font: 500 18px/1.2 "Mona Sans"; color: #d1d7e0; }
  .mr-held i { margin-right: 8px; font-style: normal; font-weight: 800; color: #3fb950; }
  .mr-held b { margin-left: 6px; font: 600 17px var(--mono); color: #f0f6fc; white-space: nowrap; }
  /* a week of runs, one square each, filled in like a sequencer */
  .mr-rep { top: 670px; padding: 22px 28px 20px; }
  .mr-rh { margin-bottom: 20px; font: 700 26px/1 "Mona Sans"; color: #f0f6fc; }
  .mr-week { display: grid; grid-template-columns: 22px repeat(24, 17px); gap: 4px; }
  .mr-week i { font: 700 16px/17px var(--mono); color: #e6edf3; font-style: normal; }
  .mr-week b { height: 17px; border-radius: 4px; background: #ffffff10; transition: background .35s, box-shadow .35s; }
  .mr-week b.t0 { background: #3fb950; box-shadow: 0 0 10px -3px #3fb950; }
  .mr-week b.t1 { background: #4493f8; box-shadow: 0 0 10px -3px #4493f8; }
  .mr-week b.t2 { background: #ab7df8; box-shadow: 0 0 10px -3px #ab7df8; }
  .mr-week b.hot { animation: mr-pop .4s cubic-bezier(.3,1.6,.4,1); }
  @keyframes mr-pop { from { transform: scale(.2); } }
  .mr-rf { display: flex; align-items: center; gap: 18px; margin-top: 18px; font: 600 16px/1 var(--mono); color: #d1d7e0; }
  .mr-rf > span { display: flex; align-items: center; gap: 8px; }
  .mr-rf > span::before { content: ""; width: 14px; height: 14px; border-radius: 3px; background: var(--c); }
  .mr-rf em { margin-left: auto; font-style: normal; color: #d1d7e0; }
  .mr-rf em b { font-size: 18px; color: #f0f6fc; font-variant-numeric: tabular-nums; }
  .mr-cv { position: absolute; left: 0; top: 0; width: 1920px; height: 1080px; pointer-events: none; }

  /* --- finale --- */
  .fin-bg { position: absolute; inset: 0; display: flex; flex-direction: column; justify-content: center; align-items: center; text-align: center; pointer-events: none; }
  .fin-bg h1 { margin: 0; font-size: 300px; line-height: .86; letter-spacing: -.045em; font-variation-settings: "wght" 800, "wdth" 112; white-space: nowrap;
    background: linear-gradient(100deg, #0fbf3e 0%, #5fed83 30%, #4493f8 65%, #ab7df8 100%); background-size: 200% 100%; -webkit-background-clip: text; background-clip: text; color: transparent;
    opacity: .2; animation: finShimmer 14s ease-in-out infinite alternate; }
  .fin-bg h1 + h1 { font-size: 210px; }
  @keyframes finShimmer { to { background-position: 100% 0; } }
  .fin-cap { position: absolute; left: 0; right: 0; bottom: 64px; text-align: center; font: 600 18px/1 var(--mono); letter-spacing: .3em; text-transform: uppercase; color: #9198a1; }
  .fin-cap b { color: #f0f6fc; font-weight: 700; }

  /* --- chrome --- */
  .progress { position: fixed; left: 0; bottom: 0; height: 5px; width: 100%; transform-origin: 0 50%;
    background: linear-gradient(90deg, #ab7df8, #4493f8, #3fb950); transition: transform .8s cubic-bezier(.7,0,.2,1); z-index: 5; }
  .dots { position: fixed; left: 28px; bottom: 22px; display: flex; gap: 12px; z-index: 5; }
  .dots button { width: 12px; height: 12px; padding: 0; border: 0; border-radius: 999px; background: #ffffff40; cursor: pointer;
    transition: width .5s cubic-bezier(.3,1.6,.4,1), background .3s; }
  .dots button.on { width: 40px; background: #f0f6fc; }
  .arrows { position: fixed; left: 16px; bottom: 10px; display: none; gap: 8px; z-index: 5; }
  .arrows button { width: 34px; height: 34px; padding: 7px; border: 1px solid #3d444d; border-radius: 8px; background: #151b23cc;
    color: #9198a1; cursor: pointer; transition: color .2s, border-color .2s; }
  .arrows button:hover { color: #f0f6fc; border-color: #9198a1; }
  .arrows button:disabled { opacity: .35; cursor: default; }
  .arrows svg { width: 100%; height: 100%; display: block; }
  body.compact-nav .dots { display: none; }
  body.compact-nav .arrows { display: flex; }
  .snd { position: fixed; right: 124px; bottom: 10px; width: 34px; height: 34px; padding: 6px; border: 1px solid #3d444d; border-radius: 8px;
    background: #151b23cc; color: #9198a1; cursor: pointer; z-index: 5; transition: color .2s, border-color .2s, background .2s; }
  .snd:hover { color: #f0f6fc; border-color: #9198a1; }
  .snd svg { width: 100%; height: 100%; display: block; }
  .snd .waves { transition: opacity .2s; }
  .snd .x { opacity: 0; transition: opacity .2s; }
  .snd.off .waves { opacity: 0; }
  .snd.off .x { opacity: 1; }
  .snd.flash { color: #f0f6fc; border-color: #f0f6fc; }
  .count { position: fixed; right: 26px; bottom: 18px; font: 600 15px/1 var(--mono); color: #9198a1; z-index: 5; letter-spacing: .1em; }
  .notes { position: fixed; left: 24px; right: 24px; bottom: 50px; max-width: 900px; margin: 0 auto; padding: 18px 22px;
    background: #151b23f0; border: 1px solid #3d444d; border-radius: 12px; color: #f0f6fc; font: 400 18px/1.45 "Mona Sans"; z-index: 6; display: none; }
  .notes.on { display: block; }

  /* --- any engine --- */
  .ae .slide-head .ae-agent .ch { background-image: linear-gradient(90deg, #ab7df8, #d2b8ff, #ab7df8); }
  .ae .slide-head .ae-prov-t .ch { background-image: linear-gradient(90deg, #f0883e, #ffc28a, #f0883e); }
  .ae-ed { position: absolute; left: 110px; top: 330px; width: 760px; border-radius: 16px; background: #0d1117f0; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000; overflow: hidden; opacity: 0; }
  .active .ae-ed { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .3s; }
  .ae-tab { display: flex; align-items: center; height: 50px; padding: 0 22px; background: #151b23; border-bottom: 1px solid #3d444d; font: 500 19px/1 var(--mono); color: #f0f6fc; }
  .ae-tab span { margin-left: auto; color: #656c76; font-size: 16px; letter-spacing: .1em; }
  .ae-code { padding: 14px 0 18px; font: 500 24px/36px var(--mono); }
  .ae-row { display: flex; white-space: pre; }
  .ae-row .no { width: 74px; padding-right: 26px; text-align: right; color: #656c76; flex: none; }
  .ae-row.hot { background: linear-gradient(90deg, color-mix(in srgb, var(--ec) 22%, transparent), transparent 80%); }
  .ae-val { color: var(--ec); text-shadow: 0 0 18px var(--ec); }
  .ae-caret { display: inline-block; width: 3px; height: 28px; margin-left: 2px; vertical-align: -5px; background: #f0f6fc; animation: ae-blink 1s steps(1) infinite; }
  @keyframes ae-blink { 50% { opacity: 0; } }
  .ae-arrow { position: absolute; left: 892px; top: 560px; width: 150px; text-align: center; font: 500 17px/1 var(--mono); color: #9198a1; opacity: 0; }
  .ae-arrow svg { display: block; width: 150px; height: 40px; margin-top: 12px; overflow: visible; }
  .ae-arrow path { fill: none; stroke: #9198a1; stroke-width: 3; stroke-dasharray: 10 8; animation: ae-flow 0.8s linear infinite; }
  @keyframes ae-flow { to { stroke-dashoffset: -18; } }
  .active .ae-arrow { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .7s; }
  .ae-provs { position: absolute; left: 1066px; top: 290px; width: 744px; display: flex; justify-content: space-between; opacity: 0; }
  .active .ae-provs { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .4s; }
  .ae-prov { display: flex; align-items: center; gap: 14px; width: 228px; height: 80px; padding: 14px 18px; border-radius: 16px; background: #161b22; border: 2px solid #3d444d; opacity: .5; transition: all .4s; }
  .ae-prov b { display: block; font: 700 25px/1 "Mona Sans"; color: #f0f6fc; }
  .ae-prov span { display: block; white-space: nowrap; margin-top: 8px; font: 500 15px/1 var(--mono); color: #9198a1; }
  .ae-prov.on { opacity: 1; border-color: var(--c); box-shadow: 0 0 44px -6px var(--c), inset 0 0 30px -14px var(--c); }
  .ae-prov.on b { color: var(--c); }
  .ae-logo { display: grid; place-items: center; flex: none; }
  .ae-logo svg { width: 100%; height: 100%; }
  .ae-prov .ae-logo { width: 40px; height: 40px; color: #f0f6fc; }
  .ae-prov.on .ae-logo { color: var(--c); }
  .ae-mono .ae-logo { width: 54px; height: 54px; }
  .ae-pick b .ae-logo { width: 22px; height: 22px; }
  .ae-toks { position: absolute; left: 1066px; top: 290px; width: 744px; height: 640px; pointer-events: none; }
  .ae-tokn { display: block; margin-top: 8px; font: normal 600 15px/1 var(--mono); color: var(--ec); white-space: nowrap; }
  .ae-run { position: absolute; left: 1066px; top: 410px; width: 744px; height: 490px; opacity: 0; }
  .active .ae-run { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .5s; }
  .ae-ring { position: absolute; border-radius: 26px; border: 2px solid var(--c); background: color-mix(in srgb, var(--c) 7%, transparent); }
  .ae-ring > b { position: absolute; left: 22px; top: 14px; font: 600 17px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: var(--c); }
  .ae-ring > i { position: absolute; right: 22px; top: 12px; font: normal 600 15px/1 var(--mono); color: #3fb950; letter-spacing: .08em; }
  .ae-core { position: absolute; left: 50%; top: 50%; width: 320px; height: 150px; margin: -75px 0 0 -160px; border-radius: 22px; background: #0d1117; border: 3px solid var(--ec);
    box-shadow: 0 0 var(--gl, 60px) -6px var(--ec), inset 0 0 40px -18px var(--ec); display: grid; grid-template-columns: 88px 1fr; align-items: center; gap: 20px; padding: 0 28px;
    transition: border-color .4s, box-shadow .4s; }
  .ae-core.swap { animation: ae-swap .55s cubic-bezier(.3,1.5,.4,1); }
  @keyframes ae-swap { 0% { transform: rotateX(0); } 45% { transform: rotateX(90deg) scale(.92); } 100% { transform: none; } }
  .ae-facts { position: absolute; left: 0; right: 0; top: 350px; display: flex; justify-content: center; gap: 14px; }
  .ae-facts span { padding: 7px 16px; border-radius: 99px; border: 1.5px solid #3fb95066; background: #3fb95014; color: #7ee787; font: 600 17px/1 var(--mono); letter-spacing: .04em; }
  .ae-facts b { font-weight: 800; }
  .ae-mono { display: grid; place-items: center; width: 88px; height: 88px; border-radius: 22px; background: var(--ec); color: #0d1117; font: 800 50px/1 "Mona Sans"; transition: background .4s; }
  .ae-core small { display: block; font: 500 16px/1 var(--mono); color: #9198a1; letter-spacing: .12em; text-transform: uppercase; }
  .ae-core strong { display: block; margin-top: 8px; font: 700 40px/1 "Mona Sans"; color: #f0f6fc; }
  .ae-picks { position: absolute; left: 110px; top: 930px; display: flex; gap: 18px; opacity: 0; }
  .active .ae-picks { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .9s; }
  .ae-pick { display: flex; align-items: center; gap: 12px; height: 54px; padding: 0 24px 0 8px; border-radius: 99px; border: 2px solid #3d444d; background: #161b22;
    font: 600 22px/1 var(--mono); color: #9198a1; transition: all .4s; }
  .ae-pick b { display: grid; place-items: center; width: 38px; height: 38px; border-radius: 99px; background: #3d444d; color: #0d1117; font: 800 20px/1 "Mona Sans"; transition: background .4s; }
  .ae-pick.on { color: #f0f6fc; border-color: var(--ec); box-shadow: 0 0 30px -6px var(--ec); }
  .ae-pick b { color: #9198a1; }
  .ae-pick.on b { background: var(--ec); color: #0d1117; }
  .ae-tag { position: absolute; left: 1000px; top: 944px; width: 880px; text-align: center; font: 600 30px/1 "Mona Sans"; color: #f0f6fc; opacity: 0; }
  .ae-tag em { font-style: normal; color: var(--ec); }
  .active .ae-tag { animation: rise .8s cubic-bezier(.2,.9,.2,1) both 1.1s; }

  /* --- enterprise ready --- */
  .er { --ec: #4493f8; }
  .er .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #4493f8, #a5d6ff, #d29922, #4493f8); }
  .er-tag { position: absolute; right: 110px; top: 118px; width: 600px; text-align: right; font: 500 30px/1.3 "Mona Sans"; color: #9198a1; opacity: 0; }
  .er-tag b { color: #f0f6fc; font-weight: 700; }
  .active .er-tag { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .5s; }
  .er-ed { position: absolute; left: 110px; top: 330px; width: 720px; border-radius: 16px; background: #0d1117f0; border: 1px solid #3d444d; box-shadow: 0 30px 80px -30px #000; overflow: hidden; opacity: 0; }
  .active .er-ed { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .3s; }
  .er-ed .ae-code { font-size: 21px; line-height: 26px; }
  .er-ed .ae-row { transition: background .4s; }
  .er-ed .ae-row.hot { background: linear-gradient(90deg, color-mix(in srgb, var(--ec) 24%, transparent), transparent 85%); }
  .er-ed .ae-row.lit { background: linear-gradient(90deg, #d2992255, transparent 85%); box-shadow: inset 4px 0 0 #d29922; }
  .er-arrow { position: absolute; left: 846px; top: 430px; width: 150px; text-align: center; font: 500 17px/1 var(--mono); color: #9198a1; opacity: 0; }
  .er-arrow svg { display: block; width: 150px; height: 40px; margin-top: 12px; overflow: visible; }
  .er-arrow path { fill: none; stroke: #9198a1; stroke-width: 3; stroke-dasharray: 10 8; animation: ae-flow .8s linear infinite; }
  .er-arrow small { display: block; margin-top: 10px; white-space: nowrap; font: 600 15px/1 var(--mono); color: #3fb950; letter-spacing: .06em; }
  .active .er-arrow { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .6s; }
  .er-card { position: absolute; width: 390px; height: 230px; padding: 24px 26px; border-radius: 20px; background: #161b22e8; border: 2px solid #3d444d; opacity: 0;
    transition: border-color .4s, box-shadow .4s, background .4s; }
  .active .er-card { animation: rise .8s cubic-bezier(.2,.9,.2,1) both calc(.5s + var(--i) * 120ms); }
  .er-card.on { border-color: var(--rc); background: color-mix(in srgb, var(--rc) 9%, #0d1117f0); box-shadow: 0 0 60px -14px var(--rc), inset 0 0 40px -24px var(--rc); }
  .er-card svg.ic { position: absolute; right: 24px; top: 22px; width: 56px; height: 56px; fill: none; stroke: #656c76; stroke-width: 2; stroke-linejoin: round; transition: stroke .4s; }
  .er-card.on svg.ic { stroke: var(--rc); filter: drop-shadow(0 0 10px var(--rc)); }
  .er-card i { display: block; font: normal 600 16px/1 var(--mono); letter-spacing: .16em; text-transform: uppercase; color: #9198a1; }
  .er-card.on i { color: var(--rc); }
  .er-card h3 { margin: 14px 56px 0 0; font: 700 34px/1 "Mona Sans"; white-space: nowrap; color: #f0f6fc; }
  .er-card code { display: block; margin-top: 16px; font: 500 20px/1 var(--mono); color: #9198a1; }
  .er-card.on code { color: var(--rc); }
  .er-st { position: absolute; left: 26px; bottom: 22px; font: 600 17px/1 var(--mono); color: #656c76; letter-spacing: .04em; }
  .er-st::before { content: "○ idle"; }
  .er-card.on .er-st::before { content: "◌ job queued…"; color: #d29922; }
  .er-card.run .er-st::before { content: "● agent job running"; color: var(--rc); }
  .er-card.eco .er-st { display: none; }
  .er-card.fw .er-st::before { content: "○ sandbox idle"; color: #656c76; }
  .er-card.fw.run .er-st::before { content: "● agent sandboxed"; color: var(--rc); }
  .er-fw { position: absolute; left: 26px; top: 138px; display: flex; gap: 6px; }
  .er-fw b { padding: 6px 9px; border-radius: 8px; border: 1.5px solid #3d444d; font: 500 15px/1 var(--mono); color: #656c76; white-space: nowrap; transition: all .3s; }
  .er-card.fw.run .er-fw b { border-color: #3fb950; color: #f0f6fc; background: #3fb95022; }
  .er-card.fw.run .er-fw b.no { border-color: var(--rc); color: var(--rc); background: color-mix(in srgb, var(--rc) 18%, transparent); text-decoration: line-through; }
  .er-steps { position: absolute; left: 26px; right: 26px; bottom: 20px; display: flex; gap: 6px; }
  .er-steps b { padding: 7px 8px; border-radius: 8px; border: 1.5px solid #3d444d; font: 500 14px/1 var(--mono); color: #656c76; white-space: nowrap; transition: all .3s; }
  .er-steps b::before { content: "○ "; }
  .er-steps b.up { border-color: var(--rc); color: #f0f6fc; background: color-mix(in srgb, var(--rc) 30%, transparent); box-shadow: 0 0 14px -2px var(--rc); }
  .er-steps b.up::before { content: "✓ "; color: var(--rc); }
  .er-card.otel .er-st::before { content: "○ waiting for spans"; color: #656c76; }
  .er-card.otel.run .er-st::before { content: "● trace exported"; color: var(--rc); }
  .er-spans { position: absolute; right: 26px; bottom: 20px; width: 150px; display: flex; flex-direction: column; gap: 5px; }
  .er-spans b { display: block; height: 9px; margin-left: var(--x); width: var(--w); border-radius: 3px; background: #3d444d; transform-origin: left; }
  .er-card.otel.run .er-spans b { background: var(--rc); box-shadow: 0 0 10px var(--rc); animation: er-span .5s cubic-bezier(.2,.9,.2,1) both calc(var(--j) * 140ms); }
  @keyframes er-span { from { transform: scaleX(0); } to { transform: none; } }
  .er-job.er-trace { width: 16px; height: 16px; margin: -8px 0 0 -8px; border-radius: 50%; background: #f0883e; box-shadow: 0 0 22px #f0883e, 0 0 4px #fff; }
  .er-job { position: absolute; width: 26px; height: 26px; margin: -13px 0 0 -13px; border-radius: 8px; background: var(--ec); box-shadow: 0 0 26px var(--ec), 0 0 6px #fff; opacity: 0; z-index: 3; pointer-events: none; }
  .er-chips { position: absolute; left: 110px; right: 110px; top: 880px; display: flex; align-items: center; gap: 12px; }
  .er-chips > i { font: normal 600 17px/1 var(--mono); letter-spacing: .2em; text-transform: uppercase; color: #d29922; margin-right: 6px; white-space: nowrap; opacity: 0; }
  .active .er-chips > i { animation: rise .7s cubic-bezier(.2,.9,.2,1) both 1s; }
  .er-chip { padding: 10px 16px; border-radius: 99px; border: 1.5px solid #d2992255; background: #d2992210; font: 500 19px/1 var(--mono); color: #c9d1d9; white-space: nowrap; opacity: 0; transition: all .35s; }
  .active .er-chip { animation: rise .7s cubic-bezier(.2,.9,.2,1) both calc(1.1s + var(--i) * 90ms); }
  .er-chip.lit { border-color: #d29922; background: #d2992230; color: #f0f6fc; box-shadow: 0 0 30px -6px #d29922; }
  .en .slide-head .l-grad .ch { background-image: linear-gradient(90deg, #ab7df8, #39c5cf, #3fb950, #ab7df8); }
  .en-card { position: absolute; top: 310px; height: 580px; box-sizing: border-box; padding: 30px 34px; border-radius: 22px; background: #0d1117f0;
    border: 2px solid color-mix(in srgb, var(--c) 70%, transparent); box-shadow: 0 0 60px -30px var(--c); opacity: 0; }
  .active .en-card { animation: rise .8s cubic-bezier(.2,.9,.2,1) both calc(.3s + var(--i) * .25s); }
  .en-card > i { font: 600 18px/1 var(--mono); letter-spacing: .18em; color: var(--c); font-style: normal; }
  .en-card h3 { margin: 14px 0 12px; font-size: 44px; line-height: 1; font-variation-settings: "wght" 700, "wdth" 100; }
  .en-pub { left: 110px; width: 480px; }
  .en-vault { left: 1010px; width: 800px; border-style: dashed; border-width: 3px; background: #08130cf0; transition: box-shadow .4s; }
  .en-vault.busy { box-shadow: 0 0 90px -20px var(--c), inset 0 0 80px -50px var(--c); }
  .en-lock { position: absolute; right: 26px; top: 26px; padding: 8px 14px; border-radius: 8px; background: var(--c); color: #0d1117;
    font: 600 16px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; }
  .en-facts { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 18px; }
  .en-facts span { padding: 7px 12px; border-radius: 99px; border: 1px solid #3d444d; font: 500 17px/1 var(--mono); color: #d1d7e0; }
  .en-facts b { color: var(--c); }
  .en-call { padding: 16px 18px; border-radius: 12px; background: #151b23; border: 1px solid #3d444d; font: 500 18px/1.35 var(--mono); color: #d1d7e0; }
  .en-call > b { display: block; color: #4493f8; font-weight: 700; margin-bottom: 6px; }
  .en-call small { display: block; margin-top: 8px; color: #656c76; font-size: 15px; letter-spacing: .12em; text-transform: uppercase; }
  .en-task { color: #f0f6fc; min-height: 50px; }
  .en-schema { color: #a5d6ff; }
  .en-call.flash { animation: en-flash .6s ease-out; }
  @keyframes en-flash { from { border-color: #4493f8; box-shadow: 0 0 30px -4px #4493f8; } }
  .en-log { margin: 18px 0 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 10px; }
  .en-log li { font: 500 19px/1.2 var(--mono); color: #d1d7e0; padding-left: 28px; position: relative; animation: rise .5s cubic-bezier(.2,.9,.2,1) both; }
  .en-log li::before { content: "✓"; position: absolute; left: 0; color: #3fb950; font-weight: 800; }
  .en-out { position: absolute; left: 34px; right: 34px; bottom: 26px; padding: 12px 16px; border-radius: 12px; border: 2px solid #ab7df8; background: #1c1430;
    font: 600 19px/1.2 "Mona Sans"; color: #f0f6fc; opacity: 0; transform: translateY(12px); transition: all .5s cubic-bezier(.2,.9,.2,1); }
  .en-out.show { opacity: 1; transform: none; box-shadow: 0 0 40px -10px #ab7df8; }
  .en-out code { font: 600 17px var(--mono); color: #ab7df8; margin-right: 8px; }
  .en-code { border-radius: 12px; background: #0d1117; border: 1px solid #263a2c; padding: 12px 0; font: 500 19px/30px var(--mono); color: #9198a1; }
  .en-line { display: flex; white-space: pre; padding-right: 16px; transition: background .3s, color .3s; }
  .en-line .no { width: 56px; padding-right: 18px; text-align: right; color: #3d444d; flex: none; }
  .en-line.hot { color: #f0f6fc; background: linear-gradient(90deg, #3fb95040, transparent 85%); }
  .en-status { margin-top: 14px; font: 600 19px/1 var(--mono); color: #3fb950; letter-spacing: .06em; min-height: 20px; }
  .en-vault.busy .en-status::after { content: "▍"; animation: ga-blink .7s steps(1) infinite; }
  .en-gate { position: absolute; left: 700px; top: 290px; width: 200px; height: 620px; text-align: center; opacity: 0; }
  .active .en-gate { animation: rise .8s cubic-bezier(.2,.9,.2,1) both .55s; }
  .en-gate > i { display: block; font: 600 17px/1.25 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #d29922; font-style: normal; }
  .en-gate small { position: absolute; left: 0; right: 0; bottom: 0; font: 500 16px/1.4 var(--mono); color: #9198a1; }
  .en-pillar { position: absolute; left: 92px; top: 60px; bottom: 60px; width: 16px; border-radius: 8px; background: linear-gradient(#d2992200, #d29922, #d2992200);
    box-shadow: 0 0 30px #d2992288; }
  .en-gnote { position: absolute; left: 50%; top: 360px; transform: translate(-50%, 10px); white-space: nowrap; padding: 10px 16px; border-radius: 10px;
    font: 700 19px/1 var(--mono); background: #0d1117; border: 2px solid; opacity: 0; transition: opacity .3s, transform .3s; z-index: 4; }
  .en-gnote.show { opacity: 1; transform: translate(-50%, 0); }
  .en-gnote.ok { color: #3fb950; border-color: #3fb950; }
  .en-gnote.no { color: #ff7b72; border-color: #f85149; box-shadow: 0 0 40px -6px #f85149; }
  .en-pk { position: absolute; left: 0; top: 582px; padding: 9px 16px; border-radius: 99px; white-space: nowrap; font: 600 17px/1 var(--mono); z-index: 3;
    background: #0d1117; border: 2px solid var(--c); color: var(--c); box-shadow: 0 0 26px -4px var(--c); }
  .en-pk.q { --c: #4493f8; }
  .en-pk.a { --c: #3fb950; }
  .en-pk.leak { --c: #f85149; }
  .en-pk.burst { animation: en-burst .6s ease-out forwards; }
  @keyframes en-burst { to { opacity: 0; filter: blur(8px); scale: 1.6; } }
  .en-ledger { position: absolute; left: 110px; top: 930px; width: 1700px; display: flex; align-items: center; gap: 22px; opacity: 0; }
  .active .en-ledger { animation: rise .8s cubic-bezier(.2,.9,.2,1) both 1s; }
  .en-ledger > i { font: 600 17px/1 var(--mono); letter-spacing: .14em; text-transform: uppercase; color: #9198a1; font-style: normal; white-space: nowrap; }
  .en-bar { flex: 1; height: 18px; border-radius: 9px; background: #21262d; overflow: hidden; }
  .en-fill { height: 100%; width: 100%; border-radius: 9px; background: linear-gradient(90deg, #3fb950, #39c5cf); transition: width .8s cubic-bezier(.2,.9,.2,1); }
  .en-fill.low { background: linear-gradient(90deg, #d29922, #f0883e); }
  .en-left { font: 600 20px/1 var(--mono); color: #d1d7e0; white-space: nowrap; }
  .en-left b { color: #f0f6fc; font-size: 26px; }
  .en-sens { display: flex; gap: 8px; }
  .en-sens span { padding: 7px 12px; border-radius: 8px; font: 500 16px/1 var(--mono); color: #656c76; border: 1px solid #30363d; }
  .en-sens span.on { color: #0d1117; background: #3fb950; border-color: #3fb950; font-weight: 700; }

  @media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation-duration: .001s !important; animation-iteration-count: 1 !important; } }
  `;
  document.head.append(Object.assign(document.createElement("style"), { textContent: css }));

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

  // ---------- slides ----------
  const slides = [];

  // Title
  {
    resetIndex();
    slides.push({
      sound: "title",
      palette: ["#0fbf3e", "#08872b", "#5fed83", "#0a241b"],
      notes: "GitHub Agentic Workflows. Coding agents running inside GitHub Actions, written in Markdown, with guardrails.",
      el: h("section", { class: "slide" }, h("div", { class: "title-wrap" }, h("h1", { class: "title" }, letters("GitHub"), letters("Agentic", "l-grad"), letters("Workflows", "l-out")))),
    });
  }

  // ---------- stage + chrome ----------
  const canvas = h("canvas", { class: "bg" });
  // Built on GitHub Actions
  {
    resetIndex();
    const ga = "GITHUB ACTIONS ✦ AGENTIC WORKFLOWS ✦ ";
    // One repository's Actions tab: a CI run (build, test, deploy) opens up and an agent step is inserted into the same pipeline.
    // [title, subtitle, file, trigger, result, jobs: [card, name, detail, ms, agentic, log lines]]
    const GA_RUNS = [
      [
        "CI",
        "build · test · deploy",
        "ci.yml",
        "on: push",
        "✓ All checks passed",
        [
          [0, "build", "npm run build", 1300, 0, ["Checked out acme/app@main", "> app@1.4.0 build", "Compiling 248 modules…", "✓ Built dist/ in 3.1s"]],
          [2, "test", "npm test", 1300, 0, ["PASS  src/api.test.ts", "PASS  src/ui.test.ts", "Tests: 132 passed, 132 total"]],
          [3, "deploy", "upload artifact", 900, 0, ["Uploading dist.zip (1.2 MB)", "✓ Artifact uploaded"]],
        ],
      ],
      [
        "Issue triage",
        "agentic workflow",
        "triage.md → triage.lock.yml",
        "on: issues",
        "✓ Issue #42 triaged",
        [
          [0, "activation", "role · sanitize", 1100, 0, ["Event: issues.opened #42", "Actor has triage role ✓", "Sanitized issue body → prompt"]],
          [1, "agent", "copilot · sandbox", 2600, 1, ["Firewall: 3 allowed domains", "Reading issue #42…", "Searching related code…", "Proposal: label bug + comment", "Wrote safe outputs (read-only token)"]],
          [2, "detection", "threat scan", 1200, 0, ["Scanning agent output…", "No prompt injection found", "No secrets found ✓"]],
          [3, "safe-outputs", "label · comment", 1100, 0, ["add_labels: bug", "add_comment on #42", "✓ Posted with scoped write token"]],
        ],
      ],
    ];
    // card x in the CI layout (agent card absent) and in the agentic layout
    const GA_XA = [272, 385, 497, 722],
      GA_XB = [160, 385, 610, 835],
      GA_W = 200,
      GA_Y = 268,
      GA_CY = GA_Y + 60;
    const gaSvg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    gaSvg.setAttribute("class", "ga-svg");
    gaSvg.setAttribute("viewBox", "0 0 1080 732");
    gaSvg.setAttribute("role", "img");
    gaSvg.setAttribute("aria-label", "Illustrative repository Actions tab: an agent step is inserted into a build, test and deploy workflow, turning it into an agentic workflow with activation, agent, detection and safe-outputs jobs");
    gaSvg.innerHTML = `
      <rect x="0" y="0" width="1080" height="732" fill="#0d1117"/>
      <path d="M40 40h16v24h-16z M44 46h8 M44 52h8" stroke="#9198a1" stroke-width="2" fill="none"/>
      <text x="72" y="60" class="ga-mono" font-size="26"><tspan fill="#4493f8">acme</tspan><tspan fill="#9198a1"> / </tspan><tspan fill="#4493f8" font-weight="700">app</tspan></text>
      <g font-size="20">
        <text x="40" y="118" fill="#9198a1">‹› Code</text><text x="150" y="118" fill="#9198a1">Issues</text>
        <text x="240" y="118" fill="#9198a1">Pull requests</text><text x="400" y="118" fill="#f0f6fc" font-weight="700">▶ Actions</text>
      </g>
      <rect x="392" y="134" width="114" height="4" rx="2" fill="#f78166"/>
      <line x1="0" y1="138" x2="1080" y2="138" stroke="#3d444d"/>
      <g class="ga-head">
        <circle class="ga-hdot" cx="52" cy="190" r="10"/>
        <text x="74" y="199" font-size="30" font-weight="700" class="ga-title"></text>
        <text x="74" y="232" font-size="18" class="ga-mono ga-file" fill="#9198a1"></text>
        <text x="1040" y="199" font-size="18" text-anchor="end" class="ga-mono ga-note" fill="#ab7df8">same runner · same logs · same checks</text>
      </g>
      <circle cx="96" cy="${GA_CY}" r="34" fill="#151b23" stroke="#3d444d" stroke-width="2"/>
      <text x="96" y="${GA_CY + 8}" font-size="22" text-anchor="middle" fill="#9198a1">⚡</text>
      <text x="96" y="${GA_Y + 128}" font-size="17" text-anchor="middle" class="ga-mono ga-on" fill="#9198a1"></text>
      ${[0, 1, 2, 3, 4].map(() => `<line class="ga-edge" y1="${GA_CY}" y2="${GA_CY}"/>`).join("")}
      ${[0, 1, 2, 3]
        .map(
          () => `<g class="ga-card">
          <text class="ga-tag" x="${GA_W / 2}" y="${GA_Y - 16}" text-anchor="middle" font-size="18" font-weight="700">✦ AGENTIC STEP</text>
          <rect class="box" x="0" y="${GA_Y}" width="${GA_W}" height="120" rx="12"/>
          <circle class="ring" cx="28" cy="${GA_Y + 34}" r="11"/>
          <path class="chk" d="M22 ${GA_Y + 34}l4 4 8-8" fill="none" stroke="#fff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>
          <g class="lbl">
            <text class="nm" x="50" y="${GA_Y + 42}" font-size="22" font-weight="700"></text>
            <text class="sub ga-mono" x="20" y="${GA_Y + 90}" font-size="16" fill="#9198a1"></text>
          </g>
        </g>`
        )
        .join("")}
      <rect x="40" y="452" width="1000" height="244" rx="10" fill="#010409" stroke="#3d444d"/>
      <text x="64" y="488" font-size="20" font-weight="700" class="ga-mono ga-logh"></text>
      <line x1="40" y1="504" x2="1040" y2="504" stroke="#21262d"/>
      ${[0, 1, 2, 3, 4, 5].map(i => `<text class="ga-mono ga-ln" x="64" y="${540 + i * 28}" font-size="18"></text>`).join("")}`;
    const gq = s => gaSvg.querySelector(s),
      gqa = s => [...gaSvg.querySelectorAll(s)];
    const gaCards = gqa(".ga-card"),
      gaEdges = gqa(".ga-edge"),
      gaLines = gqa(".ga-ln");
    gaCards[1].classList.add("ag");
    let gaT0 = 0,
      gaPhase = -1,
      gaKey = "",
      gaM = -1;
    const gaLabel = p => {
      const [title, sub, file, on, , jobs] = GA_RUNS[p];
      gaPhase = p;
      gaKey = "";
      gaSvg.classList.toggle("ga-aw", p === 1);
      gq(".ga-title").textContent = `${title} · ${sub}`;
      gq(".ga-file").textContent = file;
      gq(".ga-on").textContent = on;
      for (const [c, nm, sub2] of jobs) {
        gaCards[c].querySelector(".nm").textContent = nm;
        gaCards[c].querySelector(".sub").textContent = sub2;
      }
    };
    const gaEase = x => (x <= 0 ? 0 : x >= 1 ? 1 : x * x * (3 - 2 * x));
    // m: 0 = CI layout, 1 = agentic layout. The pipeline opens up, then the agent card drops into the gap.
    const gaLayout = m => {
      if (m === gaM) return;
      gaM = m;
      const s = gaEase(m / 0.55),
        a = gaEase((m - 0.4) / 0.6),
        x = GA_XA.map((xa, i) => xa + (GA_XB[i] - xa) * s),
        r = i => x[i] + GA_W;
      gaCards.forEach((c, i) => c.setAttribute("transform", `translate(${x[i]} ${i === 1 ? (1 - a) * -190 : 0})`));
      gaCards[1].style.opacity = a;
      // label cross-fade for cards that change role
      const lo = Math.abs(2 * Math.min(1, Math.max(0, (m - 0.15) / 0.7)) - 1);
      for (const i of [0, 2, 3]) gaCards[i].querySelector(".lbl").style.opacity = m > 0 && m < 1 ? lo : 1;
      // [x1, x2, opacity]: trigger→first, first→test (CI bridge), first→agent, agent→detection, test→last
      [
        [130, x[0], 1],
        [r(0), x[2], 1 - a],
        [r(0), x[1], a],
        [r(1), x[2], a],
        [r(2), x[3], 1],
      ].forEach(([x1, x2, o], i) => {
        gaEdges[i].setAttribute("x1", x1);
        gaEdges[i].setAttribute("x2", x2);
        gaEdges[i].style.opacity = o;
      });
    };
    // e: ms into the run of phase p (Infinity = finished, -1 = queued)
    const gaRender = (p, e) => {
      const jobs = GA_RUNS[p][5];
      let s = 0,
        cur = -1;
      const log = [],
        st = [0, 0, 0, 0];
      for (const [c, nm, , ms, ag, lines] of jobs) {
        const a = s;
        s += ms;
        if (e < a) continue;
        const n = e >= s ? lines.length : Math.min(lines.length, 1 + Math.floor(((e - a) / ms) * lines.length));
        log.push(...lines.slice(0, n).map(l => [nm, l]));
        if (e < s) ((st[c] = 1), (cur = [nm, ag]));
        else st[c] = 2;
      }
      const done = e >= s;
      const key = `${p}|${st.join("")}|${log.length}|${done}`;
      if (key === gaKey) return;
      gaKey = key;
      gaCards.forEach((c, i) => {
        c.classList.toggle("run", st[i] === 1);
        c.classList.toggle("ok", st[i] === 2);
      });
      const started = e >= 0;
      [started, st[0] === 2, st[0] === 2, st[1] === 2, st[2] === 2].forEach((on, i) => gaEdges[i].classList.toggle("on", on));
      gaSvg.classList.toggle("ga-done", done);
      gaSvg.classList.toggle("ga-running", started && !done);
      const lh = gq(".ga-logh");
      lh.textContent = done ? GA_RUNS[p][4] : cur ? `▾ ${cur[0]}` : "Queued…";
      lh.setAttribute("fill", done ? "#3fb950" : cur && cur[1] ? "#ab7df8" : "#f0f6fc");
      const tail = log.slice(-gaLines.length);
      gaLines.forEach((t, i) => {
        const l = tail[i];
        t.textContent = l ? `${l[0].padEnd(13)}${l[1]}` : "";
        t.setAttribute("fill", l && i === tail.length - 1 && !done ? "#f0f6fc" : "#9198a1");
      });
    };
    const GA_START = 1600,
      GA_HOLD = [1800, 2600],
      GA_MORPH = 1600;
    const gaLen = p => GA_RUNS[p][5].reduce((a, j) => a + j[3], 0) + GA_HOLD[p];
    slides.push({
      sound: "ship",
      trans: "up",
      enter() {
        gaT0 = performance.now();
        gaM = -1;
        gaSvg.classList.remove("ga-flip");
        gaLabel(0);
        gaLayout(0);
        gaRender(0, -1);
      },
      tick() {
        const u = performance.now() - gaT0 - GA_START,
          A = gaLen(0),
          B = gaLen(1),
          cyc = A + B + 2 * GA_MORPH;
        if (u < 0) return gaRender(0, -1);
        const c = u % cyc;
        let p = 0,
          e = c,
          m = 0;
        if (c >= A && c < A + GA_MORPH) {
          m = (c - A) / GA_MORPH;
          p = m < 0.5 ? 0 : 1;
          e = p ? -1 : Infinity;
        } else if (c >= A + GA_MORPH && c < A + GA_MORPH + B) ((p = 1), (e = c - A - GA_MORPH), (m = 1));
        else if (c >= A + GA_MORPH + B) {
          m = 1 - (c - A - GA_MORPH - B) / GA_MORPH;
          p = m >= 0.5 ? 1 : 0;
          e = p ? Infinity : -1;
        }
        if (p !== gaPhase) gaLabel(p);
        gaSvg.classList.toggle("ga-flip", m > 0 && m < 1);
        gaLayout(m);
        gaRender(p, e);
      },
      palette: ["#238636", "#1f6feb", "#8957e5", "#2ea043"],
      notes:
        "GitHub Agentic Workflows runs on the GitHub Actions you already use. The same repository runs a familiar build, test and deploy workflow; then an agent step is inserted into that pipeline, giving activation, agent, threat detection, and safe outputs. Same runner, same logs, same checks. This diagram is a synthetic, illustrative example, not a real repository or workflow run. See the public documentation for setup and availability.",
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "ga-bg" }, h("div", {}, ga + ga), h("div", {}, ga + ga)),
        h(
          "div",
          { class: "ga-copy" },
          h("div", { class: "badge" }, "● GITHUB ACTIONS"),
          h("h2", {}, letters("AI-powered", "l-grad"), h("br"), letters("workflows", "l-grad"), h("br"), letters("for GitHub"))
        ),
        h(
          "div",
          { class: "shot-wrap" },
          h(
            "div",
            { class: "shot" },
            h("div", { class: "bar" }, h("i", { style: { background: "#ff5f57" } }), h("i", { style: { background: "#febc2e" } }), h("i", { style: { background: "#28c840" } }), h("div", { class: "url" }, DOCS_URL.replace("https://", ""))),
            gaSvg,
            h("div", { class: "glare" })
          )
        )
      ),
    });
  }

  // Anatomy of a workflow: one synthetic .md dissected into front matter, schedule, tools, steps, safe outputs and prompt
  {
    resetIndex();
    const YAML = [
      "---",
      "on:",
      "  schedule: daily on weekdays",
      "tools:",
      "  github:",
      "    toolsets: [issues]",
      "  repo-memory:",
      "    branch-name: memory/docs-audit",
      "permissions: read-all",
      "steps:",
      "  - run: npm ci",
      "  - run: npm run lint -- --json > lint.json",
      "safe-outputs:",
      "  create-issue:",
      '    title-prefix: "[audit] "',
      "    max: 1",
      "---",
    ];
    const C = { fm: "#d1d7e0", sch: "#d29922", tl: "#4493f8", st: "#39c5cf", so: "#3fb950", pr: "#ab7df8" };
    const tok = (k, text) => h("span", { class: `an-tok an-tok-${k}`, "--c": C[k], style: { color: C[k] } }, text);
    const yamlLine = l => {
      if (l === "---") return [h("span", { style: { color: "#656c76" } }, l)];
      const [, ind, key, val] = l.match(/^(\s*(?:- )?)([\w-]+:)(.*)$/);
      return [ind, h("span", { style: { color: ind ? "#7ee787" : "#ff7b72" } }, key), h("span", { style: { color: "#a5d6ff" } }, val)];
    };
    const BODY = [
      [h("span", { style: { color: "#79c0ff", fontWeight: 700 } }, "# Docs audit")],
      ["Triage ", tok("st", "lint.json"), ". Skip findings already in"],
      [tok("tl", "memory"), " or ", tok("tl", "open issues"), ", then file"],
      [tok("so", "one issue"), " with a checklist of fixes."],
    ];
    const lines = [...YAML.map(yamlLine), ...BODY].map(kids => h("div", { class: "an-ln" }, kids));
    const LH = 30,
      CODE_TOP = 345;
    const CALLS = [
      ["sch", [2, 3], "Schedule", "on: schedule", "Fuzzy schedule in plain words; the compiler scatters it to a concrete cron."],
      ["tl", [4, 8], "Tools", "tools:", ["Read-only GitHub toolsets, plus ", h("code", {}, "repo-memory"), ": a git branch the agent keeps notes in across runs."]],
      ["st", [10, 12], "Steps", "deterministic", ["Plain shell runs before the agent and leaves ", h("code", {}, "lint.json"), " in the workspace."]],
      ["so", [13, 16], "Safe outputs", "max: 1", "The agent stays read-only. It can only request one issue; a separate job writes it."],
      ["pr", [18, 21], "Prompt", "markdown body", "Everything below the front matter is the agent prompt: plain-English instructions it follows on every run."],
    ];
    const CALL_TOP = [295, 440, 585, 730, 875];
    const bands = CALLS.map(([k, [a, b]]) => h("div", { class: "an-band", "--c": C[k], style: { top: `${18 + (a - 1) * LH}px`, height: `${(b - a + 1) * LH}px` } }));
    const calls = CALLS.map(([k, , title, chip, text], i) =>
      h("div", { class: "an-call", "--c": C[k], style: { top: `${CALL_TOP[i]}px` } }, h("div", { class: "an-num" }, String(i + 1).padStart(2, "0")), h("h3", {}, title, h("code", {}, chip)), h("p", {}, text))
    );
    const brace = (k, [a, b], label) => h("div", { class: "an-brace", "--c": C[k], style: { top: `${CODE_TOP + (a - 1) * LH + 2}px`, height: `${(b - a + 1) * LH - 4}px` } }, h("span", {}, label));
    const braces = [brace("fm", [1, 17], "Front matter"), brace("pr", [18, 21], "Prompt")];
    const botSvg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    botSvg.setAttribute("viewBox", "0 0 24 24");
    const bot = h("div", { class: "an-bot", "--c": C.pr, style: { top: `${18 + 17 * LH}px` } }, botSvg, "Agent prompt");
    const lockSvg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    lockSvg.setAttribute("viewBox", "0 0 24 24");
    lockSvg.innerHTML = '<path d="M12 1.5 3 5v6.5c0 5.2 3.8 9.6 9 11 5.2-1.4 9-5.8 9-11V5L12 1.5Zm0 6a2.5 2.5 0 0 1 1.25 4.67V15.5h-2.5v-3.33A2.5 2.5 0 0 1 12 7.5Z"/>';
    const lock = h("div", { class: "an-bot", "--c": C.fm, style: { top: "18px" } }, lockSvg, "Deterministic security config");
    const cc = h("div", { class: "an-cc" }, h("div", { class: "term" }, h("b", {}, "$ "), "gh aw compile"), h("div", { class: "arr" }), h("div", { class: "lockf" }, "🔒 docs-audit.lock.yml", h("small", {}, "hardened Actions YAML")));
    const wires = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    wires.setAttribute("class", "an-wires");
    wires.setAttribute("viewBox", "0 0 1920 1080");
    wires.innerHTML = CALLS.map(([k, [a, b]], i) => {
      const ry = CODE_TOP + (a - 1) * LH + ((b - a + 1) * LH) / 2,
        cy = CALL_TOP[i] + 63;
      return `<path style="--c:${C[k]}" d="M 984 ${ry} H 1050 C 1095 ${ry} 1095 ${cy} 1140 ${cy}"/><circle style="--c:${C[k]}" cx="984" cy="${ry}" r="6"/>`;
    }).join("");
    const paths = [...wires.querySelectorAll("path")],
      dots = [...wires.querySelectorAll("circle")];
    const toks = { tl: [...lines[19].querySelectorAll(".an-tok-tl")], st: [lines[18].querySelector(".an-tok-st")], so: [lines[20].querySelector(".an-tok-so")] };
    // reveal order: front matter brace, then each callout (the prompt callout also brings the body brace)
    const REVEAL = 0.6,
      GAP = 1,
      CYCLE = REVEAL + GAP * 6,
      HOT = 2.4;
    let t0 = 0,
      seen = -1,
      skip = false;
    const lit = (idx, on) => {
      if (idx === 0) {
        [braces[0], lock, cc].forEach(e => e.classList.toggle("on", on));
        [0, 8, 16].forEach(n => lines[n].classList.toggle("on", on));
        return;
      }
      const [, [a, b]] = CALLS[idx - 1];
      for (let n = a - 1; n < b; n++) lines[n].classList.toggle("on", on);
      [bands, calls, paths, dots].forEach(list => list[idx - 1].classList.toggle("on", on));
      if (idx === 5) [braces[1], bot].forEach(e => e.classList.toggle("on", on));
    };
    slides.push({
      sound: "ship",
      palette: ["#d29922", "#4493f8", "#3fb950", "#8957e5"],
      notes:
        "Anatomy of an agentic workflow, using a synthetic docs-audit example. Everything between the --- fences is front matter: a deterministic security configuration (triggers, permissions, pre-agent steps, allowed writes) that gh aw compile turns into a hardened docs-audit.lock.yml Actions workflow, independent of the model. The schedule is written in plain words, daily on weekdays, and the compiler scatters it to a concrete cron. The tools block grants read-only GitHub issue toolsets and repo-memory, a git branch where the agent keeps notes between runs. Steps are plain deterministic shell that runs before the agent, here producing lint.json. Safe outputs keep the agent read-only: it can only ask for one issue, which a separate job validates and writes. Below the fences, the markdown body is the agent prompt, natural-language instructions the coding agent follows on every run.",
      enter() {
        t0 = 0;
        seen = -1;
        skip = false;
        if (!botSvg.firstChild) botSvg.innerHTML = OCTI.copilot.map(d => `<path d="${d}"/>`).join("");
        [bot, lock, cc].forEach(e => e.classList.remove("on", "hot"));
        [...lines, ...bands, ...calls, ...paths, ...dots, ...braces, ...Object.values(toks).flat()].forEach(e => e.classList.remove("on", "hot"));
      },
      onNext() {
        if (seen >= 5) return false;
        skip = true;
        return true;
      },
      tick(t) {
        if (!t0) t0 = t;
        if (skip) ((skip = false), (t0 = Math.min(t0, t - CYCLE * 1000)));
        const e = (t - t0) / 1000;
        const n = Math.min(5, Math.floor((e - REVEAL) / GAP));
        while (seen < n) {
          seen++;
          lit(seen, true);
          sfx.pop(seen * 2);
        }
        const hot = e >= CYCLE ? Math.floor((e - CYCLE) / HOT) % 6 : -1;
        braces[0].classList.toggle("hot", hot === 0);
        lock.classList.toggle("hot", hot === 0);
        cc.classList.toggle("hot", hot === 0);
        braces[1].classList.toggle("hot", hot === 5);
        bot.classList.toggle("hot", hot === 5);
        [bands, calls, paths].forEach(list => list.forEach((el, i) => el.classList.toggle("hot", hot === i + 1)));
        ["tl", "st", "so"].forEach((k, i) => toks[k].forEach(el => el.classList.toggle("hot", hot === i + 2)));
      },
      el: h(
        "section",
        { class: "slide an" },
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Agent as code"), h("h2", { class: "pf-h" }, letters("Anatomy of a "), letters("workflow", "l-grad"))),
        h("div", { class: "an-file" }, h("div", { class: "file" }, "📄 .github/workflows/docs-audit.md"), h("div", { class: "an-code" }, bands, lines, lock, bot)),
        braces,
        wires,
        calls,
        cc
      ),
    });
  }

  // Turn Intent to Automation: prompt → markdown → issue (from the gh-aw hero)
  {
    resetIndex();
    const PROMPT = "Create a daily agent that removes code duplication";
    const MD = [
      ["---", "#656c76"],
      ["on: ", "#ff7b72", "daily", "#a5d6ff"],
      ["safe-outputs:", "#ff7b72"],
      ["  create-pull-request:", "#7ee787"],
      ["---", "#656c76"],
      ["Find duplicated code. Extract a", "#f0f6fc"],
      ["shared helper and keep tests green.", "#f0f6fc"],
    ];
    const MDLEN = MD.reduce((n, l) => n + l[0].length + (l[2] || "").length + 1, 0);
    const msg = h("div", { class: "msg" }),
      reply = h("div", { class: "reply" }, "✓ Added ", h("code", {}, "daily-dedup.md"), " and opened a pull request.");
    const pre = h("pre", {}),
      ypre = h("pre", {});
    // what gh aw compile emits: a hardened, pinned GitHub Actions workflow
    const YML = [
      ["name: Daily Dedup", "#f0f6fc"],
      ["on:", "#ff7b72"],
      ["  schedule:", "#ff7b72"],
      ['    - cron: "13 8 * * *"', "#a5d6ff"],
      ["permissions: read-all", "#7ee787"],
      ["jobs:", "#ff7b72"],
      ["  agent:", "#7ee787"],
      ["   steps:", "#ff7b72"],
      ["    - uses: checkout@08eba0b", "#d1d7e0"],
      ["    - uses: awf@4f2c91e", "#d1d7e0"],
      ["  safe_outputs:", "#7ee787"],
      ["    needs: [agent, threat]", "#d1d7e0"],
      ["    # … 900 more lines", "#656c76"],
    ];
    const go = h("div", { class: "go" }, "⚙ Go compiler · md → yml", h("span", {}, "deterministic: same .md, same .lock.yml"));
    const n1 = h("b", {}, "0"),
      n2 = h("b", {}, "0"),
      n3 = h("b", {}, "0");
    const lis = [h("li", {}, n1, " duplicate blocks found"), h("li", {}, h("b", { style: { color: "#3fb950" } }, "+", n2), " / ", h("b", { style: { color: "#f85149" } }, "−", n3), " lines"), h("li", {}, h("b", {}, "✓"), " all tests pass")];
    const cards = [
      h("div", { class: "pf-card pf-prompt" }, h("div", { class: "cap" }, "01 · Prompt"), h("div", { class: "sb" }, "9:41", h("span", {}, "▂▄▆ ▮")), h("div", { class: "you" }, "Copilot"), msg, reply),
      h("div", { class: "pf-card pf-md" }, h("div", { class: "cap" }, "02 · Markdown"), h("div", { class: "file" }, "📄 .github/workflows/daily-dedup.md"), pre),
      h("div", { class: "pf-card pf-yml" }, h("div", { class: "cap" }, "03 · Lock file"), h("div", { class: "file" }, "⚙ daily-dedup.lock.yml"), ypre, go),
      h("div", { class: "pf-card pf-issue" }, h("div", { class: "cap" }, "04 · Pull request"), h("h4", {}, "Extract shared parser helper ", h("small", {}, "#241"), h("span", { class: "open" }, "⇄ Open")), h("ul", {}, lis)),
    ];
    const links = [
      h("div", { class: "pf-link", style: { left: "530px", width: "40px", "--a": "#ab7df8", "--b": "#4493f8" } }),
      h("div", { class: "pf-link", style: { left: "1050px", width: "40px", "--a": "#4493f8", "--b": "#d29922" } }),
      h("div", { class: "pf-link", style: { left: "1390px", width: "40px", "--a": "#d29922", "--b": "#3fb950" } }),
    ];
    const tags = [
      h("div", { class: "pf-tag", style: { left: "390px" } }, h("em", {}, "Copilot writes it"), "plain markdown + frontmatter"),
      h("div", { class: "pf-tag", style: { left: "910px" } }, h("em", {}, "gh aw compile"), "deterministic Go · md → yml"),
      h("div", { class: "pf-tag", style: { left: "1250px" } }, h("em", {}, "Next morning, 08:00"), "runs in GitHub Actions"),
    ];
    const caret = () => h("span", { class: "pf-caret" });
    const RC = 2 * Math.PI * 40,
      ttaT = h("b", {}, "0:00"),
      ttaSvg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    ttaSvg.setAttribute("viewBox", "0 0 96 96");
    ttaSvg.innerHTML = `<circle class="bg" cx="48" cy="48" r="40"/><circle class="fg" cx="48" cy="48" r="40" stroke-dasharray="${RC}" stroke-dashoffset="${RC}"/>`;
    const tta = h("div", { class: "pf-tta" }, ttaSvg, h("div", {}, h("i", {}, "Time to agent"), ttaT, h("small", {}, "min")));
    let t0 = 0,
      now = 0,
      snd = { p: 0, m: -2, go: false, card: [] };
    // → / space fast-forwards to the next stage: prompt sent, markdown written, lock file compiled, pull request done
    const MD_END = 4.2 + MDLEN / 45,
      YML_END = MD_END + 0.8 + YML.length / 22,
      STAGES = [3.2, MD_END + 0.5, YML_END + 0.5, YML_END + 2.6];
    const renderMd = k => {
      pre.replaceChildren();
      for (const [a, ca, b, cb] of MD) {
        if (k <= 0) break;
        const sa = a.slice(0, k);
        k -= sa.length;
        pre.append(h("span", { style: { color: ca } }, sa));
        if (b && k > 0) {
          const sb = b.slice(0, k);
          k -= sb.length;
          pre.append(h("span", { style: { color: cb } }, sb));
        }
        if (k > 0) {
          pre.append("\n");
          k--;
        }
      }
      pre.append(caret());
    };
    slides.push({
      sound: "typing",
      trans: "up",
      palette: ["#8957e5", "#1f6feb", "#238636", "#8957e5"],
      notes:
        "Turn Intent to Automation: ask your coding agent in plain language, it writes a markdown workflow, then gh aw compile — a deterministic Go compiler, no AI involved — turns the .md into a hardened .lock.yml Actions workflow with pinned actions, a firewall and safe-output jobs. Same markdown in, same YAML out, so it is reviewable and diffable, and the next morning a pull request removing duplicated code shows up for review. Time to agent: about 1 minute from prompt to a running workflow. This mirrors the gh-aw hero page.",
      enter() {
        t0 = 0;
        snd = { p: 0, m: -2, go: false, card: [] };
        [...cards, ...links, ...tags, reply, go, ...lis].forEach(e => e.classList.remove("on"));
        msg.replaceChildren(caret());
        pre.replaceChildren();
        ypre.replaceChildren();
      },
      onNext() {
        if (!t0) return false;
        const e = (now - t0) / 1000,
          next = STAGES.find(s => s > e + 0.05);
        if (next === undefined) return false;
        t0 = now - next * 1000;
        return true;
      },
      tick(t) {
        if (!t0) t0 = t;
        now = t;
        const e = (t - t0) / 1000,
          on = (el, at) => el.classList.toggle("on", e >= at);
        on(cards[0], 0.3);
        const pk = Math.max(0, Math.min(PROMPT.length, Math.floor((e - 0.8) * 30)));
        if (pk > snd.p) {
          if (PROMPT[pk - 1] !== " ") sfx.key();
          snd.p = pk;
        }
        msg.replaceChildren(PROMPT.slice(0, pk), caret());
        on(reply, 3.2);
        on(links[0], 3.6);
        on(tags[0], 3.8);
        on(cards[1], 3.8);
        if (e >= 4.2) {
          const mk = Math.floor((e - 4.2) * 45);
          renderMd(mk);
          if (mk < MDLEN && mk >= snd.m + 2) {
            snd.m = mk;
            sfx.key(0.6);
          }
        }
        const mdEnd = MD_END;
        on(links[1], mdEnd + 0.3);
        on(tags[1], mdEnd + 0.5);
        on(cards[2], mdEnd + 0.5);
        const yk = Math.max(0, Math.min(YML.length, Math.floor((e - mdEnd - 0.8) * 22))); // compiles in a blink
        if (ypre.childElementCount !== yk) ypre.replaceChildren(...YML.slice(0, yk).flatMap(([l, c]) => [h("span", { style: { color: c } }, l), "\n"]));
        const ymlEnd = YML_END;
        on(go, ymlEnd + 0.1);
        if (go.classList.contains("on") && !snd.go) {
          snd.go = true;
          sfx.compile();
        }
        cards.forEach((c, i) => {
          const v = c.classList.contains("on");
          if (v && !snd.card[i]) sfx.pop(i * 2);
          snd.card[i] = v;
        });
        on(links[2], ymlEnd + 0.5);
        on(tags[2], ymlEnd + 0.7);
        on(cards[3], ymlEnd + 0.8);
        lis.forEach((li, i) => on(li, ymlEnd + 1.3 + i * 0.35));
        const c = Math.min(1, Math.max(0, (e - ymlEnd - 1.3) / 1.2)),
          ez = 1 - (1 - c) ** 3;
        const tu = Math.min(1, Math.max(0, (e - 0.3) / (ymlEnd + 0.5))),
          sec = Math.round(60 * tu);
        ttaT.textContent = `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, "0")}`;
        ttaSvg.querySelector(".fg").setAttribute("stroke-dashoffset", RC * (1 - tu));
        tta.classList.toggle("done", tu >= 1);
        n1.textContent = Math.round(4 * ez);
        n2.textContent = Math.round(38 * ez);
        n3.textContent = Math.round(212 * ez);
      },
      el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "How it works"), h("h2", { class: "pf-h" }, letters("Turn Intent to "), letters("Automation", "l-grad"))), tta, links, tags, cards),
    });
  }

  // Supply chain hardening: the compiler threat detection spec as concentric layers around the lock file
  {
    resetIndex();
    const CX = 975,
      CY = 650,
      CORE = 92;
    // outermost layer first: attacks pierce until the layer whose rules catch them
    const layers = [
      ["Trigger admission", "fork PRs · PR target · bot sync", "CTR-008 · 027 · 034", "#d29922"],
      ["Expression safety", "untrusted ${{ }} → env vars", "CTR-006 · 010 · 026", "#f778ba"],
      ["Pinned supply chain", "SHA pins · manifest drift", "CTR-002 · 016 · 018", "#ab7df8"],
      ["Package & arg hygiene", "install scripts · arg injection", "CTR-013 · 014 · 022", "#79c0ff"],
      ["Least privilege", "read-only token · safe outputs", "CTR-001 · 005 · 015", "#4493f8"],
      ["Secret & content scans", "secret sinks · markdown · imports", "CTR-007 · 017 · 020", "#3fb950"],
    ].map(([title, sub, ids, c], i) => ({ title, sub, ids, c, r: 330 - i * 40, flash: 0, n: 0, rot: Math.random() * 6 }));
    const attacks = [
      [0, "fork PR pwn request"],
      [0, "pull_request_target + secrets"],
      [0, "spoofed bot sync"],
      [1, "${{ github.event.issue.title }}"],
      [1, "timeout-minutes: ${{ … }}"],
      [1, "multiline expression"],
      [2, "tag moved: @v4"],
      [2, "unpinned action"],
      [2, "new secret vs manifest"],
      [3, "npm postinstall"],
      [3, "--upload-pack=evil"],
      [3, "-rf as package name"],
      [4, "contents: write"],
      [4, "direct push to main"],
      [4, "labels: *"],
      [5, "echo ${{ secrets.TOKEN }}"],
      [5, "hidden prompt in markdown"],
      [5, "conditional import swap"],
    ];
    const rows = layers.map((l, i) => h("div", { class: "ch-row", "--c": l.c, "--i": i }, h("i", {}, String(i + 1).padStart(2, "0")), h("b", {}, l.title), h("em", {}, "0"), h("span", {}, l.sub, h("br"), l.ids)));
    const line = (...parts) =>
      h(
        "span",
        { class: parts[0] === "g" ? "ch-g" : "" },
        parts.filter(p => p !== "g").map(([cls, t]) => (cls ? h("span", { class: cls, style: { display: "inline" } }, t) : t))
      );
    const md = h(
      "div",
      { class: "ch-file", "--c": "#9198a1", "--i": 0, style: { top: "296px" } },
      h("div", { class: "cm-tab" }, "triage.md", h("span", { class: "sv" }, "AUTHORED")),
      h(
        "pre",
        {},
        line([null, "---"]),
        line(["ch-k", "on"], [null, ":"]),
        line(["ch-k", "  issues"], [null, ": "], ["ch-v", "{ types: [opened] }"]),
        line(["ch-k", "permissions"], [null, ": "], ["ch-v", "read-all"]),
        line(["ch-k", "strict"], [null, ": "], ["ch-v", "true"]),
        line(["ch-k", "safe-outputs"], [null, ":"]),
        line(["ch-k", "  add-labels"], [null, ": "], ["ch-v", "{ allowed: [bug] }"]),
        line([null, "---"]),
        line(["ch-p", "Triage issue #${{ github.event.issue.number }}"]),
        line(["ch-p", "and label it."])
      )
    );
    const cmd = h("div", { class: "ch-cmd", style: { top: "640px" } }, h("b", {}, "$"), "gh aw compile --strict", h("b", {}, "↓"));
    const lock = h(
      "div",
      { class: "ch-file", "--c": "#3fb950", "--i": 1, style: { top: "684px" } },
      h("div", { class: "cm-tab" }, "triage.lock.yml", h("span", { class: "sv" }, "HARDENED")),
      h(
        "pre",
        {},
        line("g", ["ch-k", "uses"], [null, ": "], ["ch-v", "actions/checkout@de0fac2"], ["ch-c", " # v6"]),
        line("g", ["ch-k", "permissions"], [null, ": "], ["ch-v", "{ issues: read }"]),
        line("g", ["ch-k", "env"], [null, ": "], ["ch-v", "{ ISSUE_NUMBER: ${{ … }} }"]),
        line("g", ["ch-k", "persist-credentials"], [null, ": "], ["ch-v", "false"]),
        line("g", ["ch-k", "needs"], [null, ": "], ["ch-v", "[agent, detection]"]),
        line("g", ["ch-k", "timeout-minutes"], [null, ": "], ["ch-v", "20"])
      )
    );
    const cv = h("canvas", { class: "ch-cv", width: 1920, height: 1080 });
    const ctx = cv.getContext("2d");
    let shots = [],
      bits = [],
      pops = [],
      flow = [],
      last = 0,
      spawn = 0,
      total = 0,
      deck = [];
    const draw = t => {
      ctx.clearRect(0, 0, 1920, 1080);
      // compile path: triage.md → core → triage.lock.yml
      ctx.setLineDash([4, 10]);
      ctx.lineWidth = 2;
      ctx.strokeStyle = "#ffffff22";
      ctx.beginPath();
      ctx.moveTo(630, 470);
      ctx.quadraticCurveTo(800, 470, CX - CORE, CY - 20);
      ctx.moveTo(CX - CORE, CY + 20);
      ctx.quadraticCurveTo(800, 830, 630, 830);
      ctx.stroke();
      ctx.setLineDash([]);
      for (const f of flow) {
        const p = f.p,
          q = 1 - p;
        let x, y, col;
        if (f.out) {
          x = q * q * (CX - CORE) + 2 * q * p * 800 + p * p * 630;
          y = q * q * (CY + 20) + 2 * q * p * 830 + p * p * 830;
          col = "#3fb950";
        } else {
          x = q * q * 630 + 2 * q * p * 800 + p * p * (CX - CORE);
          y = q * q * 470 + 2 * q * p * 470 + p * p * (CY - 20);
          col = "#9198a1";
        }
        ctx.fillStyle = col;
        ctx.shadowColor = col;
        ctx.shadowBlur = 10;
        ctx.fillRect(x - 4, y - 4, 8, 8);
      }
      ctx.shadowBlur = 0;
      // layers: rotating dashed shells that flare when they catch something
      layers.forEach((l, i) => {
        ctx.save();
        ctx.translate(CX, CY);
        ctx.rotate(l.rot + t * 0.00012 * (i % 2 ? -1 : 1) * (1 + i * 0.25));
        ctx.strokeStyle = l.c;
        ctx.globalAlpha = 0.28 + l.flash * 0.72;
        ctx.lineWidth = 3 + l.flash * 4;
        ctx.shadowColor = l.c;
        ctx.shadowBlur = 8 + l.flash * 30;
        ctx.setLineDash([l.r * 0.42, 14 + i * 3]);
        ctx.beginPath();
        ctx.arc(0, 0, l.r, 0, Math.PI * 2);
        ctx.stroke();
        ctx.restore();
        const a = -Math.PI / 2 - 0.62;
        const bx = CX + Math.cos(a) * l.r,
          by = CY + Math.sin(a) * l.r;
        ctx.globalAlpha = 1;
        ctx.fillStyle = "#0d1117";
        ctx.beginPath();
        ctx.arc(bx, by, 15, 0, 7);
        ctx.fill();
        ctx.strokeStyle = l.c;
        ctx.lineWidth = 2;
        ctx.stroke();
        ctx.fillStyle = l.c;
        ctx.font = "700 14px 'Monaspace Neon', ui-monospace, Menlo, monospace";
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillText(String(i + 1), bx, by + 1);
      });
      ctx.setLineDash([]);
      // core: the hardened lock file
      const pulse = 0.5 + 0.5 * Math.sin(t / 420);
      const g = ctx.createRadialGradient(CX, CY, 10, CX, CY, CORE + 40);
      g.addColorStop(0, "#3fb95055");
      g.addColorStop(0.7, "#3fb95018");
      g.addColorStop(1, "#3fb95000");
      ctx.fillStyle = g;
      ctx.beginPath();
      ctx.arc(CX, CY, CORE + 40, 0, 7);
      ctx.fill();
      ctx.fillStyle = "#0d1117";
      ctx.strokeStyle = "#3fb950";
      ctx.lineWidth = 3;
      ctx.shadowColor = "#3fb950";
      ctx.shadowBlur = 18 + pulse * 18;
      ctx.beginPath();
      ctx.arc(CX, CY, CORE, 0, 7);
      ctx.fill();
      ctx.stroke();
      ctx.shadowBlur = 0;
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillStyle = "#3fb950";
      ctx.font = "600 15px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      ctx.fillText(".lock.yml", CX, CY - 34);
      ctx.fillStyle = "#f0f6fc";
      ctx.font = "800 46px 'Mona Sans', sans-serif";
      ctx.fillText(String(total), CX, CY + 4);
      ctx.fillStyle = "#9198a1";
      ctx.font = "600 13px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      ctx.fillText("BLOCKED", CX, CY + 40);
      // incoming attacks
      ctx.textBaseline = "middle";
      for (const s of shots) {
        const x = CX + Math.cos(s.a) * s.d,
          y = CY + Math.sin(s.a) * s.d;
        ctx.globalAlpha = Math.min(1, (s.d0 - s.d) / 50 + 0.15);
        ctx.fillStyle = "#ff7b72";
        ctx.shadowColor = "#f85149";
        ctx.shadowBlur = 16;
        ctx.beginPath();
        ctx.arc(x, y, 7, 0, 7);
        ctx.fill();
        ctx.strokeStyle = "#f8514966";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.moveTo(x, y);
        ctx.lineTo(x + Math.cos(s.a) * 40, y + Math.sin(s.a) * 40);
        ctx.stroke();
        ctx.shadowBlur = 0;
        ctx.font = "500 17px 'Monaspace Neon', ui-monospace, Menlo, monospace";
        const right = Math.cos(s.a) >= 0;
        ctx.textAlign = right ? "left" : "right";
        ctx.fillText(s.label, x + (right ? 16 : -16), y - 16);
      }
      ctx.globalAlpha = 1;
      for (const b of bits) {
        ctx.globalAlpha = Math.max(0, b.life);
        ctx.fillStyle = b.c;
        ctx.fillRect(b.x - 2, b.y - 2, 4, 4);
      }
      ctx.textAlign = "center";
      ctx.font = "700 16px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      for (const p of pops) {
        ctx.globalAlpha = Math.max(0, Math.min(1, p.life * 2));
        ctx.fillStyle = p.c;
        ctx.fillText(p.text, p.x, p.y);
      }
      ctx.globalAlpha = 1;
    };
    slides.push({
      sound: "forge",
      red: true,
      palette: ["#8e1519", "#4c0a0e", "#b62324", "#3d0a0d"],
      notes:
        "Supply chain hardening: the compiler threat detection specification turns every workflow into a hardened lock file before it ever runs. Defense in depth, one shell per concern: trigger admission (fork PRs, pull_request_target, bot provenance), expression safety (untrusted ${{ }} moved to env vars, no expression timeouts), a pinned supply chain (actions pinned to commit SHAs, manifest drift rejected, update checks kept on), package and argument hygiene (install scripts, flag-prefixed names, git ref injection), least privilege (read-only token, writes only through safe outputs, no bare label wildcards) and secret and content scans (secrets kept away from logs and telemetry, markdown scanned for hidden instructions, conditional imports checked). All of these are static checks on the authored file; the runtime sandbox and egress firewall come on the next slide. Strict mode turns warnings into errors; --validate, actionlint, zizmor and poutine add parser and scanner checks on the generated YAML. The attacks are simulated and the SHA is shortened for illustration.",
      enter() {
        shots = [];
        bits = [];
        pops = [];
        flow = [];
        deck = [];
        last = spawn = total = 0;
        layers.forEach((l, i) => {
          l.flash = 0;
          l.n = 0;
          rows[i].querySelector("em").textContent = "0";
          rows[i].classList.remove("hit");
        });
      },
      tick(t) {
        const dt = last ? Math.min(50, t - last) : 16;
        last = t;
        if (t - spawn > 520) {
          spawn = t;
          if (!deck.length) deck = attacks.map((_, i) => i).sort(() => Math.random() - 0.5);
          const [k, label] = attacks[deck.pop()];
          // stay clear of the file cards on the left
          const a = -Math.PI * 0.62 + Math.random() * Math.PI * 1.24;
          // start off-screen-ish, but below the heading and above the slide chrome
          const sn = Math.sin(a),
            d0 = Math.min(600, sn < 0 ? (CY - 300) / -sn : (1030 - CY) / Math.max(0.01, sn));
          shots.push({ a, d: d0, d0, k, label, v: 0.24 + Math.random() * 0.08 });
          if (Math.random() < 0.8) flow.push({ p: 0, out: false });
        }
        for (const s of shots) {
          s.d -= dt * s.v;
          const l = layers[s.k];
          if (s.d <= l.r) {
            s.dead = true;
            l.flash = 1;
            l.n++;
            total++;
            const row = rows[s.k];
            row.querySelector("em").textContent = String(l.n);
            row.classList.add("hit");
            clearTimeout(row._t);
            row._t = setTimeout(() => row.classList.remove("hit"), 260);
            const x = CX + Math.cos(s.a) * l.r,
              y = CY + Math.sin(s.a) * l.r;
            for (let j = 0; j < 16; j++) {
              const ang = s.a + (Math.random() - 0.5) * 2.4,
                sp = 0.08 + Math.random() * 0.25;
              bits.push({ x, y, vx: Math.cos(ang) * sp, vy: Math.sin(ang) * sp, life: 1, c: j % 3 ? l.c : "#ff7b72" });
            }
            pops.push({ x: x + Math.cos(s.a) * 34, y: y + Math.sin(s.a) * 34, text: l.ids.split(" · ")[0] + " ✓", c: l.c, life: 1 });
          }
        }
        shots = shots.filter(s => !s.dead);
        for (const b of bits) {
          b.x += b.vx * dt;
          b.y += b.vy * dt;
          b.life -= dt / 700;
        }
        bits = bits.filter(b => b.life > 0);
        for (const p of pops) {
          p.y -= dt * 0.03;
          p.life -= dt / 1300;
        }
        pops = pops.filter(p => p.life > 0);
        for (const f of flow) {
          f.p += dt / 1100;
          if (f.p >= 1 && !f.out) {
            f.out = true;
            f.p = 0;
          }
        }
        flow = flow.filter(f => !(f.out && f.p >= 1));
        for (const l of layers) l.flash = Math.max(0, l.flash - dt / 600);
        draw(t);
      },
      el: h(
        "section",
        { class: "slide ch-slide" },
        cv,
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Compiler threat detection spec"), h("h2", {}, letters("Supply chain "), letters("hardening", "l-grad"))),
        md,
        cmd,
        lock,
        h("div", { class: "ch-legend" }, rows),
        h("div", { class: "ch-scan" }, "+ --validate · actionlint · zizmor · poutine")
      ),
    });
  }

  // Safety and control (security architecture): filtered inputs → sandbox → safe outputs
  {
    resetIndex();
    const boxes = [
      ["01 · IN · UNTRUSTED", "Filtered inputs", "#f85149", "", ["Untrusted content sanitized before the agent sees it", h("span", {}, "Read-only ", h("code", {}, "GITHUB_TOKEN"))]],
      [
        "02 · RUN · SAFE",
        "Sandbox",
        "#3fb950",
        "sandbox",
        [
          h("span", {}, "Isolated container or ", h("code", {}, "NVX"), " microVM"),
          "Enclaves: sub-agents scoped by repo sensitivity",
          "Integrity filtering in the MCP gateway",
          "Egress firewall: domain allowlist only",
          "Model routing via API proxy: no keys inside",
        ],
      ],
      [
        "03 · OUT · SAFE",
        "Safe outputs",
        "#4493f8",
        "out",
        [h("span", {}, "Agent only ", h("em", {}, "proposes"), ": ", h("code", {}, "create-issue"), ", ", h("code", {}, "create-pull-request"), "…"), "Threat detection scans every output", "Separate job holds write permissions"],
      ],
    ];
    const BX = [110, 710, 1310];
    const threats = [
      ["prompt injection", 80, 360],
      ["secret exfiltration", 1420, 130],
      ["poisoned pull request", 1300, 900],
      ["token theft", 640, 960],
      ["rogue MCP server", 1500, 250],
      ["jailbreak", 60, 880],
      ["supply-chain attack", 1640, 420],
      ["data leak", 300, 230],
      ["malicious issue comment", 1180, 60],
      ["typosquatted package", 200, 990],
      ["curl | sh", 1720, 820],
      ["ignore previous instructions", 520, 30],
    ];
    const lane = h("canvas", { class: "sec-lane", width: 1920, height: 120 });
    const ctx = lane.getContext("2d");
    const GATE1 = 660,
      GATE2 = 1260,
      END = 1810,
      LY = 60;
    let pk = [],
      lastSpawn = 0,
      last = 0;
    slides.push({
      sound: "omen",
      red: true,
      palette: ["#8e1519", "#4c0a0e", "#b62324", "#3d0a0d"],
      notes:
        "Safety and control: the security architecture is a big selling point of gh-aw. Inputs are filtered and sanitized, the agent runs in a sandbox (container or NVX microVM, with enclaves scoping sub-agents by repo sensitivity and integrity filtering in the MCP gateway) with a read-only token, model calls routed through an API proxy so no keys live inside, behind an egress firewall and MCP gateway, and it can only request safe outputs, which are scanned by threat detection and applied by a separate job that holds the write permissions. Enterprise ready: self-hosted runners, ARC with Docker-in-Docker, OpenTelemetry, GitHub Apps auth and multi-repo workflows.",
      enter() {
        pk = [];
        lastSpawn = last = 0;
        sfx.omen();
      },
      tick(t) {
        const dt = last ? Math.min(50, t - last) : 16;
        last = t;
        if (t - lastSpawn > 260) {
          lastSpawn = t;
          const r = Math.random();
          pk.push({ x: 20, y: LY + (Math.random() - 0.5) * 50, bad: r < 0.22 ? 1 : r < 0.32 ? 2 : 0, a: 1, ph: Math.random() * 6 });
        }
        ctx.clearRect(0, 0, 1920, 120);
        ctx.strokeStyle = "#ffffff14";
        ctx.lineWidth = 2;
        ctx.setLineDash([6, 10]);
        ctx.beginPath();
        ctx.moveTo(0, LY);
        ctx.lineTo(1920, LY);
        ctx.stroke();
        ctx.setLineDash([]);
        for (const [x, c] of [
          [GATE1, "#f85149"],
          [GATE2, "#4493f8"],
        ]) {
          ctx.fillStyle = c;
          ctx.shadowColor = c;
          ctx.shadowBlur = 20;
          ctx.fillRect(x - 3, 8, 6, 104);
          ctx.shadowBlur = 0;
        }
        ctx.fillStyle = "#3fb95018";
        ctx.fillRect(GATE1 + 50, 6, GATE2 - GATE1 - 100, 108);
        ctx.fillStyle = "#9198a1";
        ctx.font = "600 15px 'Monaspace Neon', ui-monospace, Menlo, monospace";
        ctx.textAlign = "center";
        ctx.fillText("FILTER", GATE1, 2 + 12);
        ctx.fillText("THREAT DETECTION", GATE2, 2 + 12);
        ctx.fillText("GITHUB", END + 40, LY + 5);
        for (const p of pk) {
          const blockAt = p.bad === 1 ? GATE1 - 8 : p.bad === 2 ? GATE2 - 8 : Infinity;
          if (p.x < blockAt) p.x += dt * 0.32;
          else {
            p.a -= dt / 500;
            p.y += dt * 0.08;
          }
          const inBox = p.x > GATE1 && p.x < GATE2;
          const y = p.y + (inBox ? Math.sin(t / 180 + p.ph) * 14 : 0);
          const col = p.x >= blockAt ? "#ff7b72" : p.x > GATE2 ? "#4493f8" : inBox ? "#3fb950" : p.bad ? "#f85149" : "#8b949e";
          ctx.globalAlpha = Math.max(0, p.a);
          ctx.fillStyle = col;
          ctx.shadowColor = col;
          ctx.shadowBlur = 14;
          ctx.beginPath();
          ctx.arc(p.x, y, 7, 0, 7);
          ctx.fill();
          ctx.shadowBlur = 0;
        }
        ctx.globalAlpha = 1;
        pk = pk.filter(p => p.a > 0 && p.x < END);
      },
      el: h(
        "section",
        { class: "slide sec" },
        h("div", { class: "sec-omen" }),
        h(
          "div",
          { class: "sec-threats" },
          threats.map(([w, x, y], i) => h("span", { "--i": i, style: { left: `${x}px`, top: `${y}px` } }, w))
        ),
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Secure by design"), h("h2", {}, letters("Safety "), letters("and control", "l-grad"))),
        h(
          "div",
          { class: "sec-ent" },
          h("i", {}, "Enterprise ready"),
          ["Self-hosted runners", "ARC + DinD", "OpenTelemetry", "GitHub Apps", "Multi-repo"].map((x, i) => h("span", { "--i": i }, x))
        ),
        boxes.map(([tag, title, c, cls, items], i) =>
          h(
            "div",
            { class: `sec-box ${cls}`, "--c": c, "--i": i, style: { left: `${BX[i]}px` } },
            cls === "sandbox" ? h("b", { class: "sec-shield" }, "🛡 safe zone") : null,
            h("i", {}, tag),
            h("h3", {}, title),
            h(
              "ul",
              {},
              items.map((x, j) => h("li", { "--j": j }, x))
            )
          )
        ),
        [0, 1].map(i => h("div", { class: "sec-arrow", "--i": i, style: { left: `${BX[i] + 500}px` } }, "→")),
        lane
      ),
    });
  }

  // Safe outputs: the boxed agent can't create an issue, so it proposes create-issue; threat detection scans it; a separate job writes it
  {
    resetIndex();
    const BX = [110, 710, 1310];
    const CMD = 'gh issue create -t "Flaky test"';
    const typed = h("span", { class: "cur" });
    const status = h("span", { class: "st" }, "proposed");
    const card = h(
      "div",
      { class: "so-card" },
      h("div", { class: "so-beam" }),
      h("div", { class: "t" }, h("span", { class: "so-ico", "data-ico": "issue", "data-fill": "#3fb950" }), "Flaky test in cart handler"),
      h("div", { class: "lb" }, h("span", {}, "bug"), h("span", {}, "agentic")),
      status
    );
    const checks = ["Prompt injection", "Secret leaks", "Malicious links", "Harmful code"].map(x => h("li", {}, x));
    const boxes = [
      h(
        "div",
        { class: "so-box so-b0", "--c": "#3fb950", "--i": 0, style: { left: `${BX[0]}px`, borderStyle: "dashed", borderWidth: "3px" } },
        h("div", { class: "so-cage" }),
        h(
          "div",
          { class: "so-shake" },
          h("i", {}, "01 · AGENT · SANDBOXED"),
          h("h3", {}, "Agent job"),
          h("div", { class: "so-who" }, h("span", { class: "so-av" }, h("span", { class: "so-ico", "data-ico": "copilot" })), "Agent", h("span", { class: "so-perm" }, "🔒 read-only")),
          h("div", { class: "so-term" }, h("div", {}, "$ ", typed), h("div", { class: "l2" }, "✕ 403 · token is read-only"), h("div", { class: "l3" }, "→ safe output: create_issue"))
        )
      ),
      h(
        "div",
        { class: "so-box so-b1", "--c": "#d29922", "--i": 1, style: { left: `${BX[1]}px` } },
        h("i", {}, "02 · DETECT · ISOLATED"),
        h("h3", {}, "Threat detection"),
        h("div", { class: "so-who" }, h("span", { class: "so-av det" }, h("span", { class: "so-ico", "data-ico": "copilot" })), "Detection agent", h("span", { class: "so-perm" }, "no tools")),
        h("ul", { class: "so-checks" }, checks)
      ),
      h(
        "div",
        { class: "so-box so-b2", "--c": "#4493f8", "--i": 2, style: { left: `${BX[2]}px` } },
        h("i", {}, "03 · WRITE · SCOPED"),
        h("h3", {}, "Safe output job"),
        h("div", { class: "so-note" }, "Holds ", h("code", {}, "issues: write"), ", applies only declared outputs")
      ),
    ];
    // [phase class, ms into the cycle]
    const P = [
      ["p-type", 600],
      ["p-deny", 2000],
      ["p-safe", 3100],
      ["p-to1", 4100],
      ["p-scan", 5100],
      ["p-pass", 7300],
      ["p-to2", 8100],
      ["p-run", 9100],
      ["p-done", 10600],
    ];
    const at = Object.fromEntries(P),
      CYC = 14000,
      CHECK0 = 5500,
      CHECKDT = 420;
    const el = h(
      "section",
      { class: "slide so" },
      h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Agents propose, jobs write"), h("h2", {}, letters("Safe "), letters("outputs", "l-grad"))),
      boxes,
      [0, 1].map(i => h("div", { class: "so-arrow", "--i": i, style: { left: `${BX[i] + 500}px` } }, "→")),
      [0, 1].map(i => h("div", { class: "so-call", "--i": i, style: { left: `${BX[i] + 500}px` } }, i ? "verdict" : "create-issue")),
      card
    );
    let t0 = 0,
      cyc = -1,
      seen = new Set(),
      nTyped = -1,
      iconsDrawn = false;
    const place = (i, instant) => {
      if (instant) card.style.transition = "none";
      card.style.left = `${BX[i] + 40}px`;
      if (instant) {
        void card.offsetWidth;
        card.style.transition = "";
      }
    };
    const fire = (name, fn) => {
      if (seen.has(name)) return;
      seen.add(name);
      fn();
    };
    slides.push({
      sound: "factory",
      green: true,
      palette: ["#238636", "#0e3a1f", "#1f6feb", "#0b2f3a"],
      notes:
        "Safe outputs, an illustrative run with a synthetic issue. The agent runs boxed in: its token is read-only, so when it tries to create an issue directly the call is refused. Instead it calls the create-issue safe output, which only records a proposal. A separate threat detection job, an isolated agent with no tools, reads the proposed issue and looks for prompt injection, leaked secrets, malicious links and harmful code. Only when the verdict is clean does a third job, the only one holding issues: write, apply the declared output and actually create the issue. The agent never holds write credentials, and every write is limited to the outputs declared in the front matter.",
      enter() {
        if (!iconsDrawn) {
          iconsDrawn = true;
          el.querySelectorAll(".so-ico").forEach(s => {
            s.innerHTML = `<svg viewBox="0 0 24 24" fill="${s.dataset.fill || "#f0f6fc"}">${OCTI[s.dataset.ico].map(d => `<path d="${d}"/>`).join("")}</svg>`;
          });
        }
        t0 = 0;
        cyc = -1;
      },
      tick(t) {
        if (!t0) t0 = t;
        const e = t - t0,
          c = Math.floor(e / CYC),
          u = e % CYC;
        if (c !== cyc) {
          cyc = c;
          seen = new Set();
          nTyped = -1;
          P.forEach(([cls]) => el.classList.remove(cls));
          checks.forEach(li => li.classList.remove("ok"));
          boxes.forEach(b => b.classList.remove("hot"));
          status.textContent = "proposed";
          place(0, true);
        }
        for (const [cls, ms] of P) el.classList.toggle(cls, u >= ms);
        const n = Math.max(0, Math.min(CMD.length, Math.floor(((u - at["p-type"]) / (at["p-deny"] - at["p-type"] - 200)) * CMD.length)));
        if (n !== nTyped) {
          if (n > nTyped && nTyped >= 0 && u >= at["p-type"]) sfx.key(0.7);
          nTyped = n;
          typed.textContent = CMD.slice(0, n);
        }
        boxes[0].classList.toggle("hot", u >= at["p-type"] && u < at["p-to1"]);
        boxes[1].classList.toggle("hot", u >= at["p-to1"] && u < at["p-to2"]);
        boxes[2].classList.toggle("hot", u >= at["p-to2"]);
        card.classList.toggle("moving", (u >= at["p-to1"] && u < at["p-to1"] + 900) || (u >= at["p-to2"] && u < at["p-to2"] + 900));
        if (u >= at["p-deny"]) fire("deny", () => sfx.step(-1));
        if (u >= at["p-safe"]) fire("safe", () => sfx.pop(2));
        if (u >= at["p-to1"])
          fire("to1", () => {
            place(1);
            sfx.pop(3);
          });
        if (u >= at["p-scan"]) fire("scan", () => (status.textContent = "scanning…"));
        checks.forEach((li, i) => {
          if (u >= CHECK0 + i * CHECKDT)
            fire("c" + i, () => {
              li.classList.add("ok");
              sfx.pop(4 + i);
            });
        });
        if (u >= at["p-pass"])
          fire("pass", () => {
            status.textContent = "✓ no threats";
            sfx.review();
          });
        if (u >= at["p-to2"])
          fire("to2", () => {
            place(2);
            sfx.pop(5);
          });
        if (u >= at["p-run"]) fire("run", () => sfx.compile());
        if (u >= at["p-done"])
          fire("done", () => {
            status.textContent = "● open · #42";
            sfx.notify(false);
          });
      },
      el,
    });
  }

  // Private enclaves: a public-repo agent asks an isolated sub-agent about a private repo; only finite, budgeted answers come back
  {
    resetIndex();
    const BUDGET = 8;
    const ROUNDS = [
      { q: "Does it still call the v1 billing API?", schema: '{ uses_v1: boolean, callers: "0" | "1-5" | "6+" }', hot: [1, 4], a: '{ uses_v1: true, callers: "1-5" }', bits: 2.6, log: "uses v1 billing · 1-5 callers" },
      { q: "Is the auth library on the patched release?", schema: "{ patched: boolean }", hot: [7], leak: '"no, see auth/session.ts: const key = …"', a: "{ patched: false }", bits: 1, log: "auth library not patched" },
      { q: "Which test tier is flaky?", schema: '{ tier: "unit" | "integration" | "e2e" }', hot: [9], a: '{ tier: "integration" }', bits: 1.6, log: "flaky tier: integration" },
      { q: "Which team owns the billing module?", schema: "{ team: one of 16 teams }", bits: 4, over: true },
    ];
    const CODE = [
      "// billing/client.ts",
      'import { charge } from "./v1/billing";',
      "export async function renew(acct) {",
      "  const plan = await lookup(acct);",
      '  return charge(plan, { api: "v1" });',
      "}",
      "// auth/session.ts",
      'import { sign } from "auth-lib@2.3.0";',
      "// tests/integration/sync.test.ts",
      'test.retry(3)("sync settles", run);',
    ];
    const PX = 560,
      GX = 800,
      VX = 1040;
    const qTask = h("div", { class: "en-task" });
    const qSchema = h("div", { class: "en-schema" });
    const call = h("div", { class: "en-call" }, h("b", {}, "enclave_run_agent"), qTask, h("small", {}, "answer schema"), qSchema);
    const log = h("ul", { class: "en-log" });
    const out = h("div", { class: "en-out" }, h("code", {}, "create-issue"), "Plan the v1 billing migration");
    const lines = CODE.map((l, i) => h("div", { class: "en-line" }, h("span", { class: "no" }, String(i + 1)), l));
    const vaultStatus = h("div", { class: "en-status" });
    const vault = h(
      "div",
      { class: "en-card en-vault", "--c": "#3fb950", "--i": 2 },
      h("b", { class: "en-lock" }, "🔒 enclave"),
      h("i", {}, "PRIVATE REPO · ISOLATED SUB-AGENT"),
      h("h3", {}, "octo-org/private-service"),
      h("div", { class: "en-facts" }, h("span", {}, "no egress"), h("span", {}, "read-only"), h("span", {}, "masked capability"), h("span", {}, "NVX microVM")),
      h("div", { class: "en-code" }, lines),
      vaultStatus
    );
    const gNote = h("div", { class: "en-gnote" });
    const fill = h("div", { class: "en-fill" });
    const leftN = h("b");
    const invN = h("span", { class: "en-left" });
    const el = h(
      "section",
      { class: "slide en" },
      h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Finite disclosure · private repos"), h("h2", {}, letters("Private "), letters("enclaves", "l-grad"))),
      h(
        "div",
        { class: "en-card en-pub", "--c": "#4493f8", "--i": 0 },
        h("i", {}, "PUBLIC REPO · PRIMARY AGENT"),
        h("h3", {}, "Asks, never reads"),
        h("div", { class: "en-facts" }, h("span", {}, h("b", {}, "0"), " private tokens"), h("span", {}, "no clone")),
        call,
        log,
        out
      ),
      h("div", { class: "en-gate" }, h("i", {}, "MCP", h("br"), "gateway"), h("div", { class: "en-pillar" }), gNote, h("small", {}, "finite schema", h("br"), "budget ledger")),
      vault,
      h(
        "div",
        { class: "en-ledger" },
        h("i", {}, "Information budget"),
        h("div", { class: "en-bar" }, fill),
        h("span", { class: "en-left" }, leftN, " / 8 bits left"),
        invN,
        h(
          "div",
          { class: "en-sens" },
          ["public", "trusted", "internal", "confidential", "sealed"].map(s => h("span", { class: s === "confidential" ? "on" : "" }, s))
        )
      )
    );
    let ev = [],
      evI = 0,
      pks = [],
      used = 0,
      calls = 0,
      t0 = 0,
      now = 0,
      fresh = true;
    const fmt = n => (Math.round(n * 10) / 10).toString();
    const ledger = () => {
      const left = BUDGET - used;
      fill.style.width = `${(left / BUDGET) * 100}%`;
      fill.classList.toggle("low", left < 3);
      leftN.textContent = fmt(left);
      invN.textContent = `· ${calls} / 4 invocations`;
    };
    const say = (txt, ok) => {
      gNote.textContent = txt;
      gNote.className = `en-gnote show ${ok ? "ok" : "no"}`;
    };
    const launch = (txt, cls, x0, x1, dur, blocked) => {
      const p = h("div", { class: `en-pk ${cls}` }, txt);
      el.append(p);
      pks.push({ el: p, x0, x1, start: now, dur, blocked, dead: 0 });
    };
    const script = () => {
      ev = [];
      evI = 0;
      const at = (ms, fn) => ev.push([ms, fn]);
      let T = 600;
      ROUNDS.forEach((r, k) => {
        const s = T;
        at(s, () => {
          qTask.textContent = r.q;
          qSchema.textContent = r.schema;
          call.classList.remove("flash");
          void call.offsetWidth;
          call.classList.add("flash");
          lines.forEach(l => l.classList.remove("hot"));
          gNote.classList.remove("show");
          sfx.key();
        });
        at(s + 700, () => launch("task ▸", "q", PX, r.over ? GX : VX, r.over ? 700 : 1300, r.over));
        if (r.over) {
          at(s + 1400, () => {
            say(`✗ over budget · needs ${r.bits} bits`, 0);
            sfx.route(0);
          });
          T = s + 3200;
          return;
        }
        at(s + 1350, () => say("✓ masked capability", 1));
        at(s + 2000, () => {
          calls++;
          ledger();
          vault.classList.add("busy");
          vaultStatus.textContent = "sub-agent reading ";
          gNote.classList.remove("show");
        });
        r.hot.forEach((n, j) => at(s + 2400 + j * 500, () => (lines[n].classList.add("hot"), sfx.key(0.6))));
        let u = s + 3600;
        at(u, () => {
          vault.classList.remove("busy");
          vaultStatus.textContent = "answer ready";
        });
        if (r.leak) {
          at(u, () => launch(r.leak, "leak", VX, GX, 800, true));
          at(u + 800, () => {
            say("✗ free-form string · denied", 0);
            sfx.route(0);
          });
          u += 1800;
        }
        at(u, () => launch(r.a, "a", VX, PX, 1400));
        at(u + 700, () => {
          used += r.bits;
          ledger();
          say(`✓ schema · −${r.bits} bits`, 1);
          sfx.route(2);
        });
        at(u + 1400, () => {
          log.append(h("li", {}, r.log));
          sfx.pop(k * 2);
        });
        T = u + 2200;
      });
      at(T, () => {
        out.classList.add("show");
        gNote.classList.remove("show");
        sfx.review();
      });
      at(T + 4000, () => (fresh = true));
    };
    const reset = () => {
      pks.forEach(p => p.el.remove());
      pks = [];
      used = calls = 0;
      log.replaceChildren();
      out.classList.remove("show");
      vault.classList.remove("busy");
      vaultStatus.textContent = "";
      gNote.className = "en-gnote";
      qTask.textContent = "";
      qSchema.textContent = "";
      lines.forEach(l => l.classList.remove("hot"));
      ledger();
      t0 = now;
      script();
    };
    slides.push({
      sound: "vault",
      green: true,
      palette: ["#238636", "#0e3a1f", "#8957e5", "#0b2f3a"],
      notes:
        "Private enclaves. An agent running in a public repository sometimes needs facts about a private repository, but it must never hold the private code or a token for it. With the enclaves frontmatter, the compiler registers an enclave_run_agent (or enclave_run_script) tool behind the MCP gateway. The primary agent sends a task plus a finite answer schema; AWF stages the private repo read-only into an isolated sub-agent (an NVX microVM, no egress) using a masked per-run capability the primary never sees. Only answers that fit the finite schema come back, and every answer is charged against an information budget set by the repo's sensitivity: public, trusted, internal, confidential or sealed. Free-form strings are denied, and once the budget or max-invocations is spent, further questions are refused. The repository, code, questions and bit counts on this slide are synthetic and illustrative.",
      enter() {
        fresh = true;
      },
      tick(t) {
        now = t;
        if (fresh) {
          fresh = false;
          reset();
        }
        const e = t - t0;
        while (evI < ev.length && ev[evI][0] <= e) ev[evI++][1]();
        for (const p of pks) {
          if (p.dead) continue;
          const u = Math.min(1, (t - p.start) / p.dur);
          const k = u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2;
          p.el.style.transform = `translate(calc(${p.x0 + (p.x1 - p.x0) * k}px - 50%), 0)`;
          if (u >= 1) {
            p.dead = t + 600;
            if (p.blocked) p.el.classList.add("burst");
            else p.dead = t;
          }
        }
        pks = pks.filter(p => {
          if (p.dead && t >= p.dead) p.el.remove();
          return !p.dead || t < p.dead;
        });
      },
      el,
    });
  }

  // The factory mindset: engineer processes, not code
  {
    resetIndex();
    const flows = [
      "repo-assist",
      "issue-triage-agent",
      "code-simplifier",
      "dead-code-remover",
      "duplicate-code-detector",
      "semantic-function-refactor",
      "daily-file-diet",
      "unbloat-docs",
      "daily-doc-healer",
      "ci-coach",
      "hourly-ci-cleaner",
      "grumpy-reviewer",
      "pr-sous-chef",
      "mergefest",
      "security-review",
      "agentic-token-optimizer",
      "issue-monster",
      "necromancer",
      "daily-team-status",
      "poem-bot 🎭",
    ];
    const PAL = ["#ff80ea", "#ff80ea", "#b0520f", "#0a8a2e", "#76f794", "#76f794", "#b9a00a", "#ab7df8", "#0d1117"];
    const GLY = ["=", "(", ")", "<", "{", "}", "/>", "pr", "branch", "check"];
    const SVGI = {
      pr: '<svg viewBox="0 0 24 24"><path d="M16 19.25a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0Zm-14.5 0a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0Zm0-14.5a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0ZM4.75 3a1.75 1.75 0 1 0 .001 3.501A1.75 1.75 0 0 0 4.75 3Zm0 14.5a1.75 1.75 0 1 0 .001 3.501A1.75 1.75 0 0 0 4.75 17.5Zm14.5 0a1.75 1.75 0 1 0 .001 3.501 1.75 1.75 0 0 0-.001-3.501Z"/><path d="M13.405 1.72a.75.75 0 0 1 0 1.06L12.185 4h4.065A3.75 3.75 0 0 1 20 7.75v8.75a.75.75 0 0 1-1.5 0V7.75a2.250 2.25 0 0 0-2.25-2.25h-4.064l1.22 1.22a.75.75 0 0 1-1.061 1.06l-2.5-2.5a.75.75 0 0 1 0-1.06l2.5-2.5a.75.75 0 0 1 1.06 0ZM4.75 7.25A.75.75 0 0 1 5.5 8v8A.75.75 0 0 1 4 16V8a.75.75 0 0 1 .75-.75Z"/></svg>',
      branch:
        '<svg viewBox="0 0 24 24"><path d="M15 4.75a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0ZM2.5 19.25a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0Zm0-14.5a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0ZM5.75 6.5a1.75 1.75 0 1 0-.001-3.501A1.75 1.75 0 0 0 5.75 6.5Zm0 14.5a1.75 1.75 0 1 0-.001-3.501A1.75 1.75 0 0 0 5.75 21Zm12.5-14.5a1.75 1.75 0 1 0-.001-3.501A1.75 1.75 0 0 0 18.25 6.5Z"/><path d="M5.750 16.75A.75.75 0 0 1 5 16V8a.75.75 0 0 1 1.5 0v8a.75.75 0 0 1-.75.75Z"/><path d="M17.5 8.75v-1H19v1a3.75 3.75 0 0 1-3.75 3.75h-7a1.75 1.75 0 0 0-1.75 1.75H5A3.25 3.25 0 0 1 8.25 11h7a2.25 2.25 0 0 0 2.25-2.25Z"/></svg>',
      check: '<svg viewBox="0 0 24 24"><path d="M21.03 5.72a.75.75 0 0 1 0 1.06l-11.5 11.5a.747.747 0 0 1-1.072-.012l-5.5-5.75a.75.75 0 1 1 1.084-1.036l4.970 5.195L19.97 5.72a.75.75 0 0 1 1.06 0Z"/></svg>',
    };
    const N = 7,
      cells = [];
    const grid = h("div", { class: "fmm-grid" });
    const paint = (c, glyph) => {
      if (glyph) {
        const g = GLY[(Math.random() * GLY.length) | 0];
        c.className = "fmm-cell g" + (Math.random() < 0.3 ? " grey" : "");
        c.style.background = "";
        c.innerHTML = SVGI[g] || `<b>${g}</b>`;
      } else {
        c.className = "fmm-cell";
        c.innerHTML = "";
        c.style.background = PAL[(Math.random() * PAL.length) | 0];
      }
    };
    for (let i = 0; i < N * N; i++) {
      const c = h("div", { class: "fmm-cell", "--i": (i % N) + Math.floor(i / N) });
      paint(c, Math.random() < 0.2);
      grid.append(c);
      cells.push(c);
    }
    let last = 0;
    slides.push({
      sound: "factory",
      palette: ["#bf3989", "#238636", "#9e6a03", "#8957e5"],
      notes:
        "The factory mindset. Stop manually translating intent into code. Engineer processes that create trusted features. Use agentic workflows for recurring maintenance, documentation, testing and review. Design the factory; the factory builds the software.",
      enter() {
        last = 0;
      },
      tick(t) {
        if (t - last < 110) return;
        last = t;
        for (let k = 0; k < 2; k++) {
          const c = cells[(Math.random() * cells.length) | 0];
          c.classList.add("flip");
          setTimeout(() => {
            paint(c, Math.random() < 0.22);
            c.classList.add("flop");
            setTimeout(() => c.classList.remove("flop"), 260);
          }, 200);
        }
      },
      el: h(
        "section",
        { class: "slide" },
        h(
          "div",
          { class: "fmm-copy" },
          h("div", { class: "kicker" }, "The factory mindset"),
          h("h2", { class: "fmm-stop" }, "Stop manually translating intent into code."),
          h("h2", { class: "fmm-go" }, "Engineer processes that create trusted features.")
        ),
        grid,
        h(
          "div",
          { class: "fm-marquee" },
          h(
            "div",
            {},
            [...flows, ...flows].map(f => h("span", {}, f))
          )
        )
      ),
    });
  }

  const OCTI = {
    copilot: [
      "M23.922 16.992c-.861 1.495-5.859 5.023-11.922 5.023-6.063 0-11.061-3.528-11.922-5.023A.641.641 0 0 1 0 16.736v-2.869a.841.841 0 0 1 .053-.22c.372-.935 1.347-2.292 2.605-2.656.167-.429.414-1.055.644-1.517a10.195 10.195 0 0 1-.052-1.086c0-1.331.282-2.499 1.132-3.368.397-.406.89-.717 1.474-.952 1.399-1.136 3.392-2.093 6.122-2.093 2.731 0 4.767.957 6.166 2.093.584.235 1.077.546 1.474.952.85.869 1.132 2.037 1.132 3.368 0 .368-.014.733-.052 1.086.23.462.477 1.088.644 1.517 1.258.364 2.233 1.721 2.605 2.656a.832.832 0 0 1 .053.22v2.869a.641.641 0 0 1-.078.256ZM12.172 11h-.344a4.323 4.323 0 0 1-.355.508C10.703 12.455 9.555 13 7.965 13c-1.725 0-2.989-.359-3.782-1.259a2.005 2.005 0 0 1-.085-.104L4 11.741v6.585c1.435.779 4.514 2.179 8 2.179 3.486 0 6.565-1.4 8-2.179v-6.585l-.098-.104s-.033.045-.085.104c-.793.9-2.057 1.259-3.782 1.259-1.59 0-2.738-.545-3.508-1.492a4.323 4.323 0 0 1-.355-.508h-.016.016Zm.641-2.935c.136 1.057.403 1.913.878 2.497.442.544 1.134.938 2.344.938 1.573 0 2.292-.337 2.657-.751.384-.435.558-1.15.558-2.361 0-1.14-.243-1.847-.705-2.319-.477-.488-1.319-.862-2.824-1.025-1.487-.161-2.192.138-2.533.529-.269.307-.437.808-.438 1.578v.021c0 .265.021.562.063.893Zm-1.626 0c.042-.331.063-.628.063-.894v-.02c-.001-.77-.169-1.271-.438-1.578-.341-.391-1.046-.69-2.533-.529-1.505.163-2.347.537-2.824 1.025-.462.472-.705 1.179-.705 2.319 0 1.211.175 1.926.558 2.361.365.414 1.084.751 2.657.751 1.21 0 1.902-.394 2.344-.938.475-.584.742-1.44.878-2.497Z",
      "M14.5 14.25a1 1 0 0 1 1 1v2a1 1 0 0 1-2 0v-2a1 1 0 0 1 1-1Zm-5 0a1 1 0 0 1 1 1v2a1 1 0 0 1-2 0v-2a1 1 0 0 1 1-1Z",
    ],
    tag: [
      "M7.75 6.5a1.25 1.25 0 1 0 0 2.5 1.25 1.25 0 0 0 0-2.5Z",
      "M2.5 1h8.44a1.5 1.5 0 0 1 1.06.44l10.25 10.25a1.5 1.5 0 0 1 0 2.12l-8.44 8.44a1.5 1.5 0 0 1-2.12 0L1.44 12A1.497 1.497 0 0 1 1 10.94V2.5A1.5 1.5 0 0 1 2.5 1Zm0 1.5v8.44l10.25 10.25 8.44-8.44L10.94 2.5Z",
    ],
    comment: [
      "M1.5 4.25c0-.966.784-1.75 1.75-1.75h17.5c.966 0 1.75.784 1.75 1.75v12.5a1.75 1.75 0 0 1-1.75 1.75h-9.69l-3.573 3.573A1.458 1.458 0 0 1 5 21.043V18.5H3.25a1.75 1.75 0 0 1-1.75-1.75ZM3.25 4a.25.25 0 0 0-.25.25v12.5c0 .138.112.25.25.25h2.5a.75.75 0 0 1 .75.75v3.19l3.72-3.72a.749.749 0 0 1 .53-.22h10a.25.25 0 0 0 .25-.25V4.25a.25.25 0 0 0-.25-.25Z",
    ],
    pr: [
      "M16 19.25a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0Zm-14.5 0a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0Zm0-14.5a3.25 3.25 0 1 1 6.5 0 3.25 3.25 0 0 1-6.5 0ZM4.75 3a1.75 1.75 0 1 0 .001 3.501A1.75 1.75 0 0 0 4.75 3Zm0 14.5a1.75 1.75 0 1 0 .001 3.501A1.75 1.75 0 0 0 4.75 17.5Zm14.5 0a1.75 1.75 0 1 0 .001 3.501 1.75 1.75 0 0 0-.001-3.501Z",
      "M13.405 1.72a.75.75 0 0 1 0 1.06L12.185 4h4.065A3.75 3.75 0 0 1 20 7.75v8.75a.75.75 0 0 1-1.5 0V7.75a2.25 2.25 0 0 0-2.25-2.25h-4.064l1.22 1.22a.75.75 0 0 1-1.061 1.06l-2.5-2.5a.75.75 0 0 1 0-1.06l2.5-2.5a.75.75 0 0 1 1.06 0ZM4.75 7.25A.75.75 0 0 1 5.5 8v8A.75.75 0 0 1 4 16V8a.75.75 0 0 1 .75-.75Z",
    ],
    issue: ["M12 1c6.075 0 11 4.925 11 11s-4.925 11-11 11S1 18.075 1 12 5.925 1 12 1ZM2.5 12a9.5 9.5 0 0 0 9.5 9.5 9.5 9.5 0 0 0 9.5-9.5A9.5 9.5 0 0 0 12 2.5 9.5 9.5 0 0 0 2.5 12Zm9.5 2a2 2 0 1 1-.001-3.999A2 2 0 0 1 12 14Z"],
    repo: [
      "M3 2.75A2.75 2.75 0 0 1 5.75 0h14.5a.75.75 0 0 1 .75.75v20.5a.75.75 0 0 1-.75.75h-6a.75.75 0 0 1 0-1.5h5.25v-4H6A1.5 1.5 0 0 0 4.5 18v.75c0 .716.43 1.334 1.05 1.605a.75.75 0 0 1-.6 1.374A3.251 3.251 0 0 1 3 18.75ZM19.5 1.5H5.75c-.69 0-1.25.56-1.25 1.25v12.651A2.989 2.989 0 0 1 6 15h13.5Z",
      "M7 18.25a.25.25 0 0 1 .25-.25h5a.25.25 0 0 1 .25.25v5.01a.25.25 0 0 1-.397.201l-2.206-1.604a.25.25 0 0 0-.294 0L7.397 23.46a.25.25 0 0 1-.397-.2v-5.01Z",
    ],
    discussion: [
      "M1.75 1h12.5c.966 0 1.75.784 1.75 1.75v9.5A1.75 1.75 0 0 1 14.25 14H8.061l-2.574 2.573A1.458 1.458 0 0 1 3 15.543V14H1.75A1.75 1.75 0 0 1 0 12.25v-9.5C0 1.784.784 1 1.75 1ZM1.5 2.75v9.5c0 .138.112.25.25.25h2a.75.75 0 0 1 .75.75v2.19l2.72-2.72a.749.749 0 0 1 .53-.22h6.5a.25.25 0 0 0 .25-.25v-9.5a.25.25 0 0 0-.25-.25H1.75a.25.25 0 0 0-.25.25Z",
      "M22.5 8.75a.25.25 0 0 0-.25-.25h-3.5a.75.75 0 0 1 0-1.5h3.5c.966 0 1.75.784 1.75 1.75v9.5A1.75 1.75 0 0 1 22.25 20H21v1.543a1.457 1.457 0 0 1-2.487 1.03L15.939 20H10.75A1.75 1.75 0 0 1 9 18.25v-1.465a.75.75 0 0 1 1.5 0v1.465c0 .138.112.25.25.25h5.5a.75.75 0 0 1 .53.22l2.72 2.72v-2.19a.75.75 0 0 1 .75-.75h2a.25.25 0 0 0 .25-.25v-9.5Z",
    ],
    tasklist: [
      "M3 6a1 1 0 0 1 1-1h5a1 1 0 0 1 1 1v5a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1Zm1.5 4.5h4v-4h-4Zm8.25-5a.75.75 0 0 0 0 1.5h7.5a.75.75 0 0 0 0-1.5h-7.5Zm0 6a.75.75 0 0 0 0 1.5h7.5a.75.75 0 0 0 0-1.5h-7.5Zm0 6a.75.75 0 0 0 0 1.5h7.5a.75.75 0 0 0 0-1.5h-7.5Zm-2.97-2.53a.75.75 0 0 1 0 1.06l-3.5 3.5a.75.75 0 0 1-1.06 0l-2-2a.75.75 0 1 1 1.06-1.06l1.47 1.47 2.97-2.97a.75.75 0 0 1 1.06 0Z",
    ],
    stopwatch: [
      "M10.25 0h3.5a.75.75 0 0 1 0 1.5h-1v1.278a9.954 9.954 0 0 1 5.636 2.276L19.72 3.72a.751.751 0 0 1 1.042.018.751.751 0 0 1 .018 1.042l-1.315 1.316A9.959 9.959 0 0 1 22 12.75c0 5.523-4.477 10-10 10s-10-4.477-10-10a9.959 9.959 0 0 1 2.535-6.654L3.22 4.78a.751.751 0 0 1 .018-1.042.751.751 0 0 1 1.042-.018l1.335 1.334a9.958 9.958 0 0 1 5.635-2.276V1.5h-1a.75.75 0 0 1 0-1.5ZM12 21.25a8.5 8.5 0 1 0-.001-17.001A8.5 8.5 0 0 0 12 21.25Z",
    ],
    merge: [
      "M5.45 5.154A4.25 4.25 0 0 0 9.25 7.5h1.378a2.251 2.251 0 1 1 0 1.5H9.25A5.734 5.734 0 0 1 5 7.123v3.505a2.25 2.25 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.95-.218ZM4.25 13.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.5-4.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5ZM5 3.25a.75.75 0 1 0 0 .005V3.25Z",
    ],
    person: ["M12 2.5a5.5 5.5 0 0 1 3.096 10.047 9.005 9.005 0 0 1 5.9 8.181.75.75 0 1 1-1.499.044 7.5 7.5 0 0 0-14.993 0 .75.75 0 0 1-1.5-.045 9.005 9.005 0 0 1 5.9-8.18A5.5 5.5 0 0 1 12 2.5ZM8 8a4 4 0 1 0 8 0 4 4 0 0 0-8 0Z"],
    calendar: [
      "M6.75 0a.75.75 0 0 1 .75.75V3h9V.75a.75.75 0 0 1 1.5 0V3h2.75c.966 0 1.75.784 1.75 1.75v16a1.75 1.75 0 0 1-1.75 1.75H3.25a1.75 1.75 0 0 1-1.75-1.75v-16C1.5 3.784 2.284 3 3.25 3H6V.75A.75.75 0 0 1 6.75 0ZM21 9.5H3v11.25c0 .138.112.25.25.25h17.5a.25.25 0 0 0 .25-.25Zm-17.75-5a.25.25 0 0 0-.25.25V8h18V4.75a.25.25 0 0 0-.25-.25Z",
    ],
  };
  // Human Agent Processes: trigger → agent → output → human patterns, then the Repo Assist loop
  {
    resetIndex();
    const ICON = OCTI;
    const ico = (n, x, y, sz, fill = "#f0f6fc") => `<g transform="translate(${x - sz / 2},${y - sz / 2}) scale(${sz / 24})" fill="${fill}">${ICON[n].map(d => `<path d="${d}"/>`).join("")}</g>`;
    const LOOPS = [
      {
        key: "Repo Assist",
        sub: "auto maintenance loop",
        c: "#3fb950",
        pick: "Select 2 tasks",
        tasks: [
          ["Issue labelling", "tag"],
          ["Issue investigation", "comment"],
          ["Stale PR nudges", "comment"],
          ["Perf improvements", "pr"],
          ["Testing improvements", "pr"],
        ],
      },
    ];
    const VW = 1100,
      VH = 640,
      TY = [130, 230, 330, 430, 530],
      SY = 330,
      MX = 930,
      MY = 330,
      OX = 790;
    const agent = (p, x, y, r = 30) => `<circle cx="${x}" cy="${y}" r="${r}" fill="url(#${p}Agent)"/>${ico("copilot", x, y + 1, r * 1.15)}`;
    const pill = (x, y, w, label, cls = "") => `<g class="fm-pill ${cls}"><rect x="${x}" y="${y - 23}" width="${w}" height="46" rx="23"/><text x="${x + 15 + w / 2}" y="${y + 8}">${label}</text></g>`;
    const svgFor = (L, p) => {
      let g = `<defs><radialGradient id="${p}Agent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
        <marker id="${p}ArrP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
        <marker id="${p}ArrB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker>
        <clipPath id="${p}Clip" clipPathUnits="objectBoundingBox"><circle cx=".5" cy=".5" r=".5"/></clipPath></defs>`;
      g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
      [
        [0, 250, "SCHEDULE"],
        [250, 770, "ACT"],
        [770, VW, "REVIEW"],
      ].forEach(([x0, x1, t]) => {
        g += `<text class="fm-lane-t" x="${(x0 + x1) / 2}" y="40">${t}</text>`;
        if (x0) g += `<line class="fm-div" x1="${x0}" y1="0" x2="${x0}" y2="${VH}"/>`;
      });
      g += `<path class="fm-wire p" d="M240 ${SY} H272"/><path class="fm-wire p" d="M272 ${TY[0]} V${TY[4]}"/>`;
      TY.forEach((y, i) => {
        g += `<path class="fm-wire p" id="fmIn${i}" d="M240 ${SY} H272 V${y} H${300 - 4}" marker-end="url(#${p}ArrP)" style="opacity:${y === SY ? 1 : 0}"/>`;
        g += `<path class="fm-wire p" d="M272 ${y} H296" marker-end="url(#${p}ArrP)"/>`;
        g += `<path class="fm-wire p" id="fmOut${i}" d="M610 ${y} H${OX - 2}" marker-end="url(#${p}ArrP)"/>`;
        g += `<path class="fm-wire b" id="fmRev${i}" d="M${OX + 58} ${y} H${OX + 72} V${MY} H${MX - 42}" marker-end="url(#${p}ArrB)"/>`;
        g += `<g class="fm-task" id="fmTask${i}">${pill(320, y, 290, L.tasks[i][0])}${agent(p, 320, y)}</g>`;
        g += `<g class="fm-out" id="fmOutI${i}"><rect x="${OX}" y="${y - 30}" width="58" height="60" rx="12"/>${ico(L.tasks[i][1], OX + 29, y, 30)}</g>`;
      });
      g += `<path class="fm-wire b" id="fmLoop" d="M${MX} ${MY + 38} V${VH - 20} H52 V${SY + 36}" marker-end="url(#${p}ArrB)"/>`;
      g += `<g class="fm-sched">${pill(40, SY, 200, L.pick)}${agent(p, 40, SY)}</g>`;
      g += `<g class="fm-watch">${ico("stopwatch", 150, 530, 64, "#f0f6fc")}<line id="fmHand" x1="150" y1="532" x2="150" y2="512" stroke="#f0f6fc" stroke-width="4" stroke-linecap="round"/></g>`;
      g += `<g class="fm-maint"><g class="fm-pill b"><rect x="${MX}" y="${MY - 23}" width="172" height="46" rx="23"/><text x="${MX + 38 + 67}" y="${MY + 8}">Maintainer</text></g>
        <circle cx="${MX}" cy="${MY}" r="38" fill="#0d1117" stroke="#4493f8" stroke-width="3"/><image href="assets/avatars/dsyme.jpg" x="${MX - 35}" y="${MY - 35}" width="70" height="70" clip-path="url(#${p}Clip)"/></g>`;
      g += `<circle class="fm-dot" r="7"/><circle class="fm-dot" r="7"/>`;
      return g;
    };
    // Repo Assist backlog burndown: an approximate, illustrative redraw of open issues
    // (% of the count on adoption day) across F# open-source repositories.
    const makeBurndown = svg => {
      let s = 7;
      const rnd = () => ((s = (s * 16807) % 2147483647) - 1) / 2147483646;
      const D0 = -90,
        D1 = 80;
      // [level before adoption, settled level after, decay days, dashed]
      const SPEC = [
        [100, 3, 5, 0],
        [102, 2, 3, 0],
        [98, 12, 8, 0],
        [100, 15, 10, 0],
        [101, 20, 5, 0],
        [116, 32, 14, 0],
        [82, 13, 12, 0],
        [99, 26, 18, 0],
        [103, 8, 3, 0],
        [100, 30, 22, 0],
        [97, 18, 9, 0],
        [101, 86, 70, 1],
        [100, 76, 40, 1],
        [99, 62, 30, 1],
      ];
      const series = SPEC.map(([lv, tg, tau, dash]) => {
        const pts = [];
        let v = lv;
        for (let d = D0; d <= D1; d++) {
          if (d < -6) {
            if (rnd() < 0.12) v = lv + (rnd() - 0.5) * 8;
          } else if (d <= 0) v = lv + ((100 - lv) * (d + 6)) / 6;
          else if (rnd() < 0.3) v = Math.max(0, tg + (100 - tg) * Math.exp(-d / tau) + (rnd() - 0.4) * (dash ? 6 : 14));
          pts.push(v);
        }
        return { pts, dash };
      });
      const med = series[0].pts.map((_, i) => {
        const col = series.map(x => x.pts[i]).sort((a, b) => a - b);
        return (col[6] + col[7]) / 2;
      });
      const X0 = 110,
        X1 = 1010,
        YT = 140,
        YB = 560;
      const x = d => X0 + ((d - D0) / (D1 - D0)) * (X1 - X0),
        y = p => YB - (p / 120) * (YB - YT),
        XA = x(0);
      const path = pts =>
        "M" +
        pts
          .map((p, i) => `${x(D0 + i).toFixed(1)} ${y(p).toFixed(1)}`)
          .join("H")
          .replace(/H([\d.]+) ([\d.]+)/g, (_, a, b) => `H${a}V${b}`);
      const medY = d => y(med[Math.min(med.length - 1, Math.max(0, Math.floor(d - D0)))]);
      const drop = Math.round(100 - med[med.length - 1]);
      const svgFor = () => {
        let g = `<defs><radialGradient id="bdAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
          <clipPath id="bdClip"><rect id="bdClipR" x="0" y="0" width="${X0}" height="${VH}"/></clipPath></defs>`;
        g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
        g += `<text class="fm-lane-t" x="${(X0 + XA) / 2}" y="40">BEFORE</text><text class="fm-lane-t" x="${(XA + X1) / 2}" y="40">WITH REPO ASSIST</text>`;
        for (let p = 0; p <= 120; p += 20) g += `<line class="bd-grid" x1="${X0}" y1="${y(p)}" x2="${X1}" y2="${y(p)}"/><text class="bd-yl" x="${X0 - 14}" y="${y(p) + 6}">${p}%</text>`;
        for (let d = -80; d <= 80; d += 20) g += `<text class="bd-xl" x="${x(d)}" y="${YB + 34}">${d}</text>`;
        g += `<text class="bd-at" x="${(X0 + X1) / 2}" y="${YB + 72}">days before / after adoption · open issues as % of count on adoption day</text>`;
        g += `<g clip-path="url(#bdClip)">${series.map(sr => `<path class="bd-line${sr.dash ? " d" : ""}" d="${path(sr.pts)}"/>`).join("")}<path class="bd-med" d="${path(med)}"/></g>`;
        g += `<line id="bdHead" class="bd-head" x1="${X0}" y1="${YT - 10}" x2="${X0}" y2="${YB}"/>`;
        g += `<line id="bdAdopt" class="bd-adopt" x1="${XA}" y1="${YT - 30}" x2="${XA}" y2="${YB}" pathLength="1"/>`;
        g += `<g id="bdPill" class="bd-pill"><rect x="${XA - 120}" y="${YT - 62}" width="240" height="48" rx="24"/>${ico("calendar", XA - 82, YT - 38, 26)}<text x="${XA + 18}" y="${YT - 30}">Adoption day</text></g>`;
        g += `<g id="bdBot" class="bd-bot"><circle cx="0" cy="0" r="22" fill="url(#bdAgent)"/>${ico("copilot", 0, 1, 26)}</g>`;
        g += `<g id="bdRes" class="bd-res"><text x="${X1 + 34}" y="${y(med[med.length - 1]) + 2}">−${drop}%</text><text class="s" x="${X1 + 36}" y="${y(med[med.length - 1]) + 26}">median</text></g>`;
        g += `<text class="bd-note" x="${(X0 + X1) / 2}" y="${YB + 100}">illustrative redraw · F# open-source repositories</text>`;
        return g;
      };
      const ease = u => 1 - (1 - Math.min(1, Math.max(0, u))) ** 3;
      let fx = [];
      // v: ms since the burndown phase started
      const tick = v => {
        const sweep = v < 2400 ? X0 + (XA - X0) * ease(v / 2400) : v < 3200 ? XA : XA + (X1 - XA) * ease((v - 3200) / 5600);
        svg.querySelector("#bdClipR").setAttribute("width", sweep);
        const head = svg.querySelector("#bdHead");
        head.setAttribute("x1", sweep);
        head.setAttribute("x2", sweep);
        head.style.opacity = v < 8800 ? 1 : 0;
        svg.querySelector("#bdAdopt").style.strokeDashoffset = 1 - ease((v - 2400) / 600);
        svg.querySelector("#bdPill").classList.toggle("on", v >= 2700);
        const bot = svg.querySelector("#bdBot");
        bot.classList.toggle("on", v >= 3000);
        const d = D0 + ((sweep - X0) / (X1 - X0)) * (D1 - D0);
        bot.setAttribute("transform", `translate(${sweep},${medY(d)})`);
        svg.querySelector("#bdRes").classList.toggle("on", v >= 8800);
        if (v >= 2700 && !fx[0]) ((fx[0] = 1), sfx.pop(0));
        if (v >= 8800 && !fx[1]) ((fx[1] = 1), sfx.review());
      };
      return { svgFor, tick, reset: () => (fx = []) };
    };
    const newSvg = cls => {
      const el = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      el.setAttribute("class", cls);
      el.setAttribute("viewBox", `0 0 ${VW} ${VH}`);
      return el;
    };
    // An animated SCHEDULE → ACT → REVIEW swimlane cycling through the given LOOPS, two cycles each.
    let flowSeq = 0;
    const makeFlow = (svg, cap, loops) => {
      const idp = `fm${++flowSeq}`;
      const capB = h("b", {}, ""),
        capS = h("span", {}, "");
      cap.append(capB, capS);
      let mode = -1,
        cyc = -1,
        picks = [],
        t0 = 0,
        lastPh = [];
      const setMode = m => {
        mode = m;
        const L = LOOPS[loops[m]];
        svg.innerHTML = svgFor(L, idp);
        svg.style.setProperty("--c", L.c);
        capB.textContent = L.key;
        capB.style.color = L.c;
        capS.textContent = " · " + L.sub;
        cap.classList.remove("swap");
        void cap.offsetWidth;
        cap.classList.add("swap");
      };
      const at = (id, u) => {
        const p = svg.querySelector("#" + id),
          n = p.getTotalLength(),
          q = p.getPointAtLength(n * Math.min(1, Math.max(0, u)));
        return q;
      };
      const ease = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
      const CYCLE = 6200;
      const step = t => {
        if (!t0) t0 = t;
        const e = t - t0,
          c = Math.floor(e / CYCLE),
          u = e % CYCLE;
        const m = Math.floor(c / 2) % loops.length;
        if (m !== mode) setMode(m);
        if (c !== cyc) {
          cyc = c;
          lastPh = [];
          const all = [0, 1, 2, 3, 4].sort(() => Math.random() - 0.5);
          picks = all.slice(0, 2).sort();
          svg.querySelectorAll(".on").forEach(x => x.classList.remove("on"));
        }
        const hand = svg.querySelector("#fmHand"),
          ang = ((e / 1000) * 120 * Math.PI) / 180;
        hand.setAttribute("x2", 150 + 20 * Math.sin(ang));
        hand.setAttribute("y2", 532 - 20 * Math.cos(ang));
        const dots = svg.querySelectorAll(".fm-dot");
        svg.querySelector(".fm-sched").classList.toggle("on", u < 1700);
        picks.forEach((i, k) => {
          const d = dots[k];
          let pt = null,
            blue = false;
          if (u > 500 && u < 1700) {
            pt = at(`fmIn${i}`, ease((u - 500) / 1200));
            svg.querySelector(`#fmIn${i}`).style.opacity = 1;
          } else if (u >= 1700 && u < 2900) {
            svg.querySelector(`#fmTask${i}`).classList.add("on");
            pt = at(`fmOut${i}`, ease((u - 1700) / 1200));
          } else if (u >= 2900 && u < 4100) {
            svg.querySelector(`#fmOutI${i}`).classList.add("on");
            pt = at(`fmRev${i}`, ease((u - 2900) / 1200));
            blue = true;
          } else if (u >= 4100 && u < 5600 && k === 0) {
            svg.querySelector(".fm-maint").classList.add("on");
            pt = at("fmLoop", ease((u - 4100) / 1500));
            blue = true;
          }
          if (u >= 4100) svg.querySelector(".fm-maint").classList.add("on");
          if (pt && d.style.opacity !== "1") sfx.pop(i + 2 * k);
          // a blip each time the dot reaches a box: task, output, maintainer, back to schedule
          const ph = u < 1700 ? 0 : u < 2900 ? 1 : u < 4100 ? 2 : u < 5600 ? 3 : 4;
          if (ph > (lastPh[k] ?? ph) && (k === 0 || ph < 3)) ph === 3 ? sfx.review() : sfx.pop(ph * 2 + k + 1);
          lastPh[k] = ph;
          d.style.opacity = pt ? 1 : 0;
          if (pt) {
            d.setAttribute("cx", pt.x);
            d.setAttribute("cy", pt.y);
            d.setAttribute("fill", blue ? "#4493f8" : "#ab7df8");
          }
        });
      };
      return {
        reset() {
          t0 = 0;
          mode = cyc = -1;
        },
        step,
      };
    };
    // Patterns: [name, trigger, colour, nodes]; node = [kind (e event, a agent, h human), icon, label]
    const HAP = [
      ["IssueOps", "on: issues opened", "#3fb950", [["e", "issue", "Issue opened"], ["a", "copilot", "Triage"], ["e", "comment", "Comment"], ["h", "person", "Maintainer"]]],
      ["DailyOps", "on: schedule", "#d29922", [["e", "calendar", "Schedule"], ["a", "copilot", "Daily scan"], ["e", "issue", "Issue"]]],
      ["ReviewOps", "on: ready for review", "#ab7df8", [["e", "pr", "Ready for review"], ["a", "copilot", "Code review"], ["e", "comment", "Comments"], ["h", "person", "Maintainer"]]],
      ["LabelOps", "on: label added", "#f778ba", [["e", "tag", "Label added"], ["a", "copilot", "Investigate"], ["e", "comment", "Comment"], ["h", "person", "Maintainer"]]],
    ];
    const HVW = 1700,
      HVH = 580,
      HCX = [450, 770, 1090, 1410],
      HPW = 230,
      HY = [130, 260, 390, 520];
    const hapSvgFor = () => {
      let g = `<defs><radialGradient id="hapAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
        <marker id="hapArrP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
        <marker id="hapArrB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker></defs>`;
      g += `<rect class="fm-lane" x="0" y="0" width="${HVW}" height="60" rx="10"/><text class="fm-lane-t hap-lt" x="24" y="40">PATTERN</text>`;
      ["TRIGGER", "AGENT", "OUTPUT", "HUMAN"].forEach((t, k) => {
        g += `<text class="fm-lane-t" x="${HCX[k] + HPW / 2 - 10}" y="40">${t}</text>`;
      });
      HAP.forEach(([name, sub, c, nodes], r) => {
        const y = HY[r];
        g += `<g class="hap-r" style="--c:${c};animation-delay:${0.6 + r * 0.15}s">`;
        g += `<rect class="hap-row" x="0" y="${y - 54}" width="${HVW}" height="108" rx="18"/>`;
        g += `<text class="hap-name" x="28" y="${y - 2}" fill="${c}">${name}</text><text class="hap-sub" x="28" y="${y + 30}">${sub}</text>`;
        nodes.forEach(([kind], k) => {
          if (k === nodes.length - 1) return;
          const b = nodes[k + 1][0] === "h";
          g += `<path class="fm-wire ${b ? "b" : "p"}" id="hapE${r}_${k}" d="M${HCX[k] + HPW} ${y} H${HCX[k + 1] - 40}" marker-end="url(#hapArr${b ? "B" : "P"})"/>`;
        });
        nodes.forEach(([kind, icon, label], k) => {
          const x = HCX[k];
          const disc =
            kind === "a"
              ? `<circle cx="${x}" cy="${y}" r="32" fill="url(#hapAgent)"/>${ico(icon, x, y + 1, 36)}`
              : `<circle class="hap-disc ${kind}" cx="${x}" cy="${y}" r="32"/>${ico(icon, x, y, kind === "h" ? 38 : 30)}`;
          g += `<g class="hap-node" id="hapN${r}_${k}"><g class="fm-pill${kind === "h" ? " b" : kind === "e" ? " g" : ""}"><rect x="${x}" y="${y - 25}" width="${HPW}" height="50" rx="25"/><text x="${x + 16 + HPW / 2}" y="${y + 8}">${label}</text></g>${disc}</g>`;
        });
        g += `<circle class="fm-dot" id="hapD${r}" r="7"/></g>`;
      });
      return g;
    };
    const hapSvg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    hapSvg.setAttribute("class", "hap-svg");
    hapSvg.setAttribute("viewBox", `0 0 ${HVW} ${HVH}`);
    hapSvg.innerHTML = hapSvgFor();
    // Rows start one after another; each step lights a node, then a dot travels the next edge.
    const HSTEP = 850,
      HROW = 1300,
      HCYCLE = 3 * HROW + 4 * HSTEP + 2600;
    let hT0 = 0,
      hCyc = -1,
      hLit = [];
    const hEase = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
    const hapStep = t => {
      if (!hT0) hT0 = t;
      const e = t - hT0,
        c = Math.floor(e / HCYCLE),
        u = e % HCYCLE;
      if (c !== hCyc) {
        hCyc = c;
        hLit = HAP.map(() => -1);
        hapSvg.querySelectorAll(".on, .hot").forEach(x => x.classList.remove("on", "hot"));
      }
      HAP.forEach(([, , col, nodes], r) => {
        const v = u - r * HROW,
          n = nodes.length,
          dot = hapSvg.querySelector(`#hapD${r}`);
        const k = v < 0 ? -1 : Math.min(n - 1, Math.floor(v / HSTEP));
        while (hLit[r] < k) {
          const j = ++hLit[r],
            kind = nodes[j][0];
          hapSvg.querySelector(`#hapN${r}_${j}`).classList.add("on");
          if (kind === "h") sfx.review();
          else if (kind === "a") sfx.pop(1);
          else sfx.notify(j === 0);
        }
        let pt = null;
        if (k >= 0 && k < n - 1) {
          const f = ((v - k * HSTEP) / HSTEP - 0.3) / 0.7;
          if (f > 0) {
            const p = hapSvg.querySelector(`#hapE${r}_${k}`);
            p.classList.add("hot");
            pt = p.getPointAtLength(p.getTotalLength() * hEase(Math.min(1, f)));
          }
        }
        dot.style.opacity = pt ? 1 : 0;
        if (pt) {
          dot.setAttribute("cx", pt.x);
          dot.setAttribute("cy", pt.y);
          dot.setAttribute("fill", col);
          dot.style.color = col;
        }
      });
    };
    slides.push({
      sound: "loop",
      trans: "up",
      palette: ["#bf3989", "#8957e5", "#238636", "#9e6a03"],
      notes:
        "Human Agent Processes: build the factory that works for you. Each pattern is a hand-off between people and agents, wired to a GitHub event. IssueOps: an issue is opened, an agent triages it and comments, and a maintainer picks it up. DailyOps: a scheduled agent scans the repository and files an issue. ReviewOps: a pull request is marked ready for review, an agent reviews it and leaves comments, and a maintainer takes it from there. LabelOps: adding a label starts an agent that investigates, comments, and hands back to a maintainer. These are illustrative patterns with synthetic examples, not activity attributed to particular people or teams.",
      enter() {
        hT0 = 0;
        hCyc = -1;
      },
      tick(t) {
        hapStep(t);
      },
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Illustrative patterns"), h("h2", {}, letters("Human Agent "), letters("Processes", "l-grad"))),
        h("div", { class: "fm-quote hap-quote" }, "Build the ", h("em", {}, "factory"), " that works for you."),
        hapSvg
      ),
    });

    // Repo Assist: the maintenance loop next to its backlog burndown
    resetIndex();
    const raCap = h("div", { class: "fm-cap ra-cap-flow" });
    // PR Review²: the classic review gains a ready-for-review step that starts agentic reviewers and a Sous Chef loop before a human merges
    {
      resetIndex();
      const VW = 1700,
        VH = 680;
      const ico = (n, x, y, sz, fill = "#f0f6fc") => `<g transform="translate(${x - sz / 2},${y - sz / 2}) scale(${sz / (n === "merge" ? 16 : 24)})" fill="${fill}">${OCTI[n].map(d => `<path d="${d}"/>`).join("")}</g>`;
      const agent = (id, x, y, w, label) => `<g class="fm-task" id="${id}"><g class="fm-pill"><rect x="${x}" y="${y - 25}" width="${w}" height="50" rx="25"/><text x="${x + 18 + w / 2}" y="${y + 8}">${label}</text></g>
        <circle cx="${x}" cy="${y}" r="34" fill="url(#prqAgent)"/>${ico("copilot", x, y + 1, 38)}</g>`;
      const human = (id, x, y, w, label, avatar = "assets/avatars/pelikhan.jpg") => `<g class="fm-task mra-dev" id="${id}"><g class="fm-pill b"><rect x="${x}" y="${y - 25}" width="${w}" height="50" rx="25"/><text x="${x + 18 + w / 2}" y="${y + 8}">${label}</text></g>
        <circle cx="${x}" cy="${y}" r="34" fill="#0d1117" stroke="#4493f8" stroke-width="3"/><image href="${avatar}" x="${x - 31}" y="${y - 31}" width="62" height="62" clip-path="url(#prqClip)"/></g>`;
      const box = (id, x, y, icon) => `<g class="fm-out" id="${id}"><rect x="${x - 36}" y="${y - 34}" width="72" height="68" rx="14"/>${ico(icon, x, y, 34)}</g>`;
      const REV = ["nRevA", "nRevB", "nRevC"];
      // [edge id, path, color, label, lx, ly, anchor]; fan-out/fan-in wires (no label) light with their main edge
      const EDGES = [
        ["classic", "M90 374 V640 H1460 V598", "classic", ["classic review: straight to a human"], 760, 626, "middle"],
        ["p0", "M90 174 V302", "b", ["opens draft", "pull request"], 112, 236, "start"],
        ["p1", "M126 340 H462", "b", ["ready for", "review"], 250, 300, "middle"],
        ["p1a", "M400 340 V220 H462", "b", [], 0, 0, "start"],
        ["p1b", "M400 340 V460 H462", "b", [], 0, 0, "start"],
        ["p2", "M790 340 H860 V140 H1042", "p", ["review comments"], 950, 122, "middle"],
        ["p2a", "M790 220 H860", "p", [], 0, 0, "start"],
        ["p2b", "M790 460 H860 V340", "p", [], 0, 0, "start"],
        ["p3", "M1080 174 V522", "p", ["nudges", "@copilot"], 1102, 330, "start"],
        ["p4", "M1042 560 H500 V498", "p", ["pushes fixes · re-review"], 770, 544, "middle"],
        ["p5", "M1322 140 H1460 V302", "p", ["threads", "resolved"], 1482, 206, "start"],
        ["p6", "M1460 374 V522", "b", ["reviews &", "merges"], 1482, 438, "start"],
      ];
      const FAN = { p1: ["p1a", "p1b"], p2: ["p2a", "p2b"] };
      // [edge, node(s) lit on arrival, open threads shown on arrival]
      const SEQ = [
        ["p0", "nPRq"],
        ["p1", REV, 3],
        ["p2", "nChef"],
        ["p3", "nCopilotq"],
        ["p4", REV, 1],
        ["p2", "nChef"],
        ["p3", "nCopilotq"],
        ["p4", REV, 0],
        ["p2", "nChef"],
        ["p5", "nMaint"],
        ["p6", "nMergeq"],
      ];
      const LANE = { nAuthor: 0, nPRq: 0, nRevA: 1, nRevB: 1, nRevC: 1, nChef: 1, nCopilotq: 1, nMaint: 2, nMergeq: 2 };
      let g = `<defs><radialGradient id="prqAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
        <marker id="prqP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
        <marker id="prqB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker>
        <clipPath id="prqClip" clipPathUnits="objectBoundingBox"><circle cx=".5" cy=".5" r=".5"/></clipPath>
        <marker id="prqG" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#9198a1"/></marker></defs>`;
      g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
      [
        [0, 380, "AUTHOR"],
        [380, 1340, "REVIEW²"],
        [1340, VW, "MERGE"],
      ].forEach(([x0, x1, t], i) => {
        g += `<text class="fm-lane-t mra-lane" id="prqLane${i}" x="${(x0 + x1) / 2}" y="40">${t}</text>`;
        if (x0) g += `<line class="fm-div" x1="${x0}" y1="0" x2="${x0}" y2="${VH}"/>`;
      });
      for (const [id, d, c, lab, lx, ly, an] of EDGES) {
        const fan = id === "p2a" || id === "p2b";
        g += `<path class="fm-wire ${c}" id="${id}" d="${d}"${fan ? "" : ` marker-end="url(#prq${c === "p" ? "P" : c === "b" ? "B" : "G"})"`}/>`;
        if (lab.length) g += `<text class="mra-l ${c}" x="${lx}" y="${ly}" text-anchor="${an}">${lab.map((l, k) => `<tspan x="${lx}" dy="${k ? 26 : 0}">${l}</tspan>`).join("")}</text>`;
      }
      g += human("nAuthor", 90, 140, 200, "Developer") + box("nPRq", 90, 340, "pr");
      g += agent("nRevA", 500, 220, 290, "Matt Pocock skills") + agent("nRevB", 500, 340, 290, "Code quality") + agent("nRevC", 500, 460, 290, "Test quality");
      g += agent("nChef", 1080, 140, 242, "PR Sous Chef") + agent("nCopilotq", 1080, 560, 200, "Copilot");
      g += human("nMaint", 1460, 340, 220, "Developer", "assets/avatars/lpcox.jpg") + box("nMergeq", 1460, 560, "merge");
      g += `<text class="prq-thr" id="prqThr" x="968" y="356"></text>`;
      g += `<circle class="fm-dot mra-dot" r="8"/>`;
      const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      svg.setAttribute("class", "fm-svg rpa-svg");
      svg.setAttribute("viewBox", `0 0 ${VW} ${VH}`);
      svg.style.setProperty("--c", "#ab7df8");
      svg.innerHTML = g;
      const $ = id => svg.querySelector("#" + id),
        dot = svg.querySelector(".mra-dot"),
        thr = $("prqThr");
      const ease = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
      const STEP = 1050,
        START = 900,
        HOLD = 1800,
        T = [];
      let END = START;
      for (const s of SEQ) {
        T.push(END);
        END += STEP;
      }
      const CYC = END + HOLD;
      let t0 = 0,
        cyc = -1,
        popK = -1;
      const light = nids => {
        for (const nid of [].concat(nids)) {
          $(nid).classList.add("on");
          $("prqLane" + LANE[nid]).classList.add("on");
        }
      };
      const threads = k => {
        let n = null;
        for (let j = 0; j <= k; j++) if (SEQ[j][2] !== undefined) n = SEQ[j][2];
        thr.classList.toggle("show", n !== null);
        thr.classList.toggle("ok", n === 0);
        thr.textContent = n === null ? "" : n === 0 ? "✓ 0 open threads" : `${n} open thread${n > 1 ? "s" : ""}`;
      };
      slides.push({
        sound: "loop",
        palette: ["#1f6feb", "#8957e5", "#d29922", "#238636"],
        notes:
          "Pull Request Review squared. This is an illustrative flow with synthetic reviewers and counts. The classic review goes straight from the author to a human reviewer. Here we insert one step: marking the pull request ready for review. That event starts several agentic reviewers, such as one applying Matt Pocock's engineering skills, a code quality reviewer and a test quality reviewer, which post review comments. PR Sous Chef watches for unresolved threads and nudges Copilot to push fixes, which triggers another round of review. The agents loop until the threads are resolved, so the developer only sees a clean pull request. A human still reviews and merges.",
        enter() {
          t0 = 0;
          cyc = -1;
        },
        tick(t) {
          if (!t0) t0 = t;
          const e = t - t0,
            c = Math.floor(e / CYC),
            u = e % CYC;
          if (c !== cyc) {
            cyc = c;
            svg.querySelectorAll(".on, .hot").forEach(x => x.classList.remove("on", "hot"));
            thr.classList.remove("show", "ok");
            light("nAuthor");
            popK = -1;
          }
          svg.querySelectorAll(".fm-wire.hot").forEach(w => w.classList.remove("hot"));
          if (u < START || u >= END) {
            dot.style.opacity = 0;
            return;
          }
          let k = 0;
          while (k + 1 < SEQ.length && T[k + 1] <= u) k++;
          const [eid, nid] = SEQ[k],
            f = (u - T[k]) / STEP;
          const p = $(eid),
            n = p.getTotalLength(),
            q = p.getPointAtLength(n * ease(Math.min(1, f / 0.8)));
          p.classList.add("hot");
          for (const w of FAN[eid] || []) $(w).classList.add("hot");
          if (dot.style.opacity !== "1") sfx.pop(0);
          dot.style.opacity = 1;
          dot.setAttribute("cx", q.x);
          dot.setAttribute("cy", q.y);
          dot.setAttribute("fill", p.classList.contains("b") ? "#4493f8" : "#ab7df8");
          for (let j = 0; j < k; j++) light(SEQ[j][1]);
          threads(k - 1);
          if (f >= 0.8) {
            if (popK !== k) {
              popK = k;
              sfx.pop(k);
            }
            light(nid);
            threads(k);
          }
        },
        el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Ready for review · agentic reviewers · PR Sous Chef"), h("h2", {}, letters("Pull Request "), letters("Review²", "l-grad"))), svg),
      });
    }

    const raSvg = newSvg("fm-svg ra-flow");
    const raFlow = makeFlow(raSvg, raCap, [0]);
    const bdSvg = newSvg("fm-svg ra-chart");
    const bd = makeBurndown(bdSvg);
    const BD_PERIOD = 15000;
    let bdT0 = 0,
      bdRun = -1;
    slides.push({
      sound: "loop",
      palette: ["#238636", "#8957e5", "#1f6feb", "#9e6a03"],
      notes:
        "Repo Assist. The loop: on a schedule the agent picks a couple of maintenance tasks, such as labelling and investigating issues, nudging stale pull requests or proposing performance and testing improvements, and a maintainer reviews every result. The data: an approximate, illustrative redraw of a figure of open issues in F# open-source repositories, as a percentage of the count on adoption day. Backlogs are flat before Repo Assist, then burn down after adoption. Not exact data and not activity attributed to particular people or teams.",
      enter() {
        raFlow.reset();
        bdT0 = 0;
        bdRun = -1;
      },
      tick(t) {
        raFlow.step(t);
        if (!bdT0) bdT0 = t;
        const run = Math.floor((t - bdT0) / BD_PERIOD);
        if (run !== bdRun) {
          bdRun = run;
          bdSvg.innerHTML = bd.svgFor();
          bd.reset();
        }
        bd.tick((t - bdT0) % BD_PERIOD);
      },
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "A teammate on every repo"), h("h2", {}, letters("Repo "), letters("Assist", "l-grad"))),
        raCap,
        raSvg,
        h("div", { class: "fm-cap ra-cap-chart" }, h("b", { style: "color:#3fb950" }, "Backlog burndown"), h("span", {}, " · open issues after adoption")),
        bdSvg
      ),
    });
  }

  // Research → Assign → Refine: deep research files an issue, it is assigned to Copilot, and the pull request is refined with the developer
  {
    resetIndex();
    const VW = 1700,
      VH = 680;
    const ico = (n, x, y, sz, fill = "#f0f6fc") => `<g transform="translate(${x - sz / 2},${y - sz / 2}) scale(${sz / (n === "merge" ? 16 : 24)})" fill="${fill}">${OCTI[n].map(d => `<path d="${d}"/>`).join("")}</g>`;
    const agent = (id, x, y, w, label) => `<g class="fm-task" id="${id}"><g class="fm-pill"><rect x="${x}" y="${y - 25}" width="${w}" height="50" rx="25"/><text x="${x + 18 + w / 2}" y="${y + 8}">${label}</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="url(#rpaAgent)"/>${ico("copilot", x, y + 1, 38)}</g>`;
    const dev = (id, x, y) => `<g class="fm-task mra-dev" id="${id}"><g class="fm-pill b"><rect x="${x}" y="${y - 25}" width="200" height="50" rx="25"/><text x="${x + 18 + 100}" y="${y + 8}">Developer</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="#0d1117" stroke="#4493f8" stroke-width="3"/><image href="assets/avatars/mnkiefer.jpg" x="${x - 31}" y="${y - 31}" width="62" height="62" clip-path="url(#rpaClip)"/></g>`;
    const box = (id, x, y, icon) => `<g class="fm-out" id="${id}"><rect x="${x - 36}" y="${y - 34}" width="72" height="68" rx="14"/>${ico(icon, x, y, 34)}</g>`;
    // Two synthetic source files as syntax-coloured bars; rows 3–6 of A repeat as rows 4–7 of B.
    const TOK = ["#ff7b72", "#d2a8ff", "#79c0ff", "#a5d6ff", "#8b949e"];
    const DUP = [
      [16, [28, 90, 24]],
      [32, [60, 40]],
      [32, [44, 70, 18]],
      [16, [24, 52]],
    ];
    const FILES = [
      ["handlers/checkout.go", 24, [[0, [34, 60, 20]], [16, [50, 70]], [16, [40, 30, 50]], ...DUP, [16, [64, 30]], [0, [10]]], 3],
      ["handlers/cart.go", 266, [[0, [34, 48, 26]], [16, [70, 30]], [16, [56]], [16, [36, 40, 30]], ...DUP, [0, [10]]], 4],
    ];
    const ROW0 = 258,
      ROWH = 18,
      CW = 220;
    const codeScan = () => {
      let c = `<g class="rpa-code" id="nCode">`;
      for (const [name, x, rows, d] of FILES) {
        c += `<rect class="card" x="${x}" y="214" width="${CW}" height="200" rx="12"/><text class="rpa-file" x="${x + 14}" y="236">${name}</text>`;
        c += `<rect class="rpa-dup" x="${x + 6}" y="${ROW0 + d * ROWH - 12}" width="${CW - 12}" height="${DUP.length * ROWH + 6}" rx="6"/>`;
        rows.forEach(([ind, segs], i) => {
          let cx = x + 14 + ind;
          c += `<g class="rpa-row" data-y="${ROW0 + i * ROWH}">`;
          segs.forEach((w, j) => {
            c += `<rect x="${cx}" y="${ROW0 + i * ROWH - 4}" width="${w}" height="8" rx="4" fill="${TOK[(i + j) % TOK.length]}"/>`;
            cx += w + 6;
          });
          c += `</g>`;
        });
      }
      const ya = ROW0 + 3 * ROWH + 21,
        yb = ROW0 + 4 * ROWH + 21;
      c += `<path class="rpa-link" d="M238 ${ya} C255 ${ya} 255 ${yb} 272 ${yb}"/><circle class="rpa-link dot" cx="255" cy="${(ya + yb) / 2}" r="5"/>`;
      c += `<text class="rpa-badge" x="24" y="446">≈ duplicate block · 94% similar</text>`;
      // the beam is carried by two researcher agents, one on each side of the files
      c += `<g class="rpa-scan" id="rpaScan"><rect x="24" y="-28" width="462" height="28" fill="url(#rpaBeam)"/><line x1="24" y1="0" x2="486" y2="0"/>`;
      for (const x of [8, 502]) c += `<circle cx="${x}" cy="0" r="14" fill="url(#rpaAgent)"/>${ico("copilot", x, 1, 18)}`;
      return c + `</g></g>`;
    };
    const scan = f => {
      const s = $("rpaScan"),
        y = 248 + Math.min(1, f / 0.75) * 164;
      s.setAttribute("transform", `translate(0,${y})`);
      s.style.opacity = f < 0.85 ? 1 : Math.max(0, 1 - (f - 0.85) / 0.15);
      svg.querySelectorAll(".rpa-row").forEach(r => r.classList.toggle("seen", +r.dataset.y <= y));
      if (f >= 0.68 && !$("nCode").querySelector(".rpa-dup.on")) {
        svg.querySelectorAll(".rpa-dup, .rpa-link, .rpa-badge").forEach(x => x.classList.add("on"));
        sfx.pop(5);
      }
    };
    // [edge id, path, color, label, lx, ly, anchor]
    const EDGES = [
      ["e0", "M200 168 V206", "p", ["scans for duplicate code"], 222, 196, "start"],
      ["e1", "M494 340 H816", "p", ["files issue"], 655, 324, "middle"],
      ["e2", "M860 304 V140 H1258", "p", ["assigned", "to Copilot"], 882, 222, "start"],
      ["e4", "M1300 168 V292", "p", ["works on issue,", "opens pull request"], 1322, 222, "start"],
      ["e5", "M1300 372 V500", "b", ["assigns"], 1322, 442, "start"],
      ["e6", "M1504 540 H1584", "b", ["review &", "merge"], 1544, 594, "middle"],
      ["e7", "M1522 512 V168", "b", ["iterates"], 1544, 350, "start"],
      ["e8", "M1624 574 V650 H10 V140 H82", "p day", ["every day, the loop runs again"], 860, 636, "middle"],
    ];
    // the token's journey: [edge, node lit on arrival, duration]; a null edge is the duplicate-code scan
    const SEQ = [
      ["e0", "nCode"],
      [null, "nCode", 3000],
      ["e1", "nIssue"],
      ["e2", "nCopilot"],
      ["e4", "nPR"],
      ["e5", "nDev"],
      ["e7", "nCopilot"],
      ["e6", "nMerge"],
      ["e8", "nResearch", 1800],
    ];
    const LANE = { nResearch: 0, nCode: 0, nIssue: 1, nCopilot: 2, nPR: 2, nDev: 2, nMerge: 2 };
    let g = `<defs><clipPath id="rpaClip" clipPathUnits="objectBoundingBox"><circle cx=".5" cy=".5" r=".5"/></clipPath><radialGradient id="rpaAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
      <marker id="rpaP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
      <linearGradient id="rpaBeam" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#ab7df8" stop-opacity="0"/><stop offset="1" stop-color="#ab7df8" stop-opacity=".35"/></linearGradient>
      <marker id="rpaB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker></defs>`;
    g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
    [
      [0, 560, "RESEARCH"],
      [560, 1160, "ASSIGN"],
      [1160, VW, "REFINE"],
    ].forEach(([x0, x1, t], i) => {
      g += `<text class="fm-lane-t mra-lane" id="rpaLane${i}" x="${(x0 + x1) / 2}" y="40">${t}</text>`;
      if (x0) g += `<line class="fm-div" x1="${x0}" y1="0" x2="${x0}" y2="${VH}"/>`;
    });
    for (const [id, d, c, lab, lx, ly, an] of EDGES) {
      g += `<path class="fm-wire ${c}" id="${id}" d="${d}" marker-end="url(#rpa${c.startsWith("p") ? "P" : "B"})"/>`;
      g += `<text class="mra-l ${c}" x="${lx}" y="${ly}" text-anchor="${an}">${lab.map((l, k) => `<tspan x="${lx}" dy="${k ? 26 : 0}">${l}</tspan>`).join("")}</text>`;
    }
    g += agent("nResearch", 120, 140, 250, "Deep Research") + agent("nCopilot", 1300, 140, 236, "Copilot");
    g += codeScan() + box("nIssue", 860, 340, "issue") + box("nPR", 1300, 332, "pr") + box("nMerge", 1624, 540, "merge");
    g += dev("nDev", 1300, 540);
    g += `<g class="rpa-day">${ico("calendar", 30, 92, 26, "#ab7df8")}<text id="rpaDay" x="52" y="101">DAY 1</text></g>`;
    g += `<circle class="fm-dot mra-dot" r="8"/>`;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "fm-svg rpa-svg");
    svg.setAttribute("viewBox", `0 0 ${VW} ${VH}`);
    svg.style.setProperty("--c", "#ab7df8");
    svg.innerHTML = g;
    const $ = id => svg.querySelector("#" + id),
      dot = svg.querySelector(".mra-dot");
    const ease = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
    const STEP = 1050,
      START = 900,
      HOLD = 800,
      T = [];
    let END = START;
    for (const s of SEQ) {
      T.push(END);
      END += s[2] || STEP;
    }
    const CYC = END + HOLD;
    let t0 = 0,
      cyc = -1,
      popK = -1;
    const light = nid => {
      $(nid).classList.add("on");
      $("rpaLane" + LANE[nid]).classList.add("on");
    };
    slides.push({
      sound: "loop",
      palette: ["#8957e5", "#1f6feb", "#238636", "#bf3989"],
      notes:
        "Research, assign, refine: the pattern behind workflows such as Daily Test Improver and Daily Perf Improver. This is an illustrative flow with synthetic files. Deep research agents scan the repository, here for duplicate code: two handlers share a near-identical block, and the finding is filed as an issue. The issue is assigned to Copilot, which works on it and opens a pull request. The pull request is assigned back to the developer, who refines it by iterating with Copilot, then reviews and merges. Humans stay in the loop at the merge. The workflow runs on a daily schedule, so the next morning research starts again on the improved code, and the loop repeats every day.",
      enter() {
        t0 = 0;
        cyc = -1;
      },
      tick(t) {
        if (!t0) t0 = t;
        const e = t - t0,
          c = Math.floor(e / CYC),
          u = e % CYC;
        if (c !== cyc) {
          cyc = c;
          $("rpaDay").textContent = `DAY ${c + 1}`;
          svg.querySelectorAll(".on, .hot, .seen").forEach(x => x.classList.remove("on", "hot", "seen"));
          $("rpaScan").style.opacity = 0;
          $("nResearch").classList.add("on");
          $("rpaLane0").classList.add("on");
          popK = -1;
        }
        svg.querySelectorAll(".fm-wire.hot").forEach(w => w.classList.remove("hot"));
        if (u < START || u >= END) {
          dot.style.opacity = 0;
          return;
        }
        let k = 0;
        while (k + 1 < SEQ.length && T[k + 1] <= u) k++;
        const [eid, nid, dur = STEP] = SEQ[k],
          f = (u - T[k]) / dur;
        if (!eid) {
          dot.style.opacity = 0;
          for (let j = 0; j <= k; j++) light(SEQ[j][1]);
          scan(f);
          return;
        }
        const p = $(eid),
          n = p.getTotalLength(),
          q = p.getPointAtLength(n * ease(Math.min(1, f / 0.8)));
        p.classList.add("hot");
        if (dot.style.opacity !== "1") sfx.pop(0);
        dot.style.opacity = 1;
        dot.setAttribute("cx", q.x);
        dot.setAttribute("cy", q.y);
        dot.setAttribute("fill", p.classList.contains("b") ? "#4493f8" : "#ab7df8");
        for (let j = 0; j < k; j++) light(SEQ[j][1]);
        if (f >= 0.8) {
          if (popK !== k) {
            popK = k;
            sfx.pop(k);
          }
          light(nid);
        }
      },
      el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Daily Test Improver · Daily Perf Improver · …"), h("h2", {}, letters("Research · Assign · "), letters("Refine", "l-grad"))), svg),
    });
  }

  // Research → Assign → Observe: production telemetry (OpenTelemetry) feeds the next round of research
  {
    resetIndex();
    const VW = 1700,
      VH = 680;
    const ico = (n, x, y, sz, fill = "#f0f6fc") => `<g transform="translate(${x - sz / 2},${y - sz / 2}) scale(${sz / (n === "merge" ? 16 : 24)})" fill="${fill}">${OCTI[n].map(d => `<path d="${d}"/>`).join("")}</g>`;
    const agent = (id, x, y, w, label) => `<g class="fm-task" id="${id}"><g class="fm-pill"><rect x="${x}" y="${y - 25}" width="${w}" height="50" rx="25"/><text x="${x + 18 + w / 2}" y="${y + 8}">${label}</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="url(#raoAgent)"/>${ico("copilot", x, y + 1, 38)}</g>`;
    const dev = (id, x, y) => `<g class="fm-task mra-dev" id="${id}"><g class="fm-pill b"><rect x="${x}" y="${y - 25}" width="200" height="50" rx="25"/><text x="${x + 18 + 100}" y="${y + 8}">Developer</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="#0d1117" stroke="#4493f8" stroke-width="3"/><image href="assets/avatars/dsyme.jpg" x="${x - 31}" y="${y - 31}" width="62" height="62" clip-path="url(#raoClip)"/></g>`;
    const box = (id, x, y, icon) => `<g class="fm-out" id="${id}"><rect x="${x - 36}" y="${y - 34}" width="72" height="68" rx="14"/>${ico(icon, x, y, 34)}</g>`;
    // [edge id, path, color, label, lx, ly, anchor]
    const EDGES = [
      ["q0", "M856 140 H352", "p", ["queries telemetry"], 600, 122, "middle"],
      ["e0", "M170 168 V312", "p", ["researches", "the repository"], 192, 236, "start"],
      ["e1", "M170 388 V522", "p", ["creates", "issues"], 192, 450, "start"],
      ["e2", "M208 560 H472", "p", ["picked up by"], 340, 544, "middle"],
      ["e3", "M738 560 H842", "p", ["assigns to", "Copilot"], 790, 502, "middle"],
      ["e4", "M1056 560 H1122", "p", ["PR"], 1089, 544, "middle"],
      ["e5", "M1160 526 V474 H880 V522", "b", ["iterates"], 1020, 464, "middle"],
      ["e6", "M1366 560 H1582", "b", ["review &", "merge"], 1474, 520, "middle"],
      ["e7", "M1620 524 V418", "b", ["deploy", "to prod"], 1600, 466, "end"],
    ];
    const SEQ = [
      ["q0", "nResearch"],
      ["e0", "nRepo"],
      ["e1", "nIssues"],
      ["e2", "nMonster"],
      ["e3", "nCopilot"],
      ["e4", "nDev"],
      ["e5", "nCopilot"],
      ["e6", "nMerge"],
      ["e7", "nProd"],
    ];
    const LANE = { nResearch: 0, nRepo: 0, nIssues: 0, nMonster: 1, nCopilot: 2, nDev: 2, nMerge: 2, nProd: 2 };
    // Simulated production fleet; every number below is generated in the browser.
    const SRV = [
      ["prod-1", "us-east-1", 1240, 80],
      ["prod-2", "eu-west-1", 880, 195],
      ["prod-3", "ap-south-1", 610, 310],
    ];
    const CX = 858,
      CR = 1190,
      PX = 1290,
      PW = 400,
      N = 48,
      ROUTES = [
        ["POST", "/checkout", 0.45],
        ["GET", "/cart", 0.25],
        ["GET", "/search", 0.2],
        ["GET", "/healthz", 0.1],
      ];
    const KIND = [
      ["#ab7df8", 0.6],
      ["#3fb950", 0.25],
      ["#d29922", 0.15],
    ];
    let g = `<defs><clipPath id="raoClip" clipPathUnits="objectBoundingBox"><circle cx=".5" cy=".5" r=".5"/></clipPath><radialGradient id="raoAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
      <marker id="raoP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
      <marker id="raoB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker></defs>`;
    g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
    [
      [0, 440, "RESEARCH"],
      [440, 820, "ASSIGN"],
      [820, VW, "OBSERVE"],
    ].forEach(([x0, x1, t], i) => {
      g += `<text class="fm-lane-t mra-lane" id="raoLane${i}" x="${(x0 + x1) / 2}" y="40">${t}</text>`;
      if (x0) g += `<line class="fm-div" x1="${x0}" y1="0" x2="${x0}" y2="${VH}"/>`;
    });
    for (const [id, d, c, lab, lx, ly, an] of EDGES) {
      g += `<path class="fm-wire ${c}" id="${id}" d="${d}" marker-end="url(#rao${c === "p" ? "P" : "B"})"/>`;
      g += `<text class="mra-l ${c}" x="${lx}" y="${ly}" text-anchor="${an}">${lab.map((l, k) => `<tspan x="${lx}" dy="${k ? 26 : 0}">${l}</tspan>`).join("")}</text>`;
    }
    // OTLP export wires from each server to the collector
    const wire = i => [PX - 2, SRV[i][3] + 50, CR + 2, 150 + i * 70];
    SRV.forEach((_, i) => {
      const [x0, y0, x1, y1] = wire(i);
      g += `<path class="fm-wire p" d="M${x0} ${y0} C1240 ${y0} 1240 ${y1} ${x1} ${y1}" style="opacity:.3"/>`;
    });
    g += `<text class="mra-l" x="1240" y="446" text-anchor="middle"><tspan x="1240">OTLP</tspan><tspan x="1240" dy="24">export</tspan></text>`;
    // the collector: live counters, a span stream, and the fleet p95 against its SLO
    g += `<g id="nCollector"><rect class="rao-card on" x="${CX}" y="80" width="${CR - CX}" height="340" rx="14" style="--rc:#ab7df8"/>
      <circle cx="${CX + 22}" cy="108" r="10" fill="none" stroke="#f5a800" stroke-width="3"/><circle cx="${CX + 22}" cy="108" r="4" fill="#4f62ad"/>
      <text class="rao-h" x="${CX + 42}" y="116">OTel Collector</text>
      <circle class="rao-led" cx="${CX + 24}" cy="136" r="4" fill="#3fb950"/><text class="rao-sub" x="${CX + 36}" y="141">otlp/grpc :4317 · receiving</text>`;
    [
      ["spans/s", 20],
      ["metrics/s", 130],
      ["logs/s", 240],
    ].forEach(([k, x], i) => (g += `<text class="rao-k" x="${CX + x}" y="174">${k}</text><text class="rao-v" id="raoCnt${i}" x="${CX + x}" y="196">0</text>`));
    g += `<line class="fm-div" x1="${CX + 14}" y1="210" x2="${CR - 14}" y2="210"/>`;
    for (let i = 0; i < 5; i++) g += `<text class="rao-span" id="raoSpan${i}" x="${CX + 20}" y="${234 + i * 22}"></text>`;
    g += `<line class="fm-div" x1="${CX + 14}" y1="336" x2="${CR - 14}" y2="336"/>
      <text class="rao-k" x="${CX + 20}" y="356">p95 latency · fleet</text><text class="rao-breach" id="raoBreach" x="${CR - 18}" y="356" text-anchor="end">⚠ SLO breach</text>
      <line class="rao-slo" x1="${CX + 20}" y1="385" x2="${CR - 18}" y2="385"/><polyline class="rao-spark" id="raoAgg" points=""/></g>`;
    // production servers
    g += `<g class="rao-prod" id="nProd">`;
    SRV.forEach(([name, region, , Y], i) => {
      g += `<rect class="rao-card on" id="raoCard${i}" x="${PX}" y="${Y}" width="${PW}" height="100" rx="14"/>
        <rect x="${PX + 14}" y="${Y + 18}" width="44" height="64" rx="5" fill="#161b22" stroke="#3d444d" stroke-width="1.5"/>`;
      for (let s = 0; s < 3; s++)
        g += `<rect x="${PX + 19}" y="${Y + 24 + s * 19}" width="34" height="14" rx="2" fill="#0d1117" stroke="#30363d"/><circle class="rao-led" cx="${PX + 47}" cy="${Y + 31 + s * 19}" r="2.5" fill="#3fb950" style="animation-delay:${-(i * 3 + s) * 0.37}s"/>`;
      g += `<text class="rao-h" x="${PX + 72}" y="${Y + 36}">${name}</text><text class="rao-sub" id="raoVer${i}" x="${PX + 158}" y="${Y + 35}">v1.4.2</text>
        <text class="rao-sub" x="${PX + 72}" y="${Y + 58}">${region} · err <tspan id="raoErr${i}">0%</tspan></text>
        <text class="rao-v" id="raoRps${i}" x="${PX + PW - 16}" y="${Y + 32}" text-anchor="end"></text>
        <text class="rao-v" id="raoP95${i}" x="${PX + PW - 16}" y="${Y + 56}" text-anchor="end"></text>
        <polyline class="rao-spark" id="raoSpark${i}" points=""/>`;
    });
    g += `</g>`;
    g += agent("nResearch", 90, 140, 250, "Deep Research") + agent("nMonster", 510, 560, 220, "Issue Monster") + agent("nCopilot", 880, 560, 170, "Copilot");
    g += box("nRepo", 170, 350, "repo") + box("nIssues", 170, 560, "issue") + box("nMerge", 1620, 560, "merge") + dev("nDev", 1160, 560);
    g += `<g id="raoPkts"></g><circle class="fm-dot mra-dot" r="8"/>`;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "fm-svg rpa-svg");
    svg.setAttribute("viewBox", `0 0 ${VW} ${VH}`);
    svg.style.setProperty("--c", "#ab7df8");
    svg.innerHTML = g;
    const $ = id => svg.querySelector("#" + id),
      dot = svg.querySelector(".mra-dot");
    const pkts = [];
    for (let i = 0; i < 28; i++) {
      const c = document.createElementNS("http://www.w3.org/2000/svg", "circle");
      c.setAttribute("class", "rao-pkt");
      c.setAttribute("r", "4.5");
      $("raoPkts").appendChild(c);
      pkts.push({ el: c, t: -1 });
    }
    const pick = list => {
      let r = Math.random();
      for (const item of list) if ((r -= item[item.length - 1]) <= 0) return item;
      return list[0];
    };
    const hex = () => Math.floor(Math.random() * 65536).toString(16).padStart(4, "0");
    const fmt = n => Math.round(n).toLocaleString("en-US");
    const ease = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
    const STEP = 1050,
      START = 1600,
      HOLD = 4600,
      CYC = START + SEQ.length * STEP + HOLD;
    let t0 = 0,
      cyc = -1,
      popK = -1,
      deployed = false,
      lastSample = 0,
      lastPkt = 0,
      tick = 0,
      p95 = SRV.map(() => 450),
      hist = SRV.map((_, i) => Array.from({ length: N }, () => 430 + i * 35 + (Math.random() - 0.5) * 50)),
      agg = Array.from({ length: N }, () => 465 + (Math.random() - 0.5) * 30),
      spans = [];
    const light = nid => {
      $(nid).classList.add("on");
      $("raoLane" + LANE[nid]).classList.add("on");
    };
    const deploy = () => {
      deployed = true;
      SRV.forEach((_, i) => ($("raoVer" + i).textContent = "v1.4.3"));
    };
    const spark = (el, data, x0, x1, yb, h) =>
      el.setAttribute("points", data.map((v, j) => `${(x0 + ((x1 - x0) * j) / (N - 1)).toFixed(1)},${(yb - (Math.min(v, 600) / 600) * h).toFixed(1)}`).join(" "));
    const sample = () => {
      tick++;
      let rpsSum = 0;
      SRV.forEach(([, , base, Y], i) => {
        const target = deployed ? 165 + i * 14 : 430 + i * 35;
        p95[i] += (target - p95[i]) * 0.09 + (Math.random() - 0.5) * target * 0.12;
        hist[i].push(p95[i]);
        if (hist[i].length > N) hist[i].shift();
        const rps = base * (1 + 0.08 * Math.sin(tick / 9 + i) + (Math.random() - 0.5) * 0.06),
          err = Math.max(0, (p95[i] > 300 ? 1.8 : 0.2) + (Math.random() - 0.5) * 0.3),
          warn = p95[i] > 300;
        rpsSum += rps;
        $("raoRps" + i).textContent = `${fmt(rps)} rps`;
        const pe = $("raoP95" + i);
        pe.textContent = `p95 ${Math.round(p95[i])}ms`;
        pe.setAttribute("class", "rao-v " + (warn ? "warn" : "ok"));
        const ee = $("raoErr" + i);
        ee.textContent = `${err.toFixed(1)}%`;
        ee.setAttribute("class", warn ? "warn" : "ok");
        $("raoCard" + i).style.setProperty("--rc", warn ? "#d29922" : "#3fb950");
        const sp = $("raoSpark" + i);
        sp.setAttribute("stroke", warn ? "#d29922" : "#3fb950");
        spark(sp, hist[i], PX + 72, PX + PW - 16, Y + 92, 22);
      });
      const a = p95.reduce((s, v) => s + v, 0) / p95.length;
      agg.push(a);
      if (agg.length > N) agg.shift();
      const ag = $("raoAgg");
      ag.setAttribute("stroke", a > 300 ? "#d29922" : "#3fb950");
      spark(ag, agg, CX + 20, CR - 18, 410, 50);
      $("raoBreach").style.opacity = a > 300 ? 1 : 0;
      $("raoCnt0").textContent = fmt(rpsSum * 3.2);
      $("raoCnt1").textContent = fmt(420 + Math.random() * 40);
      $("raoCnt2").textContent = fmt(rpsSum * 0.6 + (a > 300 ? 300 : 0));
      if (tick % 2) {
        const i = Math.floor(Math.random() * SRV.length),
          [m, route] = pick(ROUTES),
          ms = route === "/checkout" ? p95[i] * (0.55 + Math.random() * 0.5) : 12 + Math.random() * 70;
        spans.unshift(`<tspan class="id">${hex()}</tspan> ${SRV[i][0]}  ${m} ${route}  <tspan class="${ms > 300 ? "warn" : "ok"}">${Math.round(ms)}ms</tspan>`);
        spans.length = Math.min(spans.length, 5);
        spans.forEach((s, k) => ($("raoSpan" + k).innerHTML = s));
      }
    };
    const pump = (t, dt) => {
      if (t - lastPkt > 110) {
        lastPkt = t;
        const p = pkts.find(q => q.t < 0);
        if (p) {
          p.t = 0;
          p.w = wire(Math.floor(Math.random() * SRV.length));
          p.el.setAttribute("fill", pick(KIND)[0]);
        }
      }
      for (const p of pkts) {
        if (p.t < 0) continue;
        p.t += dt / 750;
        if (p.t >= 1) {
          p.t = -1;
          p.el.style.opacity = 0;
          continue;
        }
        const [x0, y0, x1, y1] = p.w,
          u = p.t,
          v = 1 - u,
          x = v * v * v * x0 + 3 * v * u * (v * 1240 + u * 1240) + u * u * u * x1,
          y = v * v * v * y0 + 3 * v * u * (v * y0 + u * y1) + u * u * u * y1;
        p.el.setAttribute("cx", x.toFixed(1));
        p.el.setAttribute("cy", y.toFixed(1));
        p.el.style.opacity = 1;
      }
    };
    let prevT = 0;
    slides.push({
      sound: "loop",
      palette: ["#8957e5", "#238636", "#9e6a03", "#1f6feb"],
      notes:
        "Research, assign, observe: an agentic feedback loop. The production fleet, its numbers and the span stream are simulated in the browser. Servers export traces, metrics and logs over OTLP to an OpenTelemetry collector. A deep research agent queries that telemetry, notices the p95 latency on checkout breaching its SLO, researches the repository and files issues. Issue Monster assigns them to Copilot, the developer iterates, reviews and merges, and the deploy brings latency back under the SLO, which is observed again in the next round.",
      enter() {
        t0 = prevT = lastSample = lastPkt = 0;
        cyc = -1;
      },
      tick(t) {
        if (!t0) t0 = prevT = t;
        const dt = Math.min(100, t - prevT);
        prevT = t;
        if (t - lastSample > 220) {
          lastSample = t;
          sample();
        }
        pump(t, dt);
        const e = t - t0,
          c = Math.floor(e / CYC),
          u = e % CYC;
        if (c !== cyc) {
          cyc = c;
          svg.querySelectorAll(".fm-task.on, .fm-out.on, .mra-lane.on, .rao-prod.on, .hot").forEach(x => x.classList.remove("on", "hot"));
          $("raoLane2").classList.add("on");
          deployed = false;
          SRV.forEach((_, i) => ($("raoVer" + i).textContent = "v1.4.2"));
          popK = -1;
        }
        const k = Math.floor((u - START) / STEP),
          f = ((u - START) % STEP) / STEP;
        svg.querySelectorAll(".fm-wire.hot").forEach(w => w.classList.remove("hot"));
        if (u < START || k >= SEQ.length) {
          if (k >= SEQ.length) {
            if (!deployed) deploy();
            SEQ.forEach(([, id]) => light(id));
          }
          dot.style.opacity = 0;
          return;
        }
        const [eid, nid] = SEQ[k],
          p = $(eid),
          n = p.getTotalLength(),
          q = p.getPointAtLength(n * ease(Math.min(1, f / 0.8)));
        p.classList.add("hot");
        if (dot.style.opacity !== "1") sfx.pop(0);
        dot.style.opacity = 1;
        dot.setAttribute("cx", q.x);
        dot.setAttribute("cy", q.y);
        dot.setAttribute("fill", p.classList.contains("b") ? "#4493f8" : "#ab7df8");
        for (let j = 0; j < k; j++) light(SEQ[j][1]);
        if (f >= 0.8) {
          if (popK !== k) {
            popK = k;
            sfx.pop(k);
            if (nid === "nProd") deploy();
          }
          light(nid);
        }
      },
      el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Agentic feedback loop"), h("h2", {}, letters("Research · Assign · "), letters("Observe", "l-grad"))), svg),
    });
  }

  // Mine → Refine → Apply: harvesting developer interventions into linters
  {
    resetIndex();
    const RX = 460,
      VW = 1700 + RX,
      VH = 680,
      MINX = RX + 120,
      MINY = 140;
    const ico = (n, x, y, sz, fill = "#f0f6fc") => `<g transform="translate(${x - sz / 2},${y - sz / 2}) scale(${sz / (n === "merge" ? 16 : 24)})" fill="${fill}">${OCTI[n].map(d => `<path d="${d}"/>`).join("")}</g>`;
    const agent = (id, x, y, w, label) => `<g class="fm-task" id="${id}"><g class="fm-pill"><rect x="${x}" y="${y - 25}" width="${w}" height="50" rx="25"/><text x="${x + 18 + w / 2}" y="${y + 8}">${label}</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="url(#mraAgent)"/>${ico("copilot", x, y + 1, 38)}</g>`;
    const dev = (id, x, y) => `<g class="fm-task mra-dev" id="${id}"><g class="fm-pill b"><rect x="${x}" y="${y - 25}" width="200" height="50" rx="25"/><text x="${x + 18 + 100}" y="${y + 8}">Developer</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="#0d1117" stroke="#4493f8" stroke-width="3"/><image href="assets/avatars/pelikhan.jpg" x="${x - 31}" y="${y - 31}" width="62" height="62" clip-path="url(#mraClip)"/></g>`;
    const box = (id, x, y, icon) => `<g class="fm-out" id="${id}"><rect x="${x - 36}" y="${y - 34}" width="72" height="68" rx="14"/>${ico(icon, x, y, 34)}</g>`;
    // [edge id, path, color, label, lx, ly, anchor]
    const EDGES = [
      ["e0", "M200 168 V292", "p", ["mines patterns", "autonomously"], 222, 222, "start"],
      ["e1", "M200 372 V500", "p", ["opens", "analyzer PR"], 222, 430, "start"],
      ["e2", "M238 540 H606", "p", ["analyzer rules"], 420, 524, "middle"],
      ["e3", "M640 506 V372", "b", ["reviews &", "merges"], 662, 436, "start"],
      ["e4", "M678 338 H882", "p", ["challenge"], 780, 322, "middle"],
      ["e5", "M920 300 V168", "p", ["autonomous", "hardening PR"], 942, 222, "start"],
      ["e6", "M1060 140 H1258", "p", ["hardened rules → repo"], 1158, 120, "middle"],
      ["e7", "M1300 168 V292", "p", ["groups findings,", "opens cleanup PRs"], 1322, 222, "start"],
      ["e8", "M1300 372 V500", "p", ["assigns"], 1322, 442, "start"],
      ["e9", "M1504 540 H1584", "b", ["review &", "merge"], 1544, 594, "middle"],
      ["e10", "M1522 512 V168", "b", ["iterates"], 1544, 350, "start"],
    ];
    // the token's journey: [edge, node lit on arrival]
    const SEQ = [
      ["e0", "nComment"],
      ["e1", "nPR1"],
      ["e2", "nDev1"],
      ["e3", "nIssue"],
      ["e4", "nPR2"],
      ["e5", "nRefiner"],
      ["e6", "nMonster"],
      ["e7", "nPR3"],
      ["e8", "nDev2"],
      ["e10", "nMonster"],
      ["e9", "nMerge"],
    ];
    const LANE = { nComment: 0, nPR1: 0, nDev1: 1, nIssue: 1, nPR2: 1, nRefiner: 1, nMonster: 2, nPR3: 2, nDev2: 2, nMerge: 2 };
    let g = `<defs><clipPath id="mraClip" clipPathUnits="objectBoundingBox"><circle cx=".5" cy=".5" r=".5"/></clipPath><radialGradient id="mraAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
      <marker id="mraP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
      <marker id="mraB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker></defs>`;
    g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
    g += `<text class="fm-lane-t mra-lane on" x="${RX / 2}" y="40">HUMANS ↔ AGENTS</text><line class="fm-div" x1="${RX}" y1="0" x2="${RX}" y2="${VH}"/>
      <clipPath id="mraFeedClip"><rect x="16" y="164" width="${RX - 32}" height="${VH - 172}"/></clipPath>
      <rect class="mra-repo" x="16" y="78" width="${RX - 32}" height="${VH - 82}" rx="16"/>
      <text class="mra-repo-t" x="38" y="114">▣ example/repo · synthetic activity</text>
      <g id="mraBeams"></g><g id="mraFeed" clip-path="url(#mraFeedClip)"></g>
      <text class="mra-seen" id="mraSeen" x="${MINX - 34}" y="92"></text>
      <g transform="translate(${RX},0)">`;
    [
      [0, 520, "MINE"],
      [520, 1140, "CHALLENGE"],
      [1140, 1700, "APPLY"],
    ].forEach(([x0, x1, t], i) => {
      g += `<text class="fm-lane-t mra-lane" id="lane${i}" x="${(x0 + x1) / 2}" y="40">${t}</text>`;
      if (x0) g += `<line class="fm-div" x1="${x0}" y1="0" x2="${x0}" y2="${VH}"/>`;
    });
    for (const [id, d, c, lab, lx, ly, an] of EDGES) {
      g += `<path class="fm-wire ${c}" id="${id}" d="${d}" marker-end="url(#mra${c === "p" ? "P" : "B"})"/>`;
      g += `<text class="mra-l ${c}" x="${lx}" y="${ly}" text-anchor="${an}">${lab.map((l, k) => `<tspan x="${lx}" dy="${k ? 26 : 0}">${l}</tspan>`).join("")}</text>`;
    }
    g += agent("nMiner", 120, 140, 230, "Linter Miner") + agent("nRefiner", 800, 140, 240, "Linter Refiner") + agent("nMonster", 1300, 140, 236, "LintMonster");
    g += box("nComment", 200, 332, "comment") + box("nPR1", 200, 540, "pr") + box("nIssue", 640, 338, "issue") + box("nPR2", 920, 338, "pr") + box("nPR3", 1300, 332, "pr") + box("nMerge", 1624, 540, "merge");
    g += dev("nDev1", 640, 540) + dev("nDev2", 1300, 540);
    g += `<circle class="fm-dot mra-dot" r="8"/></g>`;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "fm-svg mra-svg");
    svg.setAttribute("viewBox", `0 0 ${VW} ${VH}`);
    svg.style.setProperty("--c", "#ab7df8");
    svg.innerHTML = g;
    const $ = id => svg.querySelector("#" + id),
      dot = svg.querySelector(".mra-dot");
    const ease = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
    const STEP = 1050,
      START = 900,
      HOLD = 2600,
      CYC = START + SEQ.length * STEP + HOLD;
    let t0 = 0,
      cyc = -1,
      popK = -1;
    // humans and agents interacting in the repo; the miner watches every exchange
    const FEED = [
      ["reviewer", "review", "don't swallow this error", "error"],
      [0, "fix", "wrapped with fmt.Errorf(%w)"],
      ["maintainer", "review", "use errors.Is, not ==", "errors.Is"],
      [0, "fix", "updated 3 call sites"],
      ["reviewer", "issue", "flaky test in example_test.go"],
      [0, "CI Doctor", "proposed a test fix"],
      ["maintainer", "review", "again: errors.Is, not ==", "errors.Is"],
      [0, "fix", "switched to errors.Is"],
      ["reviewer", "review", "no context.TODO in handlers"],
      [0, "fix", "threaded ctx through"],
      ["maintainer", "review", "wrap this error with %w", "error"],
      [0, "Repo Assist", "labelled 4 issues"],
    ];
    const feed = $("mraFeed"),
      beams = $("mraBeams"),
      seen = $("mraSeen"),
      ROWH = 84,
      FBOT = VH - 52;
    let rows = [],
      fi = 0,
      lastAdd = 0,
      hits = 0;
    const esc = x => x.replace(/&/g, "&amp;").replace(/</g, "&lt;");
    const addRow = instant => {
      if (!instant) sfx.notify(!!FEED[fi % FEED.length][0]);
      const [who, kind, msg, pat] = FEED[fi++ % FEED.length],
        el = document.createElementNS("http://www.w3.org/2000/svg", "g");
      const body = pat ? esc(msg).replace(esc(pat), `<tspan class="pat">${esc(pat)}</tspan>`) : esc(msg);
      el.setAttribute("class", "mra-row" + (who ? "" : " ag") + (instant ? "" : " new"));
      el.innerHTML =
        (who ? `<circle cx="58" cy="0" r="26" fill="#0d1117" stroke="#4493f8" stroke-width="2"/>${ico("person", 58, 0, 30)}` : `<circle cx="58" cy="0" r="26" fill="url(#mraAgent)"/>${ico("copilot", 58, 1, 30)}`) +
        `<text class="mra-who" x="96" y="-8">${who || "copilot"} · ${kind}</text><text class="mra-msg" x="96" y="20">${body}</text>`;
      feed.appendChild(el);
      rows.push(el);
      rows.forEach((r, k) => {
        r.style.transform = `translateY(${FBOT - (rows.length - 1 - k) * ROWH}px)`;
      });
      if (rows.length > 8) rows.shift().remove();
      if (instant) return;
      const y = FBOT,
        col = pat ? "#d29922" : who ? "#4493f8" : "#ab7df8";
      const b = document.createElementNS("http://www.w3.org/2000/svg", "path");
      b.setAttribute("class", "mra-beam");
      b.setAttribute("stroke", col);
      b.setAttribute("d", `M${RX - 20} ${y} C${RX + 30} ${y}, ${MINX - 60} ${MINY + 140}, ${MINX - 8} ${MINY + 36}`);
      beams.appendChild(b);
      setTimeout(() => b.remove(), 1400);
      $("nMiner").classList.remove("blink");
      void $("nMiner").getBBox();
      $("nMiner").classList.add("blink");
      if (pat) {
        hits++;
        seen.textContent = `pattern seen ×${hits}`;
        seen.classList.remove("pop");
        void seen.getBBox();
        seen.classList.add("pop");
      }
    };
    const reset = () => {
      svg.querySelectorAll(".on, .hot").forEach(x => x.classList.remove("on", "hot"));
      $("nMiner").classList.add("on");
      $("lane0").classList.add("on");
    };
    slides.push({
      sound: "loop",
      palette: ["#8957e5", "#1f6feb", "#bf3989", "#238636"],
      notes:
        "Mine, refine, apply: harvesting developer interventions into linters. All activity and review messages shown here are synthetic examples, not quotations or records of real people. Repeated corrections can become analyzer proposals, reviewed and refined by a developer, then applied across the repository through cleanup pull requests. Human corrections become deterministic checks.",
      enter() {
        t0 = 0;
        cyc = -1;
        lastAdd = 0;
        hits = 0;
        seen.textContent = "";
        feed.innerHTML = "";
        beams.innerHTML = "";
        rows = [];
        fi = 0;
        for (let i = 0; i < 5; i++) addRow(true);
      },
      tick(t) {
        if (!t0) t0 = t;
        const e = t - t0,
          c = Math.floor(e / CYC),
          u = e % CYC;
        if (c !== cyc) {
          cyc = c;
          reset();
          popK = -1;
        }
        if (t - lastAdd > 1500) {
          lastAdd = t;
          addRow(false);
        }
        const k = Math.floor((u - START) / STEP),
          f = ((u - START) % STEP) / STEP;
        svg.querySelectorAll(".fm-wire.hot").forEach(w => w.classList.remove("hot"));
        if (u < START || k >= SEQ.length) {
          dot.style.opacity = 0;
          return;
        }
        const [eid, nid] = SEQ[k],
          p = $(eid),
          n = p.getTotalLength(),
          q = p.getPointAtLength(n * ease(Math.min(1, f / 0.8)));
        p.classList.add("hot");
        if (dot.style.opacity !== "1") sfx.pop(0);
        dot.style.opacity = 1;
        dot.setAttribute("cx", q.x);
        dot.setAttribute("cy", q.y);
        dot.setAttribute("fill", p.classList.contains("b") ? "#4493f8" : "#ab7df8");
        for (let j = 0; j < k; j++) {
          $(SEQ[j][1]).classList.add("on");
          $("lane" + LANE[SEQ[j][1]]).classList.add("on");
        }
        if (f >= 0.8) {
          if (popK !== k) {
            popK = k;
            sfx.pop(k);
          }
          $(nid).classList.add("on");
          $("lane" + LANE[nid]).classList.add("on");
        }
      },
      el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Harvesting developer interventions into linters"), h("h2", {}, letters("Mine · Refine · "), letters("Apply", "l-grad"))), svg),
    });
  }

  // Scan → Fix → Learn: the .NET runtime team's CI health loop; a scanner files Known Build Errors, a fixer attempts fixes, a feedback agent tunes both prompts
  {
    resetIndex();
    const VW = 1700,
      VH = 680;
    const ico = (n, x, y, sz, fill = "#f0f6fc") => `<g transform="translate(${x - sz / 2},${y - sz / 2}) scale(${sz / (n === "merge" ? 16 : 24)})" fill="${fill}">${OCTI[n].map(d => `<path d="${d}"/>`).join("")}</g>`;
    const agent = (id, x, y, w, label) => `<g class="fm-task" id="${id}"><g class="fm-pill"><rect x="${x}" y="${y - 25}" width="${w}" height="50" rx="25"/><text x="${x + 18 + w / 2}" y="${y + 8}">${label}</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="url(#sflAgent)"/>${ico("copilot", x, y + 1, 38)}</g>`;
    const maint = (id, x, y) => `<g class="fm-task mra-dev" id="${id}"><g class="fm-pill b"><rect x="${x}" y="${y - 25}" width="200" height="50" rx="25"/><text x="${x + 18 + 100}" y="${y + 8}">Maintainer</text></g>
      <circle cx="${x}" cy="${y}" r="34" fill="#0d1117" stroke="#4493f8" stroke-width="3"/>${ico("person", x, y, 34, "#79c0ff")}</g>`;
    const box = (id, x, y, icon) => `<g class="fm-out" id="${id}"><rect x="${x - 36}" y="${y - 34}" width="72" height="68" rx="14"/>${ico(icon, x, y, 34)}</g>`;
    // synthetic outer-loop pipelines; one fails per cycle, and the fixer's outcome rotates with it
    const PIPES = ["stress · jit", "stress · gc", "pgo", "outer-loop", "arm64", "wasm"];
    const FAIL = [1, 3, 4],
      KIND = ["test hang", "assertion failure", "build break"];
    const ROW0 = 284,
      ROWH = 26;
    let pipes = `<g class="sfl-box" id="sflPipes"><rect class="sfl-card" x="24" y="220" width="460" height="232" rx="12"/><text class="sfl-title" x="40" y="246">OUTER-LOOP PIPELINES</text>`;
    PIPES.forEach((p, i) => (pipes += `<g class="sfl-row sfl-pipe" data-y="${ROW0 + i * ROWH}"><text x="40" y="${ROW0 + i * ROWH}">${p}</text><circle cx="462" cy="${ROW0 + i * ROWH - 6}" r="7"/></g>`));
    pipes += `<text class="sfl-badge" id="sflBadge" x="40" y="440"></text>`;
    pipes += `<g class="sfl-scan" id="sflBeam"><rect x="24" y="-28" width="460" height="28" fill="url(#sflBeamG)"/><line x1="24" y1="0" x2="484" y2="0"/></g></g>`;
    const RUB = [
      ["ok", "✓ signature is specific"],
      ["ok", "✓ known-error data is valid"],
      ["ok", "✓ fixed, never muted"],
      ["ko", "✗ owner pinged wrongly"],
      ["ed", "+ prompt edit proposed"],
    ];
    let rubric = `<g class="sfl-box" id="sflRubric"><rect class="sfl-card" x="1316" y="270" width="364" height="190" rx="12"/><text class="sfl-title" x="1332" y="296">KPI TRACKER · RUBRIC</text>`;
    RUB.forEach(([c, t], i) => (rubric += `<g class="sfl-row sfl-rub"><text class="${c}" x="1332" y="${330 + i * 27}">${t}</text></g>`));
    rubric += `</g>`;
    const scanPipes = (f, c) => {
      const s = $("sflBeam"),
        y = 262 + Math.min(1, f / 0.75) * 166;
      s.setAttribute("transform", `translate(0,${y})`);
      s.style.opacity = f < 0.85 ? 1 : Math.max(0, 1 - (f - 0.85) / 0.15);
      svg.querySelectorAll(".sfl-pipe").forEach(r => r.classList.toggle("seen", +r.dataset.y <= y));
      if (ROW0 + FAIL[c % 3] * ROWH <= y && !$("sflBadge").classList.contains("on")) {
        $("sflBadge").classList.add("on");
        sfx.pop(5);
      }
    };
    const readRubric = f => svg.querySelectorAll(".sfl-rub").forEach((r, i) => r.classList.toggle("seen", f >= (i + 0.5) / (RUB.length + 0.5)));
    // [edge id, path, color, label, lx, ly, anchor, extra label class]
    const EDGES = [
      ["sflE0", "M110 174 V212", "p", ["scans failing runs"], 132, 200, "start"],
      ["sflE1", "M492 340 H596", "p", ["files Known", "Build Error"], 640, 404, "middle"],
      ["sflE2", "M640 304 V140 H818", "p", ["picks up", "open errors"], 662, 222, "start"],
      ["sflO0", "M860 174 V274 H760 V320", "p", ["small and build-validated:", "draft fix PR"], 880, 222, "start", "sfl-ol"],
      ["sflO1", "M860 174 V274 H900 V320", "p", ["plausible, unverified:", "help-wanted PR"], 880, 222, "start", "sfl-ol"],
      ["sflO2", "M860 174 V274 H1040 V320", "p", ["no code change possible:", "root-cause comment"], 880, 222, "start", "sfl-ol"],
      ["sflR0", "M760 394 V470 H860 V542", "b", ["maintainer", "reviews"], 840, 500, "end", "sfl-ol"],
      ["sflR1", "M900 394 V470 H860 V542", "b", ["maintainer", "reviews"], 840, 500, "end", "sfl-ol"],
      ["sflR2", "M1040 394 V470 H860 V542", "b", ["maintainer", "reviews"], 840, 500, "end", "sfl-ol"],
      ["sflE3", "M1072 580 H1190 V140 H1214", "b", ["reactions & reviews"], 1140, 622, "middle"],
      ["sflE4", "M1252 174 V292", "p", ["scores both", "against rubrics"], 1274, 222, "start"],
      ["sflE5", "M1252 366 V542", "p", ["proposes", "prompt edits"], 1274, 494, "start"],
      ["sflE6", "M1464 580 H1640 V655 H10 V140 H72", "b day", ["sharper prompts · the loop runs again"], 700, 642, "middle"],
    ];
    // the token's journey: [edge, node lit on arrival, duration]; SCAN and RUBRIC are in-place steps, {o} is this cycle's outcome
    const SEQ = [
      ["sflE0", "sflPipes"],
      ["SCAN", "sflPipes", 2600],
      ["sflE1", "sflKbe"],
      ["sflE2", "sflFixer"],
      ["sflO{o}", "sflOut{o}"],
      ["sflR{o}", "sflDev"],
      ["sflE3", "sflFeed"],
      ["sflE4", "sflKpi"],
      ["RUBRIC", "sflRubric", 2400],
      ["sflE5", "sflDev2"],
      ["sflE6", "sflScan", 1800],
    ];
    const LANE = { sflScan: 0, sflPipes: 0, sflKbe: 1, sflFixer: 1, sflOut0: 1, sflOut1: 1, sflOut2: 1, sflDev: 1, sflFeed: 2, sflKpi: 2, sflRubric: 2, sflDev2: 2 };
    let g = `<defs><radialGradient id="sflAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
      <marker id="sflP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
      <linearGradient id="sflBeamG" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#ab7df8" stop-opacity="0"/><stop offset="1" stop-color="#ab7df8" stop-opacity=".35"/></linearGradient>
      <marker id="sflB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker></defs>`;
    g += `<rect class="fm-lane" x="0" y="0" width="${VW}" height="60"/>`;
    [
      [0, 520, "SCAN · EVERY 12H"],
      [520, 1160, "FIX · WEEKDAYS"],
      [1160, VW, "LEARN · BI-WEEKLY"],
    ].forEach(([x0, x1, t], i) => {
      g += `<text class="fm-lane-t mra-lane" id="sflLane${i}" x="${(x0 + x1) / 2}" y="40">${t}</text>`;
      if (x0) g += `<line class="fm-div" x1="${x0}" y1="0" x2="${x0}" y2="${VH}"/>`;
    });
    for (const [id, d, c, lab, lx, ly, an, extra = ""] of EDGES) {
      g += `<path class="fm-wire ${c}" id="${id}" d="${d}" marker-end="url(#sfl${c.startsWith("p") ? "P" : "B"})"/>`;
      g += `<text class="mra-l ${c} ${extra}" x="${lx}" y="${ly}" text-anchor="${an}">${lab.map((l, k) => `<tspan x="${lx}" dy="${k ? 26 : 0}">${l}</tspan>`).join("")}</text>`;
    }
    g += agent("sflScan", 110, 140, 230, "CI Scanner") + agent("sflFixer", 860, 140, 200, "CI Fixer") + agent("sflFeed", 1252, 140, 210, "Feedback");
    g += pipes + box("sflKbe", 640, 340, "issue");
    g += box("sflOut0", 760, 360, "pr") + box("sflOut1", 900, 360, "pr") + box("sflOut2", 1040, 360, "comment");
    g += `<text class="sfl-rule" x="1150" y="520" text-anchor="end">✕ never disables a test</text>`;
    g += maint("sflDev", 860, 580) + box("sflKpi", 1252, 330, "issue") + rubric + maint("sflDev2", 1252, 580);
    g += `<g class="rpa-day">${ico("calendar", 30, 92, 26, "#ab7df8")}<text id="sflCycle" x="52" y="101">CYCLE 1</text></g>`;
    g += `<circle class="fm-dot mra-dot" r="8"/>`;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "fm-svg sfl-svg");
    svg.setAttribute("viewBox", `0 0 ${VW} ${VH}`);
    svg.style.setProperty("--c", "#ab7df8");
    svg.innerHTML = g;
    const $ = id => svg.querySelector("#" + id),
      dot = svg.querySelector(".mra-dot");
    const ease = u => (u < 0.5 ? 2 * u * u : 1 - (-2 * u + 2) ** 2 / 2);
    const STEP = 1050,
      START = 900,
      HOLD = 900,
      T = [];
    let END = START;
    for (const s of SEQ) {
      T.push(END);
      END += s[2] || STEP;
    }
    const CYC = END + HOLD;
    let t0 = 0,
      cyc = -1,
      popK = -1;
    const at = (s, o) => s.replace("{o}", o);
    const light = nid => {
      $(nid).classList.add("on");
      $("sflLane" + LANE[nid]).classList.add("on");
    };
    slides.push({
      sound: "loop",
      palette: ["#8957e5", "#1f6feb", "#2da44e", "#d29922"],
      notes:
        "Scan, fix, learn: a simplified rendering of the CI health process the .NET runtime team built from three agentic workflows. The pipelines, failures and rubric results shown are synthetic and illustrative. Every twelve hours a scanner reads failing outer-loop runs, such as stress, PGO or platform legs, and files a Known Build Error issue with a precise error signature, so the same failure is immediately ignorable in pull request CI. Detection only: it never touches the code. On weekdays a fixer picks up the open errors and always attempts a real fix. A small, build-validated change becomes a draft fix pull request; a plausible but unverified one becomes a help-wanted pull request that asks the likely author and area owners for help; when no code change is possible it posts one root-cause comment. It never disables or mutes a test. Maintainers review, close or comment. Every two weeks a feedback workflow reads those reactions, scores the scanner and the fixer against separate rubrics, and keeps a single KPI tracker issue with at most three proposed prompt edits. Humans apply the edits, the prompts get sharper, and the loop runs again.",
      enter() {
        t0 = 0;
        cyc = -1;
      },
      tick(t) {
        if (!t0) t0 = t;
        const e = t - t0,
          c = Math.floor(e / CYC),
          u = e % CYC,
          o = c % 3;
        if (c !== cyc) {
          cyc = c;
          $("sflCycle").textContent = `CYCLE ${c + 1}`;
          svg.querySelectorAll(".on, .hot, .seen").forEach(x => x.classList.remove("on", "hot", "seen"));
          svg.querySelectorAll(".sfl-pipe").forEach((r, i) => r.classList.toggle("bad", i === FAIL[o]));
          $("sflBadge").textContent = `✕ ${PIPES[FAIL[o]]}: ${KIND[o]}`;
          $("sflBeam").style.opacity = 0;
          light("sflScan");
          popK = -1;
        }
        svg.querySelectorAll(".fm-wire.hot").forEach(w => w.classList.remove("hot"));
        if (u < START || u >= END) {
          dot.style.opacity = 0;
          return;
        }
        let k = 0;
        while (k + 1 < SEQ.length && T[k + 1] <= u) k++;
        const [eid, nid, dur = STEP] = SEQ[k],
          f = (u - T[k]) / dur;
        if (eid === "SCAN" || eid === "RUBRIC") {
          dot.style.opacity = 0;
          for (let j = 0; j <= k; j++) light(at(SEQ[j][1], o));
          if (eid === "SCAN") scanPipes(f, c);
          else readRubric(f);
          return;
        }
        const p = $(at(eid, o)),
          n = p.getTotalLength(),
          q = p.getPointAtLength(n * ease(Math.min(1, f / 0.8)));
        p.classList.add("hot");
        if (dot.style.opacity !== "1") sfx.pop(0);
        dot.style.opacity = 1;
        dot.setAttribute("cx", q.x);
        dot.setAttribute("cy", q.y);
        dot.setAttribute("fill", p.classList.contains("b") ? "#4493f8" : "#ab7df8");
        for (let j = 0; j < k; j++) light(at(SEQ[j][1], o));
        if (f >= 0.8) {
          if (popK !== k) {
            popK = k;
            sfx.pop(k);
          }
          light(at(nid, o));
        }
      },
      el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Continuous CI health · as built by the .NET runtime team"), h("h2", {}, letters("Scan · Fix · "), letters("Learn", "l-grad"))), svg),
    });
  }

  // Cost management: guard, measure, optimize (+ model routing)
  {
    resetIndex();
    const STEPS = [
      { id: "on", n: 1, label: "Scheduling", c: "#4493f8", aic: 700, bar: "weekly" },
      { id: "steps", n: 2, label: "Deterministic", c: "#d29922", aic: 420, bar: "steps" },
      { id: "cost", n: 2, label: "Cost guard", c: "#f85149", aic: 420, bar: "caps" },
      { id: "agent", n: 5, label: "Sub-agent", c: "#3fb950", aic: 160, bar: "haiku" },
      { id: "router", n: 1, label: "Model routing", c: "#ab7df8", aic: 110, bar: "routed" },
    ];
    const BASE = 2800;
    const K = "#ff7b72",
      V = "#a5d6ff",
      G = "#7ee787",
      D = "#656c76",
      T = "#f0f6fc",
      C = "#8b949e",
      O = "#ffa657";
    const linesFor = s => {
      const L = [
        { t: [["---", D]] },
        {
          id: "on",
          t: [
            ["on: ", K],
            [s >= 1 ? "weekly" : "daily", V, "v"],
          ],
          k: s >= 1 ? "on2" : "on1",
        },
        { t: [["tools:", K]] },
        {
          t: [
            ["  github: ", K],
            ["pull_requests", G],
          ],
        },
        { t: [["safe-outputs:", K]] },
        { t: [["  create-issue:", K]] },
      ];
      if (s >= 2)
        L.push(
          {
            id: "steps",
            t: [
              ["steps: ", K],
              ["# deterministic compute", C],
            ],
          },
          {
            t: [
              ["  - run: ", K],
              ["gh pr list --json title,body > prs.json", G],
            ],
          }
        );
      if (s >= 3)
        L.push(
          {
            id: "cost",
            t: [
              ["max-ai-credits: ", K],
              ["500", O],
            ],
          },
          {
            t: [
              ["max-daily-ai-credits: ", K],
              ["1000", O],
            ],
          }
        );
      if (s >= 5)
        L.push({
          id: "router",
          t: [
            ["model: ", K],
            ["router", V],
          ],
        });
      L.push({ t: [["---", D]] }, { t: [["# Daily PR Summary", T]] }, { t: [["", T]], k: "b1" });
      if (s >= 4)
        L.push(
          {
            t: [
              ["(1) Summarize each PR in prs.json with the ", T],
              ["`pr-summary`", O],
              [" subagent", T],
            ],
          },
          { t: [["(2) Create executive summary from PR summaries into an issue", T]] },
          ...(s >= 5 ? [] : [{ t: [["", T]], k: "b2" }]),
          {
            id: "agent",
            t: [
              ["## agent: ", T],
              ["`pr-summary`", O],
            ],
          },
          { t: [["---", D]], k: "d3" },
          {
            t: [
              ["models: ", K],
              ["haiku", V],
            ],
          },
          { t: [["---", D]], k: "d4" },
          { t: [["Create an executive summary of the PR.", T]] }
        );
      else L.push({ t: [["Create an issue with the executive summary of the activity", T]] }, { t: [["in the repo yesterday.", T]] });
      return L;
    };
    const code = h("div", { class: "cm-code" });
    const ed = h("div", { class: "cm-ed" }, h("div", { class: "cm-tab" }, "⬇ daily-pr-summary.md", h("span", {}, "×"), h("b", { class: "sv" }, "")), h("div", { class: "cm-crumb" }, ".github › workflows › daily-pr-summary.md"), code);
    const sv = ed.querySelector(".sv");
    let seen = new Set();
    const render = s => {
      const L = linesFor(s),
        rows = {},
        now = new Set();
      code.replaceChildren();
      L.forEach((l, i) => {
        const key = l.k || l.t.map(x => x[0]).join("");
        now.add(key);
        const tx = h(
          "span",
          { class: "tx" },
          l.t.map(([x, c, cls]) => h("span", { class: cls || "", style: { color: c } }, x))
        );
        const row = h("div", { class: `cm-row${seen.size && !seen.has(key) ? (l.id === "on" ? " chg" : " new") : ""}` }, h("span", { class: "no" }, String(i + 1)), tx);
        if (seen.size && !seen.has(key)) row.querySelector(".tx").style.animationDelay = `${(i % 6) * 40}ms`;
        code.append(row);
        if (l.id) rows[l.id] = { i, tx };
      });
      seen = now;
      STEPS.slice(0, s).forEach((st, j) => {
        const r = rows[st.id];
        if (!r) return;
        let w = 0;
        for (let k = r.i; k < r.i + st.n; k++) w = Math.max(w, code.children[k].querySelector(".tx").offsetWidth);
        const hot = j === s - 1 ? " hot" : "";
        code.append(h("div", { class: `cm-hl${hot}`, "--c": st.c, style: { top: `${8 + r.i * 28}px`, height: `${st.n * 28}px`, width: `${w + 30}px` } }));
        code.append(h("div", { class: `cm-pill${hot}`, "--c": st.c, style: { left: "740px", top: `${8 + r.i * 28 - 9}px` } }, h("b", {}, String(j + 1).padStart(2, "0")), st.label));
      });
      sv.textContent = s ? `${s} / ${STEPS.length} optimizations` : "baseline";
    };
    const num = h("b", {}, "2,800"),
      delta = h("em", {}, "−96%");
    const chips = [
      ["Guard", "#f85149"],
      ["Measure", "#4493f8"],
      ["Optimize", "#3fb950"],
    ].map(([x, c]) => h("span", { "--c": c }, x));
    const BARS = [{ bar: "daily", c: "#8b949e", aic: BASE }, ...STEPS];
    const bars = BARS.map(b => h("div", { class: "cm-bar", "--c": b.c }, h("span", {}, b.aic.toLocaleString("en-US")), h("div", {}), h("small", {}, b.bar)));
    const guard = h("div", { class: "cm-guard" }, "🛡", h("span", {}, "Hard caps: 500 AIC / run · 1,000 AIC / day"));
    const meter = h(
      "div",
      { class: "cm-meter" },
      h("div", { class: "cm-gmo" }, chips),
      h("i", {}, "AI credits per week · illustrative"),
      h("div", { class: "cm-big" }, num, h("small", {}, "AIC"), delta),
      h("div", { class: "cm-bars" }, bars),
      guard
    );
    const cv = h("canvas", { width: 1260, height: 568 }),
      ctx = cv.getContext("2d");
    const route = h("div", { class: "cm-route" }, cv, h("i", {}, "model: router"));
    const MODELS = [
      ["robust", "#ab7df8", 15, 70, "costly failures"],
      ["balanced", "#4493f8", 3, 150, "general work"],
      ["economy", "#3fb950", 1, 230, "triage, summaries"],
    ];
    const AX = 60,
      AY = 150,
      RX = 230,
      EX = 430;
    let step = 0,
      t0 = 0,
      tStep = 0,
      last = 0,
      spawn = 0,
      shown = BASE,
      pk = [],
      hist = [];
    const pick = () => {
      const r = Math.random();
      if (step >= 5) return r < 0.12 ? 0 : r < 0.4 ? 1 : 2;
      if (step >= 4) return r < 0.35 ? 0 : 2;
      return 0;
    };
    const setStep = s => {
      step = s;
      tStep = performance.now();
      hist = [];
      render(s);
      bars.forEach((b, i) => {
        b.classList.toggle("on", i <= s);
        b.querySelector("div").style.height = i <= s ? `${Math.max(4, 96 * Math.sqrt(BARS[i].aic / BASE))}px` : "0";
      });
      chips[0].classList.toggle("on", s >= 3);
      chips[1].classList.add("on");
      chips[2].classList.toggle("on", s >= 1);
      guard.classList.toggle("on", s >= 3);
      delta.classList.toggle("on", s >= STEPS.length);
      route.classList.toggle("on", s >= 5);
    };
    const drawRoute = (t, dt) => {
      ctx.setTransform(2, 0, 0, 2, 0, 0);
      ctx.clearRect(0, 0, 630, 284);
      const routed = step >= 5;
      if (t - spawn > 140) {
        spawn = t;
        const m = pick();
        pk.push({ m, p: 0, sp: 0.0009 + Math.random() * 0.0004 });
        hist.push(m);
        if (hist.length > 60) hist.shift();
      }
      ctx.lineWidth = 2;
      MODELS.forEach(([, c, , y], i) => {
        ctx.strokeStyle = c + (routed || (step >= 4 && i !== 1) || i === 0 ? "88" : "22");
        ctx.setLineDash([5, 7]);
        ctx.lineDashOffset = -t / 30;
        ctx.beginPath();
        ctx.moveTo(RX, AY);
        ctx.bezierCurveTo(RX + 90, AY, EX - 90, y, EX, y);
        ctx.stroke();
      });
      ctx.setLineDash([]);
      ctx.strokeStyle = "#ffffff30";
      ctx.beginPath();
      ctx.moveTo(AX + 26, AY);
      ctx.lineTo(RX - 22, AY);
      ctx.stroke();
      ctx.fillStyle = "#8957e5";
      ctx.shadowColor = "#ab7df8";
      ctx.shadowBlur = 20;
      ctx.beginPath();
      ctx.arc(AX, AY, 24, 0, 7);
      ctx.fill();
      ctx.shadowBlur = 0;
      ctx.fillStyle = "#f0f6fc";
      ctx.font = "700 14px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      ctx.textAlign = "center";
      ctx.fillText("agent", AX, AY + 46);
      ctx.save();
      ctx.translate(RX, AY);
      ctx.rotate(Math.PI / 4 + (routed ? t / 900 : 0));
      ctx.fillStyle = routed ? "#ab7df8" : "#30363d";
      ctx.shadowColor = "#ab7df8";
      ctx.shadowBlur = routed ? 26 + 10 * Math.sin(t / 200) : 0;
      ctx.fillRect(-16, -16, 32, 32);
      ctx.restore();
      ctx.shadowBlur = 0;
      ctx.fillStyle = routed ? "#d2a8ff" : "#656c76";
      ctx.fillText(routed ? "mode: auto" : "fixed model", RX, AY + 46);
      const cnt = [0, 0, 0];
      hist.forEach(m => cnt[m]++);
      MODELS.forEach(([name, c, price, y, bar], i) => {
        const share = hist.length ? cnt[i] / hist.length : 0;
        ctx.fillStyle = c;
        ctx.shadowColor = c;
        ctx.shadowBlur = share > 0.05 ? 18 : 0;
        ctx.beginPath();
        ctx.arc(EX, y, 10, 0, 7);
        ctx.fill();
        ctx.shadowBlur = 0;
        ctx.textAlign = "left";
        ctx.fillStyle = "#f0f6fc";
        ctx.font = "700 15px 'Monaspace Neon', ui-monospace, Menlo, monospace";
        ctx.fillText(`router/${name}`, EX + 22, y - 6);
        ctx.fillStyle = "#9198a1";
        ctx.font = "500 12px 'Monaspace Neon', ui-monospace, Menlo, monospace";
        ctx.fillText(bar, EX + 22, y + 12);
        ctx.fillStyle = c;
        ctx.fillText(`${"$".repeat(price > 10 ? 3 : price > 1 ? 2 : 1)} · ${Math.round(share * 100)}% of runs`, EX + 22, y + 28);
      });
      for (const q of pk) {
        const was = q.p;
        q.p += dt * q.sp;
        if (was < 0.4 && q.p >= 0.4) sfx.route(q.m);
        const [, c, , y] = MODELS[q.m];
        let x, yy;
        if (q.p < 0.4) {
          const u = q.p / 0.4;
          x = AX + 26 + (RX - AX - 26) * u;
          yy = AY;
        } else {
          const u = (q.p - 0.4) / 0.6,
            a = 1 - u;
          x = a ** 3 * RX + 3 * a * a * u * (RX + 90) + 3 * a * u * u * (EX - 90) + u ** 3 * EX;
          yy = a ** 3 * AY + 3 * a * a * u * AY + 3 * a * u * u * y + u ** 3 * y;
        }
        ctx.fillStyle = q.p < 0.4 ? "#f0f6fc" : c;
        ctx.shadowColor = ctx.fillStyle;
        ctx.shadowBlur = 12;
        ctx.beginPath();
        ctx.arc(x, yy, q.m === 0 ? 6 : q.m === 1 ? 4.5 : 3.5, 0, 7);
        ctx.fill();
      }
      ctx.shadowBlur = 0;
      pk = pk.filter(q => q.p < 1);
    };
    slides.push({
      sound: "coins",
      palette: ["#9e6a03", "#238636", "#1f6feb", "#8957e5"],
      notes:
        "Cost management: guard, measure, optimize. Start from a daily PR summary. 1) Scheduling: does it really need to run daily? Weekly cuts runs 7x. 2) Deterministic compute: fetch the PRs with a plain gh step instead of letting the agent explore with tools. 3) Guard: max-ai-credits per run and max-daily-ai-credits cap spend. 4) Sub-agents: fan per-PR summaries out to a pr-summary sub-agent on a smaller model. 5) Model routing: use the documented model: router modes to balance cost and quality for eligible main-agent runs. Numbers are illustrative, not internal usage or billing data: roughly 2,800 to 110 AIC a week. Keys: → next optimization.",
      enter() {
        t0 = last = spawn = 0;
        pk = [];
        hist = [];
        seen = new Set();
        shown = BASE;
        setStep(0);
      },
      onNext() {
        if (step < STEPS.length) {
          setStep(step + 1);
          return true;
        }
        return false;
      },
      onPrev() {
        if (step > 0) {
          setStep(step - 1);
          return true;
        }
        return false;
      },
      tick(t) {
        if (!t0) t0 = t;
        const dt = last ? Math.min(50, t - last) : 16;
        last = t;
        if (step < STEPS.length && t - t0 > 2600 && performance.now() - tStep > 3000) {
          setStep(step + 1);
          sfx.step(1);
        }
        const target = step ? STEPS[step - 1].aic : BASE;
        shown += (target - shown) * Math.min(1, dt / 220);
        num.textContent = Math.round(shown).toLocaleString("en-US");
        num.style.color = step >= STEPS.length ? "#3fb950" : "";
        drawRoute(t, dt);
      },
      el: h("section", { class: "slide cm" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Guard · measure · optimize"), h("h2", {}, letters("Cost "), letters("management", "l-grad"))), ed, meter, route),
    });
  }

  // Model routing: the router reads each run, weighs what a mistake would cost and picks the cheapest model and effort that is good enough
  {
    resetIndex();
    // Illustrative runs: each tagged phrase (k kind, s scope, d difficulty) lifts out into a label chip.
    const RUNS = [
      {
        wf: "daily-report",
        text: [["Summarize "], ["yesterday's activity", "s"], [" into a "], ["short", "d"], [" "], ["daily report", "k"], ["."]],
        chips: ["report", "repo-wide", "easy"],
        stakes: 0,
        why: "a report is easy to ignore",
        tier: 0,
        effort: "HIGH",
      },
      {
        wf: "security-audit",
        text: [["Audit", "k"], [" "], ["the auth module", "s"], [" for "], ["subtle injection bugs", "d"], ["."]],
        chips: ["security", "subsystem", "hard"],
        stakes: 1,
        why: "a missed bug would be silent",
        tier: 1,
        effort: "HIGH",
      },
      {
        wf: "pr-review",
        text: [["Review", "k"], [" "], ["this pull request", "s"], [" for "], ["common code smells", "d"], ["."]],
        chips: ["review", "multi-file", "medium"],
        stakes: 0,
        why: "a review is only a suggestion",
        tier: 0,
        effort: "XHIGH",
      },
      {
        wf: "release",
        text: [["Merge", "k"], [" "], ["the release branch", "s"], [" once "], ["checks are green", "d"], ["."]],
        chips: ["release", "repo-wide", "easy"],
        stakes: 2,
        why: "easy, but a merge can't be undone",
        tier: 2,
        effort: "MEDIUM",
      },
      {
        wf: "issue-triage",
        text: [["Label", "k"], [" "], ["the new issue", "s"], [" with "], ["one area tag", "d"], ["."]],
        chips: ["triage", "one issue", "trivial"],
        stakes: 0,
        why: "a label is easy to change",
        tier: 0,
        effort: "LOW",
      },
      {
        wf: "docs-sync",
        text: [["Update the docs", "k"], [" to match "], ["the new CLI flags", "s"], [" in "], ["a few lines", "d"], ["."]],
        chips: ["docs", "multi-file", "easy"],
        stakes: 0,
        why: "docs are easy to fix",
        tier: 0,
        effort: "MEDIUM",
      },
    ];
    const TIERS = [
      ["SMALL", "#3fb950", "$", "small"],
      ["MID-SIZE", "#4493f8", "$$", "mid-size"],
      ["FRONTIER", "#ab7df8", "$$$", "frontier"],
    ];
    const ZONES = [
      ["low", "economy", "#3fb950", 150],
      ["medium", "balanced", "#4493f8", 90],
      ["high", "robust", "#ab7df8", 30],
    ];
    const CHIPS = [
      ["kind", "#f778ba", "k"],
      ["scope", "#39c5cf", "s"],
      ["difficulty", "#e3b341", "d"],
    ];
    // Measured on gh-aw's own workflows: cost per run by day, relative to each workflow's own two weeks before routing (through 12 Nov 2026).
    const DAYS = [1.02, 1.02, 0.97, 1.06, 1.04, 1.05, 0.95, 1.06, 0.95, 1.05, 0.99, 0.99, 0.93, 0.89, 0.63, 0.5, 0.2, 0.2, 0.23, 0.31, 0.24, 0.15, 0.15];
    const ALPH = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789$-";
    // each run plays out at about 56% of its first pace (0.75 x 0.75), so the eye can follow every step
    const SLOW = 16 / 9;
    const svgOf = (cls, vb, inner) => {
      const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      s.setAttribute("class", cls);
      s.setAttribute("viewBox", vb);
      s.innerHTML = inner;
      return s;
    };

    // the stakes gauge: three zones on a half dial, and a sprung needle
    const GX = 190,
      GY = 200,
      GR = 128;
    const pt = (a, r) => [GX + r * Math.cos((a * Math.PI) / 180), GY - r * Math.sin((a * Math.PI) / 180)];
    const arc = (a0, a1) => {
      const [x0, y0] = pt(a0, GR),
        [x1, y1] = pt(a1, GR);
      return `M${x0.toFixed(1)} ${y0.toFixed(1)}A${GR} ${GR} 0 0 1 ${x1.toFixed(1)} ${y1.toFixed(1)}`;
    };
    let gs = `<path class="mr-track" d="${arc(180, 0)}"/>`;
    ZONES.forEach(([lab, , c, mid], i) => {
      const [lx, ly] = pt(mid, 164);
      gs += `<path class="mr-z" d="${arc(180 - i * 60 - (i ? 1.5 : 0), 120 - i * 60 + (i < 2 ? 1.5 : 0))}" stroke="${c}" style="color:${c}"/>`;
      gs += `<text class="mr-zl" x="${lx.toFixed(1)}" y="${(ly + 6).toFixed(1)}" fill="${c}">${lab}</text>`;
    });
    gs += `<g class="mr-needle"><path d="M${GX - 5} ${GY}L${GX} ${GY - 112}L${GX + 5} ${GY}Z"/><circle cx="${GX}" cy="${GY}" r="13"/></g>`;
    gs += `<text class="mr-read" x="${GX}" y="${GY + 46}"></text><text class="mr-read2" x="${GX}" y="${GY + 66}"></text>`;
    const gauge = svgOf("mr-gauge", "0 0 380 270", gs);
    const zones = [...gauge.querySelectorAll(".mr-z")],
      zlabs = [...gauge.querySelectorAll(".mr-zl")],
      needleEl = gauge.querySelector(".mr-needle"),
      read = gauge.querySelector(".mr-read"),
      read2 = gauge.querySelector(".mr-read2");

    // the run card: the workflow's model line, the task, its labels, the gauge and the reason
    const wf = h("span", { class: "mr-wf" }),
      text = h("div", { class: "mr-text" }),
      caret = h("span", { class: "pf-caret" }),
      beam = h("div", { class: "mr-beam" }),
      chips = CHIPS.map(([l, c]) => h("div", { class: "mr-chip", "--c": c }, h("i", {}, l), h("b", {}))),
      chipRow = h("div", { class: "mr-chips" }, chips),
      why = h("div", { class: "mr-why" });
    const card = h(
      "div",
      { class: "mr-run" },
      h(
        "div",
        { class: "mr-bar" },
        ["#ff5f57", "#febc2e", "#28c840"].map(c => h("i", { style: { background: c } })),
        wf,
        h("span", { class: "mr-gap" }),
        h("span", { class: "mr-model" }, "model: ", h("b", {}, "router"))
      ),
      text,
      beam,
      chipRow,
      gauge,
      why
    );
    const fields = [
      ["model", 8],
      ["effort", 7],
      ["cost", 3],
    ].map(([l, n]) => {
      const tiles = Array.from({ length: n }, () => h("span", { class: "mr-t" }, " "));
      return { el: h("div", { class: "mr-field" }, h("i", {}, l), h("div", { class: "mr-tiles" }, tiles)), tiles };
    });
    const tiles = fields.flatMap(f => f.tiles);
    const hero = h(
      "div",
      { class: "mr-hero" },
      card,
      h(
        "div",
        { class: "mr-flap" },
        h("b", { class: "mr-arrow" }, "→"),
        fields.map(f => f.el)
      )
    );

    // measured results: the day-by-day cost strip, with the switch to model: router on day 0
    let ss = `<line x1="0" y1="25" x2="544" y2="25"/>`;
    DAYS.forEach((v, i) => {
      ss += `<rect class="${i < 14 ? "pre" : "post"}" x="${(i * 23.6).toFixed(1)}" y="${(125 - v * 100).toFixed(1)}" width="18" height="${(v * 100).toFixed(1)}" rx="3" style="--i:${i}"/>`;
    });
    ss += `<line class="mk" x1="327.6" y1="6" x2="327.6" y2="128"/><text class="mk" x="336" y="46">model: router</text>`;
    ss += `<text x="0" y="148">two weeks before</text><text x="336" y="148">after</text>`;
    const big = h("b", {}, "0%");
    const proof = h(
      "div",
      { class: "mr-proof" },
      h("div", { class: "mr-pf-h" }, "gh-aw's own workflows", h("em", {}, "measured")),
      h("div", { class: "mr-big" }, big, h("span", {}, "cost per run", h("small", {}, "65 → 16 AI credits"))),
      svgOf("mr-strip", "0 0 544 150", ss),
      h("div", { class: "mr-held" }, h("span", {}, h("i", {}, "✓"), "run success", h("b", {}, "87.6% → 88.5%")), h("span", {}, h("i", {}, "✓"), "quality", h("b", {}, "83.9 → 83.6")))
    );

    // built for workflows that repeat: a week of one workflow's runs, one square each, coloured by the model it got (illustrative)
    const week = h("div", { class: "mr-week" }, ["M", "T", "W", "T", "F", "S", "S"].map(d => [h("i", {}, d), Array.from({ length: 24 }, () => h("b", {}))]).flat(2));
    const cells = [...week.querySelectorAll("b")];
    const runCount = h("b", {}, "0"),
      weekNo = h("span", {}, "week 1");
    const rep = h(
      "div",
      { class: "mr-rep" },
      h("div", { class: "mr-rh" }, "Built for workflows that repeat"),
      week,
      h(
        "div",
        { class: "mr-rf" },
        TIERS.map(([, c, , name]) => h("span", { "--c": c }, name)),
        h("em", {}, runCount, " runs · ", weekNo)
      )
    );
    const cv = h("canvas", { class: "mr-cv", width: 1920, height: 1080 }),
      cx = cv.getContext("2d");
    const sec = h("section", { class: "slide mr" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "The right model for every run"), h("h2", {}, letters("Model "), letters("routing", "l-grad"))), hero, proof, rep, cv);

    // builds: 0 the router at work, 1 measured results, 2 built for workflows that repeat
    let t0 = 0,
      last = 0,
      build = 0,
      tb = 0,
      si = 0,
      no = 0,
      run = null,
      needle = -90,
      nv = 0,
      nt = -90,
      flipT = 0,
      savedAt = 0,
      saved = false,
      weekAt = 0,
      wk = 1,
      lit = 0,
      picks = [],
      bits = [],
      ghosts = [];
    // sparks from an element's centre, in stage coordinates
    const burst = (el, c, n) => {
      const r = el.getBoundingClientRect(),
        s = sec.getBoundingClientRect(),
        k = 1920 / s.width,
        x = (r.left + r.width / 2 - s.left) * k,
        y = (r.top + r.height / 2 - s.top) * k;
      for (let i = 0; i < n; i++) {
        const a = Math.random() * Math.PI * 2,
          v = 0.1 + Math.random() * 0.3;
        bits.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v - 0.1, life: 1, c, s: 2 + Math.random() * 3 });
      }
    };
    const startRun = t => {
      const r = RUNS[si++ % RUNS.length];
      no += 1 + ((Math.random() * 4) | 0);
      run = { ...r, t, words: [], typed: -1, ok: [], read: false, gauged: false, said: false, flap: false, done: false };
      wf.replaceChildren(`${r.wf}.md`, h("small", {}, `run #${no}`));
      text.replaceChildren();
      let n = 0;
      for (const [s, k] of r.text) {
        const el = h("span", { class: k ? `mr-w ${k}` : "" });
        text.append(el);
        run.words.push({ el, s, k, at: n, lit: false });
        n += s.length;
      }
      run.len = n;
      chips.forEach((c, j) => {
        c.classList.remove("on");
        c.lastChild.textContent = r.chips[j];
      });
      [text, chipRow].forEach(x => x.classList.remove("mr-out"));
      [why, ...zones, ...zlabs].forEach(x => x.classList.remove("on"));
      // the previous pick fades back until the board flips to this run's pick
      tiles.forEach(x => x.classList.add("old"));
      nt = -90;
      read.textContent = read2.textContent = "";
    };
    const lift = (w, t) => {
      w.lit = true;
      w.el.classList.add("on");
      const j = CHIPS.findIndex(c => c[2] === w.k),
        chip = chips[j];
      const g = h("div", { class: "mr-ghost", "--c": CHIPS[j][1], style: { left: `${36 + w.el.offsetLeft}px`, top: `${84 + w.el.offsetTop}px` } }, w.s);
      card.append(g);
      void g.offsetWidth;
      g.style.transform = `translate(${chip.offsetLeft - w.el.offsetLeft}px, ${300 + chip.offsetTop - 84 - w.el.offsetTop}px) scale(.6)`;
      g.style.opacity = "0";
      ghosts.push({ g, t, chip });
      sfx.pop(j + 3);
    };
    const stepRun = t => {
      // run time at the first pace, so the steps below keep their relative timing
      const e = (t - run.t) / SLOW,
        n = Math.min(run.len, Math.floor((e / 520) * run.len));
      // untyped letters keep their place but stay invisible, so the lines never reflow while typing
      if (n !== run.typed) {
        run.typed = n;
        for (const w of run.words) {
          const k = Math.max(0, Math.min(w.s.length, n - w.at));
          w.el.replaceChildren(w.s.slice(0, k), ...(n >= w.at && k < w.s.length ? [caret] : []), h("span", { class: "mr-hide" }, w.s.slice(k)));
        }
      }
      // the beam reads the task; each tagged phrase lifts out into its label chip
      const u = (e - 620) / 600;
      beam.style.opacity = u > 0 && u < 1 ? 1 : 0;
      if (u > 0 && !run.read) {
        const x = Math.min(1, u) * 584;
        beam.style.transform = `translateX(${x}px)`;
        for (const w of run.words) if (w.k && !w.lit && x > w.el.offsetLeft + w.el.offsetWidth / 2) lift(w, t);
        run.read = u >= 1;
      }
      if (!run.gauged && e > 1350) {
        run.gauged = true;
        const [lab, mode, c, deg] = ZONES[run.stakes];
        nt = 90 - deg;
        zones[run.stakes].classList.add("on");
        zlabs[run.stakes].classList.add("on");
        read.textContent = `${lab.toUpperCase()} STAKES`;
        read.style.fill = c;
        read2.textContent = mode;
      }
      if (!run.said && e > 1600) {
        run.said = true;
        why.textContent = run.why;
        why.classList.add("on");
      }
      if (!run.flap && e > 1800) {
        run.flap = true;
        const [m, c, co] = TIERS[run.tier];
        run.target = m.padEnd(8) + run.effort.padEnd(7) + co.padEnd(3);
        run.c = c;
        run.settle = tiles.map((_, i) => t + (220 + i * 28) * SLOW);
        tiles.forEach((x, i) => {
          x.classList.remove("set", "old");
          x.style.setProperty("--c", c);
          run.ok[i] = false;
        });
        flipT = 0;
      }
      if (run.flap && !run.done) {
        const flip = t - flipT > 55 * SLOW;
        if (flip) {
          flipT = t;
          sfx.mr.flap();
        }
        let left = 0;
        tiles.forEach((x, i) => {
          if (run.ok[i]) return;
          if (t >= run.settle[i]) {
            run.ok[i] = true;
            x.textContent = run.target[i];
            if (run.target[i] !== " ") x.classList.add("set");
          } else {
            left++;
            if (flip) x.textContent = ALPH[(Math.random() * ALPH.length) | 0];
          }
        });
        if (!left) {
          run.done = true;
          burst(fields[0].el, run.c, 26);
          sfx.mr.pick(run.tier);
        }
      }
      if (run.done && e > 3440) [text, chipRow].forEach(x => x.classList.add("mr-out"));
      if (run.done && e > 3700) startRun(t);
    };
    const newWeek = () => {
      picks = cells.map(() => {
        const r = Math.random();
        return r < 0.71 ? 0 : r < 0.87 ? 1 : 2;
      });
      lit = 0;
      cells.forEach(c => (c.className = ""));
    };
    const setBuild = (b, t) => {
      const was = build;
      build = b;
      tb = t;
      sec.classList.toggle("side", b >= 1);
      proof.classList.toggle("on", b >= 1);
      rep.classList.toggle("on", b >= 2);
      if (b === 1 && was < 1) {
        savedAt = t;
        saved = false;
        big.textContent = "0%";
      }
      if (b === 2 && was < 2) {
        wk = 1;
        weekNo.textContent = "week 1";
        newWeek();
        weekAt = t;
      }
      // stepping back cancels a count-up or a week that hasn't finished
      if (b < 1) savedAt = 0;
      if (b < 2) weekAt = 0;
    };
    slides.push({
      sound: "router",
      palette: ["#8957e5", "#238636", "#1f6feb", "#6e40c9"],
      notes:
        "Model routing. Every workflow run pays for a model, and the usual habit is to pin one strong model for every run. With one line, model: router, the router reads each run's task before the agent starts, labels the kind of work, its scope and its difficulty, weighs what a mistake would cost (low, medium or high stakes, which gh-aw calls economy, balanced and robust), then picks the cheapest model and reasoning effort that is good enough, using benchmark results that ship with the AWF firewall. It runs inside the firewall, respects the workflow's model policy, and gh aw audit shows what it picked and why. The tasks, labels and picks in the animation are illustrative. The results are measured on gh-aw's own workflows: across 127 routed workflows and 3,097 routed runs through 12 Nov 2026, each compared with its own two weeks before routing, cost per run fell 75% (65 to 16 AI credits) while run success (87.6% to 88.5%) and judged quality (83.9 to 83.6 out of 100) held steady. Most routers pick a model for one prompt in a chat, with a person watching. This one is built for workflows that repeat: the same job runs on every schedule, issue and pull request with no one watching, so the router weighs the stakes of each run itself, and the saving repeats on every run. The week of runs is illustrative. Keys: → next step.",
      enter() {
        t0 = last = build = si = 0;
        run = null;
        // jump straight back to the centred layout instead of gliding there
        hero.style.transition = "none";
        sec.classList.remove("side");
        void hero.offsetWidth;
        [proof, rep, why, ...chips, ...zones, ...zlabs].forEach(x => x.classList.remove("on"));
        big.textContent = "0%";
        savedAt = weekAt = 0;
        ghosts.forEach(g => g.g.remove());
        ghosts = [];
        bits = [];
        tiles.forEach(x => {
          x.textContent = " ";
          x.classList.remove("set", "old");
        });
        text.replaceChildren();
        wf.replaceChildren();
        read.textContent = read2.textContent = "";
        beam.style.opacity = 0;
        needle = nt = -90;
        nv = 0;
        no = 4180 + ((Math.random() * 60) | 0);
        sfx.mr.reset();
        cx.clearRect(0, 0, 1920, 1080);
      },
      onNext() {
        if (build >= 2) return false;
        setBuild(build + 1, performance.now());
        return true;
      },
      onPrev() {
        if (!build) return false;
        setBuild(build - 1, performance.now());
        return true;
      },
      tick(t) {
        if (!t0) {
          t0 = last = tb = t;
          hero.style.transition = "";
        }
        const dt = Math.min(50, t - last);
        last = t;
        // two full runs before the results, then the week
        if (build === 0 && t - tb > 900 + 2 * 3700 * SLOW) setBuild(1, t);
        else if (build === 1 && t - tb > 7500) setBuild(2, t);
        if (!run && t - t0 > 900) startRun(t);
        if (run) stepRun(t);
        // damped spring at the slower pace, so the needle overshoots and settles
        const ds = dt / SLOW;
        nv += (0.00025 * (nt - needle) - 0.011 * nv) * ds;
        needle += nv * ds;
        needleEl.setAttribute("transform", `rotate(${needle.toFixed(2)} ${GX} ${GY})`);
        ghosts = ghosts.filter(g => {
          if (t - g.t > 380 * SLOW) g.chip.classList.add("on");
          if (t - g.t < 700 * SLOW) return true;
          g.g.remove();
          return false;
        });
        if (savedAt) {
          const u = Math.max(0, Math.min(1, (t - savedAt - 950) / 800));
          if (u > 0 && !saved) {
            saved = true;
            sfx.mr.saved();
          }
          big.textContent = `${u ? "−" : ""}${Math.round(75 * (1 - (1 - u) ** 3))}%`;
          if (u >= 1) savedAt = 0;
        }
        // the week fills run by run at the same slower pace, then the next week starts
        if (weekAt) {
          const e = t - weekAt,
            n = Math.max(0, Math.min(cells.length, Math.floor((e - 600) / 35)));
          for (; lit < n; lit++) {
            cells[lit].className = `t${picks[lit]} hot`;
            if (wk === 1 && lit % 24 === 23) sfx.mr.day(Math.floor(lit / 24));
          }
          runCount.textContent = ((wk - 1) * cells.length + lit).toLocaleString("en-US");
          if (lit === cells.length && e > 600 + cells.length * 35 + 2400) {
            weekNo.textContent = `week ${++wk}`;
            newWeek();
            weekAt = t - 200;
          }
        }
        cx.clearRect(0, 0, 1920, 1080);
        for (const b of bits) {
          b.x += b.vx * dt;
          b.y += b.vy * dt;
          b.vy += 0.0005 * dt;
          b.life -= dt / 850;
          cx.globalAlpha = Math.max(0, b.life);
          cx.fillStyle = b.c;
          cx.fillRect(b.x - b.s / 2, b.y - b.s / 2, b.s, b.s);
        }
        cx.globalAlpha = 1;
        bits = bits.filter(b => b.life > 0);
      },
      el: sec,
    });
  }

  // Any agent, any provider: one Markdown workflow retargets engine + model provider; tokens stream from the provider through the same guardrails
  {
    resetIndex();
    const PROV = {
      github: { name: "GitHub", sub: "Copilot models", c: "#a371f7" },
      anthropic: { name: "Anthropic", sub: "Claude models", c: "#e3845f" },
      openai: { name: "OpenAI", sub: "GPT models", c: "#56d4bc" },
    };
    const PIDS = Object.keys(PROV);
    const COMBOS = [
      ["copilot", "github"],
      ["claude", "anthropic"],
      ["codex", "openai"],
      ["claude", "github"],
      ["pi", "github"],
      ["codex", "github"],
    ];
    const ENG = { copilot: "Copilot", claude: "Claude", codex: "Codex", pi: "Pi" };
    // Monochrome marks (currentColor): Copilot, Claude, OpenAI from Simple Icons (CC0), GitHub from Octicons (MIT),
    // Pi from pi.dev, Anthropic from Simple Icons (CC0). Trademarks belong to their respective owners.
    const LOGO = {"copilot": ["0 0 24 24", ["M23.922 16.997C23.061 18.492 18.063 22.02 12 22.02 5.937 22.02.939 18.492.078 16.997A.641.641 0 0 1 0 16.741v-2.869a.883.883 0 0 1 .053-.22c.372-.935 1.347-2.292 2.605-2.656.167-.429.414-1.055.644-1.517a10.098 10.098 0 0 1-.052-1.086c0-1.331.282-2.499 1.132-3.368.397-.406.89-.717 1.474-.952C7.255 2.937 9.248 1.98 11.978 1.98c2.731 0 4.767.957 6.166 2.093.584.235 1.077.546 1.474.952.85.869 1.132 2.037 1.132 3.368 0 .368-.014.733-.052 1.086.23.462.477 1.088.644 1.517 1.258.364 2.233 1.721 2.605 2.656a.841.841 0 0 1 .053.22v2.869a.641.641 0 0 1-.078.256Zm-11.75-5.992h-.344a4.359 4.359 0 0 1-.355.508c-.77.947-1.918 1.492-3.508 1.492-1.725 0-2.989-.359-3.782-1.259a2.137 2.137 0 0 1-.085-.104L4 11.746v6.585c1.435.779 4.514 2.179 8 2.179 3.486 0 6.565-1.4 8-2.179v-6.585l-.098-.104s-.033.045-.085.104c-.793.9-2.057 1.259-3.782 1.259-1.59 0-2.738-.545-3.508-1.492a4.359 4.359 0 0 1-.355-.508Zm2.328 3.25c.549 0 1 .451 1 1v2c0 .549-.451 1-1 1-.549 0-1-.451-1-1v-2c0-.549.451-1 1-1Zm-5 0c.549 0 1 .451 1 1v2c0 .549-.451 1-1 1-.549 0-1-.451-1-1v-2c0-.549.451-1 1-1Zm3.313-6.185c.136 1.057.403 1.913.878 2.497.442.544 1.134.938 2.344.938 1.573 0 2.292-.337 2.657-.751.384-.435.558-1.15.558-2.361 0-1.14-.243-1.847-.705-2.319-.477-.488-1.319-.862-2.824-1.025-1.487-.161-2.192.138-2.533.529-.269.307-.437.808-.438 1.578v.021c0 .265.021.562.063.893Zm-1.626 0c.042-.331.063-.628.063-.894v-.02c-.001-.77-.169-1.271-.438-1.578-.341-.391-1.046-.69-2.533-.529-1.505.163-2.347.537-2.824 1.025-.462.472-.705 1.179-.705 2.319 0 1.211.175 1.926.558 2.361.365.414 1.084.751 2.657.751 1.21 0 1.902-.394 2.344-.938.475-.584.742-1.44.878-2.497Z"]], "claude": ["0 0 24 24", ["m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"]], "codex": ["0 0 24 24", ["M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z"]], "pi": ["0 0 560 560", ["M420 280H280V140H0V0H420V280Z", "M560 560H420V280H560V560Z", "M140 560H0V140H140V280H280V420H140V560Z"]], "openai": ["0 0 24 24", ["M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z"]], "github": ["0 0 16 16", ["M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z"]], "anthropic": ["0 0 24 24", ["M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 20.459h3.7442l1.3693-3.5527h7.0052l1.3693 3.5528h3.7442L10.5363 3.5409Zm-.3712 10.2232 2.2914-5.9456 2.2914 5.9456Z"]]};
    const logo = id => {
      const s = h("span", { class: "ae-logo" });
      s.innerHTML = `<svg viewBox="${LOGO[id][0]}" aria-hidden="true" fill="currentColor">${LOGO[id][1].map(d => `<path d="${d}"/>`).join("")}</svg>`;
      return s;
    };
    const K = "#ff7b72",
      G = "#7ee787",
      D = "#656c76",
      T = "#f0f6fc",
      V = "#a5d6ff";
    const vId = h("span", { class: "ae-val" });
    const vPv = h("span", { class: "ae-val" });
    const caret = h("i", { class: "ae-caret" });
    const LINES = [
      [["---", D]],
      [
        ["on: ", K],
        ["daily", V],
      ],
      [["engine:", K]],
      [["  id: ", K], vId],
      [["  model-provider: ", K], vPv],
      [["tools:", K]],
      [
        ["  github: ", K],
        ["issues", G],
      ],
      [["safe-outputs:", K]],
      [["  add-labels:", K]],
      [["---", D]],
      [["# Daily issue triage", T]],
      [["Label new issues and suggest an owner.", T]],
    ];
    const rows = LINES.map((parts, i) => h("div", { class: "ae-row" + (i === 3 || i === 4 ? " hot" : "") }, h("span", { class: "no" }, String(i + 1)), h("span", {}, parts.map(p => (p instanceof Node ? p : h("span", { style: { color: p[1] } }, p[0]))))));
    const ed = h("div", { class: "ae-ed" }, h("div", { class: "ae-tab" }, "daily-triage.md", h("span", {}, "SYNTHETIC")), h("div", { class: "ae-code" }, rows));
    const arrow = h("div", { class: "ae-arrow" }, "gh aw compile");
    arrow.innerHTML += '<svg viewBox="0 0 150 40"><path d="M4 20 H136 M120 6 L140 20 L120 34"/></svg>';
    const provEls = PIDS.map(id => h("div", { class: "ae-prov", "--c": PROV[id].c }, logo(id), h("div", {}, h("b", {}, PROV[id].name), h("span", {}, PROV[id].sub))));
    const mono = h("div", { class: "ae-mono" });
    const nm = h("strong");
    const tokN = h("em", { class: "ae-tokn" }, "0");
    const core = h("div", { class: "ae-core" }, mono, h("div", {}, h("small", {}, "agent engine"), nm, tokN));
    const ring = (label, c, inset) => h("div", { class: "ae-ring", "--c": c, style: { inset: `${inset}px` } }, h("b", {}, label), h("i", {}, "✓ same"));
    const run = h("div", { class: "ae-run" }, ring("Actions runner · lock file", "#4493f8", 0), ring("Network firewall", "#d29922", 44), ring("Sandboxed container", "#f85149", 88), core, h("div", { class: "ae-facts" }, h("span", {}, h("b", {}, "0"), " secrets"), h("span", {}, "read-only tokens")));
    const TW = 744,
      TH = 640;
    const tc = h("canvas", { class: "ae-toks", width: TW, height: TH });
    const tx = tc.getContext("2d");
    const picks = Object.keys(ENG).map(id => h("div", { class: "ae-pick", "data-e": id }, h("b", {}, logo(id)), id));
    const tag = h("div", { class: "ae-tag" }, "Swap the ", h("em", {}, "engine"), ". Pick the ", h("em", {}, "provider"), ". Keep the guardrails.");
    const el = h(
      "section",
      { class: "slide ae" },
      h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "One workflow · swap the engine · pick the model provider"), h("h2", {}, letters("Any "), letters("agent", "l-grad ae-agent"), letters(", any "), letters("provider", "l-grad ae-prov-t"))),
      ed,
      arrow,
      h("div", { class: "ae-provs" }, provEls),
      run,
      tc,
      h("div", { class: "ae-picks" }, picks),
      tag
    );
    let cur = 0,
      phase = "hold",
      since = 0,
      tId = "",
      tPv = "",
      toks = [],
      spawnAt = 0,
      tokens = 0,
      shown = 0,
      gulp = 0,
      last = 0;
    const setCombo = i => {
      cur = i;
      const [eid, pid] = COMBOS[i];
      const c = PROV[pid].c;
      el.style.setProperty("--ec", c);
      mono.replaceChildren(logo(eid));
      nm.textContent = ENG[eid];
      provEls.forEach((p, j) => p.classList.toggle("on", PIDS[j] === pid));
      picks.forEach(p => p.classList.toggle("on", p.dataset.e === eid));
      core.classList.remove("swap");
      void core.offsetWidth;
      core.classList.add("swap");
    };
    // token stream: from the active provider card (top) down into the agent core
    const CORE = { x: TW / 2, y: 120 + 245 };
    const WORDS = ["label", " the", " issue", "triage", " owner", " bug", " new", " suggest", " needs", "-info", " repro", " docs", "ing", " area", ":ui", " P2"];
    const drawToks = (t, dt) => {
      tx.clearRect(0, 0, TW, TH);
      tx.font = "600 18px 'Monaspace Neon', monospace";
      tx.textAlign = "center";
      tx.textBaseline = "middle";
      const pid = COMBOS[cur][1],
        j = PIDS.indexOf(pid),
        sx = 124 + j * 248,
        sy = 70,
        c = PROV[pid].c;
      if (phase === "hold" && t > spawnAt) {
        spawnAt = t + 240;
        toks.push({ p: 0, sx: sx + (Math.random() - 0.5) * 140, sy: sy + 40, c, w: WORDS[(Math.random() * WORDS.length) | 0] });
      }
      for (const q of toks) {
        q.p += dt / 900;
        const u = Math.min(1, q.p);
        tx.globalAlpha = u > 0.85 ? (1 - u) / 0.15 : Math.min(1, u * 6);
        const x = q.sx + (CORE.x - q.sx) * u,
          y = q.sy + (CORE.y - 75 - q.sy) * u,
          bw = tx.measureText(q.w).width + 20;
        tx.fillStyle = "#0d1117";
        tx.strokeStyle = q.c;
        tx.lineWidth = 1.5;
        tx.beginPath();
        tx.roundRect(x - bw / 2, y - 15, bw, 30, 8);
        tx.fill();
        tx.stroke();
        tx.fillStyle = q.c;
        tx.fillText(q.w.trim(), x, y + 1);
        if (q.p >= 1) {
          tokens += 100 + ((Math.random() * 200) | 0);
          gulp = Math.min(1, gulp + 0.15);
        }
      }
      tx.globalAlpha = 1;
      toks = toks.filter(q => q.p < 1);
      gulp *= 0.94 ** (dt / 16);
      core.style.setProperty("--gl", `${40 + gulp * 90}px`);
      shown += (tokens - shown) * Math.min(1, dt / 160);
      tokN.textContent = "↓ " + Math.round(shown).toLocaleString("en-US") + " tokens";
    };
    slides.push({
      sound: "engine",
      palette: ["#8957e5", "#9e6a03", "#1f6feb", "#238636"],
      notes:
        "Any agent, any provider. The same Markdown workflow can target different coding agents and model providers by changing two frontmatter lines: engine id copilot, claude, codex or pi, and model-provider github, anthropic or openai. Claude can run on Anthropic or on GitHub Copilot inference; Codex on OpenAI or GitHub. The agent pulls tokens from whichever provider you pick, but the compiler keeps the same guardrails around it: the Actions runner and lock file, the network firewall, and the sandboxed container, which holds zero secrets and only read-only tokens. Token counts are simulated and the workflow is a synthetic example; see the engines and model routing reference for setup and availability.",
      enter() {
        tId = COMBOS[0][0];
        tPv = COMBOS[0][1];
        vId.textContent = tId;
        vPv.textContent = tPv;
        phase = "hold";
        since = last = spawnAt = 0;
        toks = [];
        tokens = shown = 0;
        setCombo(0);
      },
      tick(t) {
        const dt = last ? Math.min(50, t - last) : 16;
        last = t;
        if (!since) since = t;
        const el2 = t - since;
        const [nId, nPv] = COMBOS[(cur + 1) % COMBOS.length];
        if (phase === "hold" && el2 > 3000) {
          phase = "del";
          since = t;
        } else if (phase === "del" && el2 > 30) {
          since = t;
          if (tPv) tPv = tPv.slice(0, -1);
          else if (tId !== nId) tId = tId.slice(0, -1);
          vId.textContent = tId;
          vPv.textContent = tPv;
          if (!tPv && (tId === nId || !tId)) phase = "type";
        } else if (phase === "type" && el2 > 75) {
          since = t;
          if (tId !== nId) tId = nId.slice(0, tId.length + 1);
          else tPv = nPv.slice(0, tPv.length + 1);
          vId.textContent = tId;
          vPv.textContent = tPv;
          sfx.key(0.7);
          if (tId === nId && tPv === nPv) {
            setCombo((cur + 1) % COMBOS.length);
            sfx.pop(cur * 2);
            phase = "hold";
          }
        }
        drawToks(t, dt);
      },
      el,
    });
  }

  // Enterprise ready: one Markdown workflow compiles to plain Actions YAML, so it runs on any runner (larger, self-hosted), keeps the agent behind the egress firewall (optionally in a microVM), reuses any action from the ecosystem, exports OpenTelemetry traces, and keeps Apps, OIDC and governance
  {
    resetIndex();
    const RUN = [
      { k: "[self-hosted, linux]", c: "#d29922" },
      { k: "[self-hosted, arm64]", c: "#ab7df8" },
      { k: "[self-hosted, gpu]", c: "#4493f8" },
    ];
    const OTEL = { c: "#f0883e", ic: '<path d="M8 14h22M16 26h30M24 38h24M12 50h16"/><circle cx="8" cy="14" r="3"/><circle cx="16" cy="26" r="3"/><circle cx="24" cy="38" r="3"/><circle cx="12" cy="50" r="3"/>' };
    const SRV = '<rect x="10" y="10" width="44" height="13" rx="3"/><rect x="10" y="26" width="44" height="13" rx="3"/><rect x="10" y="42" width="44" height="13" rx="3"/><path d="M17 16.5h2M17 32.5h2M17 48.5h2M40 16.5h8M40 32.5h8M40 48.5h8"/>';
    const FW = { c: "#ff7b72", ic: '<path d="M32 6 54 14v16c0 14-9 24-22 28C19 54 10 44 10 30V14Z"/><path d="M22 32l7 7 13-14"/>' };
    const ECO = { c: "#3fb950", ic: '<rect x="8" y="8" width="20" height="20" rx="4"/><rect x="36" y="8" width="20" height="20" rx="4"/><rect x="8" y="36" width="20" height="20" rx="4"/><rect x="38" y="38" width="20" height="20" rx="4" transform="rotate(12 48 48)"/>' };
    const STEPS = ["checkout", "setup-node", "cache"];
    const CHIPS = [
      ["GitHub Apps", [6, 7, 8]],
      ["OIDC", [4, 5]],
      ["Runner groups", [3]],
      ["Environments", []],
      ["Org secrets", [8]],
      ["OpenTelemetry", [13, 14, 15]],
      ["Rulesets", []],
      ["Audit log", []],
    ];
    const K = "#ff7b72",
      D = "#656c76",
      T = "#f0f6fc",
      V = "#a5d6ff",
      E = "#d2a8ff";
    const vRun = h("span", { class: "ae-val" });
    const LINES = [
      [["---", D]],
      [
        ["on: ", K],
        ["issues", V],
      ],
      [["runs-on: ", K], vRun],
      [["permissions:", K]],
      [
        ["  id-token: ", K],
        ["write", V],
      ],
      [["github-app:", K]],
      [
        ["  client-id: ", K],
        ["${{ vars.APP_ID }}", E],
      ],
      [
        ["  private-key: ", K],
        ["${{ secrets.APP_KEY }}", E],
      ],
      [["network:", K]],
      [
        ["  allowed: ", K],
        ["[defaults, node]", V],
      ],
      [["steps:", K]],
      [
        ["  - uses: ", K],
        ["actions/setup-node@v4", V],
      ],
      [["observability:", K]],
      [["  otlp:", K]],
      [
        ["    endpoint: ", K],
        ["${{ secrets.OTLP_ENDPOINT }}", E],
      ],
      [["---", D]],
      [["# Triage new issues for the platform team", T]],
    ];
    const rows = LINES.map((parts, i) => h("div", { class: "ae-row" + (i === 2 ? " hot" : "") }, h("span", { class: "no" }, String(i + 1)), h("span", {}, parts.map(p => (p instanceof Node ? p : h("span", { style: { color: p[1] } }, p[0]))))));
    const ed = h("div", { class: "er-ed" }, h("div", { class: "ae-tab" }, "platform-triage.md", h("span", {}, "SYNTHETIC")), h("div", { class: "ae-code" }, rows));
    const arrow = h("div", { class: "er-arrow" }, "gh aw compile");
    arrow.innerHTML += '<svg viewBox="0 0 150 40"><path d="M4 20 H136 M120 6 L140 20 L120 34"/></svg><small>.lock.yml</small>';
    const SB = 1,
      RN = 2,
      EC = 3;
    const POS = [0, 1, 2, 3].map(i => [1010 + (i % 2) * 420, 330 + Math.floor(i / 2) * 260]);
    const MID = i => [POS[i][0] + 195, POS[i][1] + 115];
    const steps = STEPS.map((x, j) => h("b", { "--j": j }, x));
    const card = (i, c, tag, title, code, ic, extra) => {
      const el = h("div", { class: "er-card", "--rc": c, "--i": i, style: { left: `${POS[i][0]}px`, top: `${POS[i][1]}px` } }, h("i", {}, tag), h("h3", {}, title), h("code", {}, code), h("span", { class: "er-st" }), extra);
      el.insertAdjacentHTML("afterbegin", `<svg class="ic" viewBox="0 0 64 64" aria-hidden="true">${ic}</svg>`);
      return el;
    };
    const spans = h(
      "div",
      { class: "er-spans" },
      [
        [0, 100],
        [8, 46],
        [30, 58],
        [62, 30],
      ].map(([x, w], j) => h("b", { "--x": `${x}%`, "--w": `${w}%`, "--j": j }))
    );
    const otel = card(0, OTEL.c, "Observability · OTLP", "OpenTelemetry", "observability.otlp", OTEL.ic, spans);
    otel.classList.add("otel");
    const cRun = h("span");
    const runner = card(RN, RUN[0].c, "Your network", "Self Hosted", cRun, SRV);
    const fw = card(SB, FW.c, "Egress firewall", "microVM", "network.allowed", FW.ic, h("div", { class: "er-fw" }, ["✓ defaults", "✓ node", "✕ other"].map(x => h("b", { class: x[0] === "✕" ? "no" : "" }, x))));
    fw.classList.add("fw");
    const eco = card(EC, ECO.c, "Actions · Marketplace", "Ecosystem", "uses: any action", ECO.ic, h("div", { class: "er-steps" }, steps));
    eco.classList.add("eco");
    const job = h("div", { class: "er-job" });
    const trace = h("div", { class: "er-job er-trace" });
    const chips = CHIPS.map(([x], i) => h("span", { class: "er-chip", "--i": i }, x));
    const el = h(
      "section",
      { class: "slide er" },
      h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "All the goodness of GitHub Actions"), h("h2", {}, letters("Enterprise "), letters("ready", "l-grad"))),
      h("div", { class: "er-tag" }, "It compiles to ", h("b", {}, "plain GitHub Actions"), ", so it runs wherever your Actions already run."),
      ed,
      arrow,
      otel,
      fw,
      runner,
      eco,
      job,
      trace,
      h("div", { class: "er-chips" }, h("i", {}, "Identity · governance"), chips)
    );
    const fly = (dot, [x0, y0], [x1, y1]) => {
      dot.style.transition = "none";
      Object.assign(dot.style, { left: `${x0}px`, top: `${y0}px`, opacity: "1" });
      void dot.offsetWidth;
      dot.style.transition = "left .7s cubic-bezier(.5,0,.2,1), top .7s cubic-bezier(.5,0,.2,1), opacity .3s .7s";
      Object.assign(dot.style, { left: `${x1}px`, top: `${y1}px`, opacity: "0" });
    };
    let cur = 0,
      phase = "hold",
      since = 0,
      typed = "",
      arriveAt = 0,
      otelAt = 0,
      chipAt = 0,
      sbAt = 0,
      chip = -1;
    const setRunner = (i, t) => {
      cur = i;
      el.style.setProperty("--ec", RUN[i].c);
      runner.style.setProperty("--rc", RUN[i].c);
      cRun.textContent = RUN[i].k;
      runner.classList.add("on");
      for (const c of [runner, eco, fw]) c.classList.remove("run");
      eco.classList.remove("on");
      fw.classList.remove("on");
      steps.forEach(p => p.classList.remove("up"));
      fly(job, [840, 460], MID(RN));
      arriveAt = t + 700;
      sbAt = otelAt = Infinity;
    };
    const setChip = i => {
      chip = i;
      chips.forEach((c, j) => c.classList.toggle("lit", j === i));
      const lit = CHIPS[i][1];
      rows.forEach((r, j) => r.classList.toggle("lit", lit.includes(j + 1) && j !== 2));
    };
    slides.push({
      sound: "ship",
      palette: ["#1f6feb", "#0d419d", "#9e6a03", "#0a1a33"],
      notes:
        "Enterprise ready. An agentic workflow compiles to a plain GitHub Actions lock file, so you keep all the goodness of Actions: hosted, larger and Arm runners, or self-hosted runners and runner groups inside your own network, all selected with runs-on. Wherever it runs, the agent sits behind the egress firewall, which only allows the domains listed under network, and on eligible hosted KVM runners it can run inside a Cloud Hypervisor microVM. Custom steps reuse the whole Actions ecosystem, from setup and cache actions to your own internal actions, before the agent starts. Every run exports OpenTelemetry traces over OTLP to your own observability backend. Identity stays first class: GitHub App installation tokens instead of personal tokens, OIDC (id-token: write) to federate into clouds and MCP servers without long-lived keys, org and enterprise secrets, environments with required reviewers, rulesets and the audit log. The workflow, runner labels and job animation are synthetic and illustrative; see the self-hosted runners, OpenTelemetry and enterprise configuration reference for supported deployments.",
      enter() {
        typed = RUN[0].k;
        vRun.textContent = typed;
        phase = "hold";
        since = chipAt = 0;
        chip = -1;
        chips.forEach(c => c.classList.remove("lit"));
        rows.forEach(r => r.classList.remove("lit"));
        otel.classList.remove("on", "run");
        for (const c of [runner, eco, fw]) c.classList.remove("on", "run");
        steps.forEach(p => p.classList.remove("up"));
        cRun.textContent = RUN[0].k;
        sbAt = Infinity;
        for (const d of [job, trace]) {
          d.style.transition = "none";
          d.style.opacity = "0";
        }
        cur = 0;
        arriveAt = otelAt = Infinity;
      },
      tick(t) {
        if (!since) {
          since = t;
          chipAt = t + 1800;
          setRunner(0, t + 900);
        }
        if (t > arriveAt && !runner.classList.contains("run")) {
          runner.classList.add("run");
          sbAt = t + 900;
        }
        if (t > sbAt) {
          sbAt = Infinity;
          fw.classList.remove("run");
          void fw.offsetWidth;
          fw.classList.add("on", "run");
          fly(trace, MID(SB), MID(0));
          otelAt = t + 700;
        }
        if (t > otelAt) {
          otelAt = Infinity;
          otel.classList.remove("run");
          void otel.offsetWidth;
          otel.classList.add("on", "run");
        }
        if (t > arriveAt) {
          const n = Math.min(steps.length, Math.floor((t - arriveAt) / 260) + 1);
          steps.forEach((p, j) => p.classList.toggle("up", j < n));
          eco.classList.add("on");
          eco.classList.toggle("run", n === steps.length);
        }
        if (t > chipAt) {
          chipAt = t + 1500;
          setChip((chip + 1) % CHIPS.length);
        }
        const el2 = t - since;
        const next = RUN[(cur + 1) % RUN.length].k;
        if (phase === "hold" && el2 > 3400) {
          phase = "del";
          since = t;
        } else if (phase === "del" && el2 > 28) {
          since = t;
          typed = typed.slice(0, -1);
          vRun.textContent = typed;
          if (!typed) phase = "type";
        } else if (phase === "type" && el2 > 70) {
          since = t;
          typed = next.slice(0, typed.length + 1);
          vRun.textContent = typed;
          sfx.key(0.7);
          if (typed === next) {
            setRunner((cur + 1) % RUN.length, t);
            sfx.pop(cur * 2);
            phase = "hold";
          }
        }
      },
      el,
    });
  }

  // Illustrative centralized campaigns
  {
    resetIndex();
    const steps = [
      ["01", "Observe", "Runs, costs and outcomes from every repo in one dashboard", "#4493f8"],
      ["02", "Govern", "Reviewed policy in a control repository: modes, caps, safe outputs", "#ab7df8"],
      ["03", "Orchestrate", "Discover, rank and cap targets, then dispatch workers", "#d29922"],
      ["04", "Work", "One target per worker, bounded mission, correlated results", "#3fb950"],
      ["05", "Evolve", "Outcomes feed back; agents improve each cycle", "#f778ba"],
    ];
    const cv = h("canvas", { class: "cao-canvas", width: 2280, height: 1280 });
    const ctx = cv.getContext("2d");
    const stages = ["review", "limited live", "scheduled live"].map(s => h("em", {}, s));
    const CW = 1140,
      CH = 640,
      hub = { x: 170, y: CH / 2 };
    const repos = [];
    for (let c = 0; c < 6; c++) for (let r = 0; r < 7; r++) repos.push({ x: 470 + c * 110 + (r % 2) * 55, y: 70 + r * 84, state: 0, t: 0 });
    let packets = [],
      lastSpawn = 0,
      t0 = 0,
      stage = 0,
      gen = 1,
      evolvedAt = -1e9,
      ok = 0,
      tried = 0,
      spin = 0;
    const failP = () => Math.max(0.04, 0.5 - (gen - 1) * 0.16);
    const draw = t => {
      ctx.setTransform(2, 0, 0, 2, 0, 0);
      ctx.clearRect(0, 0, CW, CH);
      for (const p of repos) {
        ctx.strokeStyle = "#ffffff10";
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(hub.x, hub.y);
        ctx.lineTo(p.x, p.y);
        ctx.stroke();
      }
      for (const p of repos) {
        const col = p.state === 3 ? "#f85149" : p.state === 2 ? "#3fb950" : p.state === 1 ? "#d29922" : "#3d444d";
        ctx.fillStyle = col;
        ctx.shadowColor = col;
        ctx.shadowBlur = p.state ? 18 : 0;
        ctx.beginPath();
        ctx.roundRect(p.x - 22, p.y - 16, 44, 32, 7);
        ctx.fill();
        ctx.shadowBlur = 0;
        ctx.fillStyle = "#0d1117";
        ctx.fillRect(p.x - 12, p.y - 6, 24, 3);
        ctx.fillRect(p.x - 12, p.y + 2, 16, 3);
      }
      for (const k of packets) {
        const e = k.u < 0.5 ? 2 * k.u * k.u : 1 - (-2 * k.u + 2) ** 2 / 2;
        const [a, b] = k.back ? [k.to, hub] : [hub, k.to];
        const x = a.x + (b.x - a.x) * e,
          y = a.y + (b.y - a.y) * e;
        ctx.fillStyle = k.back ? (k.fail ? "#f85149" : "#3fb950") : "#ab7df8";
        ctx.shadowColor = ctx.fillStyle;
        ctx.shadowBlur = 16;
        ctx.beginPath();
        ctx.arc(x, y, 6, 0, 7);
        ctx.fill();
        ctx.shadowBlur = 0;
      }
      const pulse = 1 + 0.06 * Math.sin(t / 300);
      const g = ctx.createRadialGradient(hub.x, hub.y, 10, hub.x, hub.y, 130 * pulse);
      g.addColorStop(0, "#ab7df860");
      g.addColorStop(1, "#ab7df800");
      ctx.fillStyle = g;
      ctx.beginPath();
      ctx.arc(hub.x, hub.y, 130 * pulse, 0, 7);
      ctx.fill();
      // feedback loop: three arrows orbit the control repo, faster right after an evolution
      const flash = Math.max(0, 1 - (t - evolvedAt) / 1400);
      ctx.lineWidth = 5;
      ctx.lineCap = "round";
      for (let i = 0; i < 3; i++) {
        const a0 = spin + i * 2.094,
          a1 = a0 + 1.5,
          rr = 104;
        ctx.strokeStyle = "#f778ba";
        ctx.shadowColor = "#f778ba";
        ctx.shadowBlur = 10 + 30 * flash;
        ctx.beginPath();
        ctx.arc(hub.x, hub.y, rr, a0, a1);
        ctx.stroke();
        const hx = hub.x + rr * Math.cos(a1),
          hy = hub.y + rr * Math.sin(a1),
          d = a1 + Math.PI / 2;
        ctx.fillStyle = "#f778ba";
        ctx.beginPath();
        ctx.moveTo(hx + 13 * Math.cos(d), hy + 13 * Math.sin(d));
        ctx.lineTo(hx + 9 * Math.cos(d + 2.4), hy + 9 * Math.sin(d + 2.4));
        ctx.lineTo(hx + 9 * Math.cos(d - 2.4), hy + 9 * Math.sin(d - 2.4));
        ctx.fill();
      }
      ctx.shadowBlur = 0;
      ctx.fillStyle = "#161b22";
      ctx.strokeStyle = "#ab7df8";
      ctx.lineWidth = 3;
      ctx.beginPath();
      ctx.arc(hub.x, hub.y, 74 * (1 + 0.12 * flash), 0, 7);
      ctx.fill();
      ctx.stroke();
      ctx.fillStyle = "#f0f6fc";
      ctx.textAlign = "center";
      ctx.font = "800 22px 'Mona Sans'";
      ctx.fillText("control", hub.x, hub.y - 14);
      ctx.fillText("repo", hub.x, hub.y + 12);
      ctx.fillStyle = "#f778ba";
      ctx.font = "700 17px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      ctx.fillText(`agents v${gen}`, hub.x, hub.y + 38);
      ctx.fillStyle = "#9198a1";
      ctx.font = "600 15px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      ctx.fillText("FEEDBACK LOOP", hub.x, hub.y - 132);
      const rate = tried ? Math.round((100 * ok) / tried) : 0;
      ctx.fillText("SUCCESS", hub.x, hub.y + 150);
      ctx.fillStyle = rate > 85 ? "#3fb950" : rate > 65 ? "#d29922" : "#f85149";
      ctx.font = "800 34px 'Mona Sans'";
      ctx.fillText(tried ? `${rate}%` : "—", hub.x, hub.y + 186);
      if (flash > 0) {
        ctx.globalAlpha = flash;
        ctx.fillStyle = "#f778ba";
        ctx.font = "800 20px 'Mona Sans'";
        ctx.fillText("evolved ↑", hub.x, hub.y - 160 - 20 * (1 - flash));
        ctx.globalAlpha = 1;
      }
      ctx.fillStyle = "#9198a1";
      ctx.font = "600 15px 'Monaspace Neon', ui-monospace, Menlo, monospace";
      ctx.fillText("WORKERS · ONE TARGET EACH", 470 + 2.5 * 110 + 27, CH - 6);
    };
    slides.push({
      sound: "radar",
      palette: ["#8957e5", "#1f6feb", "#238636", "#9e6a03"],
      notes:
        "An illustrative multi-repository feedback loop, not an internal system or roadmap. A control repository coordinates bounded workers, reviews outcomes and updates shared workflows. The repositories, success rates and rollout stages in this animation are simulated, not operational data.",
      enter() {
        repos.forEach(p => (p.state = 0));
        packets = [];
        t0 = 0;
        stage = 0;
        gen = 1;
        ok = tried = 0;
        evolvedAt = -1e9;
        stages.forEach((s, i) => s.classList.toggle("on", i === 0));
      },
      tick(t) {
        if (!t0) t0 = lastSpawn = t;
        const dt = Math.min(50, t - (this._last || t));
        this._last = t;
        spin += dt * (0.0016 + 0.01 * Math.max(0, 1 - (t - evolvedAt) / 1400));
        if (t - lastSpawn > 120) {
          lastSpawn = t;
          const idle = repos.filter(p => p.state === 0);
          if (idle.length) {
            const to = idle[(Math.random() * idle.length) | 0];
            to.state = 1;
            packets.push({ to, u: 0, back: false });
          } else if (!packets.length) {
            gen++;
            ok = tried = 0;
            evolvedAt = t;
            repos.forEach(p => (p.state = 0));
          }
        }
        for (const k of packets) {
          k.u += dt / 800;
          if (k.u >= 1 && !k.back) {
            k.back = true;
            k.u = 0;
            k.fail = Math.random() < failP();
            k.to.state = k.fail ? 3 : 2;
          } else if (k.u >= 1 && k.back) {
            tried++;
            if (!k.fail) ok++;
          }
        }
        packets = packets.filter(k => k.u < 1);
        const s = Math.min(2, gen - 1);
        if (s !== stage) {
          stage = s;
          stages.forEach((e, i) => e.classList.toggle("on", i === s));
        }
        draw(t);
      },
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Illustrative multi-repository pattern"), h("h2", {}, letters("Centralized "), letters("campaigns", "l-grad"))),
        h(
          "div",
          { class: "cao-steps" },
          steps.map(([n, b, s, c], i) => h("div", { class: "cao-step", "--i": i, "--c": c }, h("i", {}, n), h("b", {}, b), h("span", {}, s)))
        ),
        cv,
        h("div", { class: "rollout" }, stages[0], h("s", {}, "→"), stages[1], h("s", {}, "→"), stages[2])
      ),
    });
  }

  // Finale: one control repo dispatching work to hundreds of repos, each running a few agentic processes
  {
    resetIndex();
    const cv = h("canvas", { class: "finale-canvas", width: 1920, height: 1080 });
    const ctx = cv.getContext("2d");
    const nf = new Intl.NumberFormat("en-US");
    const cRuns = h("b", {}, "0");
    const SX = 960,
      SY = 540,
      NR = 420,
      GA = Math.PI * (3 - Math.sqrt(5));
    const PROC = ["#3fb950", "#4493f8", "#ab7df8", "#d29922", "#db61a2", "#79c0ff"];
    const repos = [],
      links = [];
    for (let i = 0; i < NR; i++) {
      let near = null;
      const f = Math.sqrt((i + 6) / (NR + 6)) * (0.9 + Math.random() * 0.2),
        a = i * GA + (Math.random() - 0.5) * 0.5;
      let x = SX + Math.cos(a) * f * 1010 + (Math.random() - 0.5) * 70,
        y = SY + Math.sin(a) * f * 600 + (Math.random() - 0.5) * 50;
      if (repos.length > 8 && Math.random() < 0.22) {
        // some repos huddle near a neighbor, like an org
        const o = repos[(Math.random() * repos.length) | 0],
          ang = Math.random() * 7,
          rr = 14 + Math.random() * 22;
        x = o.x + Math.cos(ang) * rr;
        y = o.y + Math.sin(ang) * rr;
        near = o;
      }
      if (x < 24 || x > 1896 || y < 24 || y > 1056) continue;
      const k = 2 + ((Math.random() * 3) | 0),
        sz = 4 + Math.random() ** 1.6 * 5;
      repos.push({
        x,
        y,
        sz,
        d: 0,
        glow: 0,
        busy: 0,
        back: 0,
        procs: Array.from({ length: k }, (_, j) => ({ c: PROC[(Math.random() * PROC.length) | 0], a: (j / k) * Math.PI * 2, r: sz + 4 + j * 2.4, s: (0.6 + Math.random() * 0.8) * (Math.random() < 0.5 ? -1 : 1) })),
      });
      if (near) links.push([near, repos[repos.length - 1]]);
    }
    let pk = [],
      peer = [],
      peerT = 0,
      t0 = 0,
      last = 0,
      spawn = 0,
      runs = 0,
      hubFlash = 0,
      CX = SX,
      CY = SY,
      placed = false;
    // The control repo is the dot of the (dotless) "ı" in "GitHub": measure where the glyph landed.
    const iDot = h("span", {}, "ı");
    const mctx = document.createElement("canvas").getContext("2d");
    const place = () => {
      if (!iDot.offsetWidth) return;
      const cs = getComputedStyle(iDot),
        fs = parseFloat(cs.fontSize);
      mctx.font = `800 ${fs}px "Mona Sans"`;
      if ("fontStretch" in mctx) mctx.fontStretch = "semi-expanded";
      const m = mctx.measureText("ı");
      // ink of the dotless i stem, relative to the span's origin and baseline
      const left = iDot.offsetLeft - m.actualBoundingBoxLeft,
        right = iDot.offsetLeft + m.actualBoundingBoxRight;
      const base = iDot.offsetTop + (iDot.offsetHeight + m.fontBoundingBoxAscent - m.fontBoundingBoxDescent) / 2;
      const stemTop = base - m.actualBoundingBoxAscent;
      CX = (left + right) / 2;
      CY = stemTop - fs * 0.045 - 24;
      let mx = 0;
      for (const r of repos) {
        r.d = Math.hypot(r.x - CX, (r.y - CY) * 1.6);
        mx = Math.max(mx, r.d);
      }
      for (const r of repos) r.d /= mx;
      placed = true;
    };
    document.fonts?.ready.then(() => {
      placed = false;
    });
    const curve = (ax, ay, bx, by, f, bend) => {
      const mx = (ax + bx) / 2 - (by - ay) * bend,
        my = (ay + by) / 2 + (bx - ax) * bend,
        u = 1 - f;
      return [u * u * ax + 2 * u * f * mx + f * f * bx, u * u * ay + 2 * u * f * my + f * f * by];
    };
    slides.push({
      sound: "finale",
      palette: ["#0fbf3e", "#1f6feb", "#8957e5", "#238636"],
      notes:
        "An illustrative pattern: a control repository coordinates small agentic processes across repositories and receives proposed results as issues and pull requests. All repository and run counts are simulated, not production metrics or a product roadmap. Processes, not prompts: engineered, guarded and measured. That's GitHub Agentic Workflows.",
      enter() {
        t0 = last = spawn = peerT = 0;
        pk = [];
        peer = [];
        runs = 0;
        placed = false;
        repos.forEach(r => {
          r.glow = r.busy = r.back = 0;
        });
        cRuns.textContent = "0";
      },
      tick(t) {
        if (!placed) {
          place();
          if (!placed) return;
        }
        if (!t0) {
          t0 = last = t;
        }
        const e = (t - t0) / 1000,
          dt = Math.min(0.05, (t - last) / 1000);
        last = t;
        const reveal = Math.min(1, e / 2.6);
        if (e > 1.2 && t - spawn > 28) {
          spawn = t;
          const r = repos[(Math.random() * repos.length) | 0];
          if (r.d < reveal * 1.15) pk.push({ r, k: 0, out: true, sp: 0.55 + Math.random() * 0.35, bend: (Math.random() - 0.5) * 0.5, c: PROC[(Math.random() * PROC.length) | 0] });
          hubFlash = 1;
        }
        ctx.clearRect(0, 0, 1920, 1080);
        hubFlash = Math.max(0, hubFlash - dt * 2);
        // faint spokes from control to busy repos
        ctx.lineWidth = 1;
        for (const r of repos) {
          r.glow = Math.max(0, r.glow - dt * 0.8);
          if (r.busy > 0) {
            r.busy -= dt;
            if (r.busy <= 0 && r.back) {
              pk.push({ r, k: 0, out: false, sp: 0.7, bend: -0.18, c: "#3fb950" });
              r.back = 0;
            }
          }
          if (r.glow > 0.05) {
            ctx.globalAlpha = r.glow * 0.12;
            ctx.strokeStyle = "#5fed83";
            ctx.beginPath();
            ctx.moveTo(CX, CY);
            ctx.lineTo(r.x, r.y);
            ctx.stroke();
          }
        }
        // clustered repos run agentic workflows between each other, no control repo needed
        if (e > 2 && t - peerT > 90 && links.length) {
          peerT = t;
          const [a, b] = links[(Math.random() * links.length) | 0];
          peer.push(Math.random() < 0.5 ? { a, b, k: 0, c: PROC[(Math.random() * PROC.length) | 0] } : { a: b, b: a, k: 0, c: PROC[(Math.random() * PROC.length) | 0] });
        }
        ctx.lineWidth = 1;
        ctx.strokeStyle = "#8b949e";
        for (const [a, b] of links) {
          const v = Math.min(1, Math.max(0, (reveal * 1.15 - Math.max(a.d, b.d)) * 6));
          if (v <= 0) continue;
          ctx.globalAlpha = v * (0.16 + Math.max(a.glow, b.glow) * 0.3);
          ctx.beginPath();
          ctx.moveTo(a.x, a.y);
          ctx.lineTo(b.x, b.y);
          ctx.stroke();
        }
        peer = peer.filter(q => {
          q.k += dt * 1.3;
          if (q.k >= 1) {
            q.b.glow = Math.max(q.b.glow, 0.6);
            return false;
          }
          const f = q.k * q.k * (3 - 2 * q.k),
            x = q.a.x + (q.b.x - q.a.x) * f,
            y = q.a.y + (q.b.y - q.a.y) * f;
          ctx.globalAlpha = 0.95;
          ctx.fillStyle = q.c;
          ctx.beginPath();
          ctx.arc(x, y, 1.8, 0, 7);
          ctx.fill();
          ctx.globalAlpha = 0.25;
          ctx.beginPath();
          ctx.arc(x, y, 4.5, 0, 7);
          ctx.fill();
          return true;
        });
        // repos and their orbiting processes
        for (const r of repos) {
          const v = Math.max(0, Math.min(1, (reveal * 1.15 - r.d) * 6));
          if (v <= 0) continue;
          const boost = 1 + r.glow * 5;
          for (const p of r.procs) {
            p.a += dt * p.s * boost;
            const px = r.x + Math.cos(p.a) * p.r * v,
              py = r.y + Math.sin(p.a) * p.r * v;
            ctx.globalAlpha = v * (0.45 + r.glow * 0.55);
            ctx.fillStyle = p.c;
            ctx.beginPath();
            ctx.arc(px, py, 1.6 + r.glow, 0, 7);
            ctx.fill();
          }
          ctx.globalAlpha = v * (0.55 + r.glow * 0.45);
          ctx.fillStyle = r.glow > 0.1 ? "#f0f6fc" : "#8b949e";
          if (r.glow > 0.1) {
            ctx.shadowColor = "#5fed83";
            ctx.shadowBlur = 14 * r.glow;
          }
          ctx.fillRect(r.x - r.sz / 2, r.y - r.sz / 2, r.sz, r.sz);
          ctx.shadowBlur = 0;
        }
        // work packets: out from control (colored by process), results back (green)
        pk = pk.filter(q => {
          q.k += dt * q.sp;
          if (q.k >= 1) {
            if (q.out) {
              q.r.glow = 1;
              q.r.busy = 0.8 + Math.random() * 1.4;
              q.r.back = 1;
            } else {
              runs++;
              hubFlash = 1;
            }
            return false;
          }
          const [ax, ay, bx, by] = q.out ? [CX, CY, q.r.x, q.r.y] : [q.r.x, q.r.y, CX, CY];
          const f = q.k * q.k * (3 - 2 * q.k),
            [x, y] = curve(ax, ay, bx, by, f, q.bend);
          const [x0, y0] = curve(ax, ay, bx, by, Math.max(0, f - 0.06), q.bend);
          ctx.globalAlpha = 0.55;
          ctx.strokeStyle = q.c;
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          ctx.moveTo(x0, y0);
          ctx.lineTo(x, y);
          ctx.stroke();
          ctx.globalAlpha = 1;
          ctx.fillStyle = q.c;
          ctx.beginPath();
          ctx.arc(x, y, 2.4, 0, 7);
          ctx.fill();
          return true;
        });
        // the central control repo
        const pulse = 0.5 + 0.5 * Math.sin(e * 2.2);
        for (let k = 0; k < 3; k++) {
          const ph = (e * 0.5 + k / 3) % 1;
          ctx.globalAlpha = (1 - ph) * 0.35 * reveal;
          ctx.strokeStyle = "#5fed83";
          ctx.lineWidth = 2;
          ctx.beginPath();
          ctx.arc(CX, CY, 30 + ph * 220, 0, 7);
          ctx.stroke();
        }
        ctx.globalAlpha = reveal;
        ctx.shadowColor = "#0fbf3e";
        ctx.shadowBlur = 40 + 30 * Math.max(pulse * 0.5, hubFlash);
        ctx.fillStyle = "#0fbf3e";
        ctx.beginPath();
        ctx.arc(CX, CY, 24 + hubFlash * 3, 0, 7);
        ctx.fill();
        ctx.shadowBlur = 0;
        ctx.fillStyle = "#0d1117";
        ctx.fillRect(CX - 8, CY - 9, 16, 18);
        ctx.fillStyle = "#5fed83";
        ctx.fillRect(CX - 5, CY - 6, 10, 2);
        ctx.fillRect(CX - 5, CY - 1, 10, 2);
        ctx.fillRect(CX - 5, CY + 4, 6, 2);
        ctx.globalAlpha = 1;
        cRuns.textContent = nf.format(runs);
      },
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "fin-bg" }, h("h1", {}, "G", iDot, "tHub"), h("h1", {}, "Agentic Workflows")),
        cv,
        h("div", { class: "fin-cap" }, "Illustrative  ·  ", h("b", {}, "1"), " control repo  ·  ", h("b", {}, `${repos.length}`), " repos  ·  ", cRuns, " runs")
      ),
    });
  }

  const stage = h(
    "div",
    { class: "stage" },
    slides.map(s => s.el)
  );
  const progress = h("div", { class: "progress" });
  const dots = h(
    "div",
    { class: "dots" },
    slides.map((_, i) =>
      h("button", {
        "aria-label": `Slide ${i + 1}`,
        onclick: e => {
          e.stopPropagation();
          go(i);
        },
      })
    )
  );
  const arrowBtn = (label, d, fn) => {
    const b = h("button", {
      "aria-label": label,
      title: label,
      onclick: e => {
        e.stopPropagation();
        fn();
      },
    });
    b.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="${d}"/></svg>`;
    return b;
  };
  const prevBtn = arrowBtn("Previous slide", "M15 5l-7 7 7 7", () => prev());
  const nextBtn = arrowBtn("Next slide", "M9 5l7 7-7 7", () => next());
  const arrows = h("div", { class: "arrows" }, prevBtn, nextBtn);
  const count = h("div", { class: "count" });
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
  const notes = h("div", { class: "notes" });
  const wash = h("div", { class: "wash" });
  const gwash = h("div", { class: "wash green" });
  const gridc = h("canvas", { class: "gridc" });
  document.body.append(canvas, gwash, wash, gridc, stage, progress, dots, arrows, count, snd, notes);

  // Dot row: left offset + 12px dots + 12px gaps + 28px for the wide active dot; sound/count need ~170px on the right.
  const dotsWidth = 28 + slides.length * 24 - 12 + 28;
  const fit = () => {
    const s = Math.min(innerWidth / W, innerHeight / H);
    stage.style.transform = `scale(${s}) translate(-50%, -50%)`;
    document.body.classList.toggle("compact-nav", dotsWidth > innerWidth - 170);
  };
  addEventListener("resize", fit);
  fit();

  function toggleSound() {
    snd.classList.toggle("off", sfx.toggle());
    snd.classList.add("flash");
    setTimeout(() => snd.classList.remove("flash"), 400);
  }

  // ---------- sound: tiny WebAudio synth (M to mute) ----------
  const sfx = (() => {
    // `out` is the current slide's bus; it is swapped (and the old one faded) on every slide change
    // so long themes and drones scheduled for one slide never leak into the next.
    let ac = null,
      master = null,
      out = null,
      dry = null,
      muted = localStorage.getItem("aw-mute") === "1";
    const SCALE = [0, 2, 4, 7, 9, 12, 14, 16, 19, 21];
    const ROOTS = [57, 60, 62, 55, 64, 59, 61, 56, 58, 63, 53, 65];
    const hz = m => 440 * 2 ** ((m - 69) / 12);
    const init = () => {
      if (ac) {
        if (ac.state === "suspended") ac.resume();
        return ac;
      }
      const AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return null;
      ac = new AC();
      master = ac.createGain();
      master.gain.value = 0.5;
      out = ac.createGain();
      out.connect(master);
      const comp = ac.createDynamicsCompressor();
      dry = ac.createGain();
      dry.gain.value = 0.5;
      dry.connect(comp);
      const dl = ac.createDelay(1),
        fb = ac.createGain(),
        wet = ac.createGain(),
        lp = ac.createBiquadFilter();
      dl.delayTime.value = 0.27;
      fb.gain.value = 0.38;
      wet.gain.value = 0.32;
      lp.type = "lowpass";
      lp.frequency.value = 2600;
      master.connect(comp);
      master.connect(dl);
      dl.connect(lp);
      lp.connect(fb);
      fb.connect(dl);
      lp.connect(wet);
      wet.connect(comp);
      comp.connect(ac.destination);
      return ac;
    };
    const cut = () => {
      if (!ac) return;
      const old = out,
        t = ac.currentTime;
      old.gain.cancelScheduledValues(t);
      old.gain.setValueAtTime(old.gain.value, t);
      old.gain.linearRampToValueAtTime(0, t + 0.25);
      setTimeout(() => old.disconnect(), 400);
      out = ac.createGain();
      out.connect(master);
    };
    const noise = (() => {
      let b = null;
      return () => {
        if (b) return b;
        b = ac.createBuffer(1, ac.sampleRate, ac.sampleRate);
        const d = b.getChannelData(0);
        for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
        return b;
      };
    })();
    const env = (g, t, a, peak, rel) => {
      g.gain.setValueAtTime(0.0001, t);
      g.gain.exponentialRampToValueAtTime(peak, t + a);
      g.gain.exponentialRampToValueAtTime(0.0001, t + a + rel);
    };
    const tone = (m, t, { type = "triangle", dur = 0.5, vol = 0.18, cutoff = 3000, detune = 7 } = {}) => {
      const f = ac.createBiquadFilter(),
        g = ac.createGain();
      f.type = "lowpass";
      f.Q.value = 6;
      f.frequency.setValueAtTime(cutoff, t);
      f.frequency.exponentialRampToValueAtTime(400, t + dur);
      [-detune, detune].forEach(dt => {
        const o = ac.createOscillator();
        o.type = type;
        o.frequency.value = hz(m);
        o.detune.value = dt;
        o.connect(f);
        o.start(t);
        o.stop(t + dur + 0.05);
      });
      f.connect(g);
      g.connect(out);
      env(g, t, 0.008, vol, dur);
    };
    const whoosh = (t, dir) => {
      const src = ac.createBufferSource(),
        f = ac.createBiquadFilter(),
        g = ac.createGain(),
        pan = ac.createStereoPanner?.();
      src.buffer = noise();
      f.type = "bandpass";
      f.Q.value = 1.4;
      const [a, b] = dir > 0 ? [300, 5200] : [5200, 300];
      f.frequency.setValueAtTime(a, t);
      f.frequency.exponentialRampToValueAtTime(b, t + 0.42);
      src.connect(f);
      f.connect(g);
      if (pan) {
        pan.pan.setValueAtTime(-0.8 * dir, t);
        pan.pan.linearRampToValueAtTime(0.8 * dir, t + 0.45);
        g.connect(pan);
        pan.connect(out);
      } else g.connect(out);
      g.gain.setValueAtTime(0.0001, t);
      g.gain.exponentialRampToValueAtTime(0.18, t + 0.18);
      g.gain.exponentialRampToValueAtTime(0.0001, t + 0.5);
      src.start(t, Math.random() * 0.4);
      src.stop(t + 0.55);
      // sub drop for weight
      const o = ac.createOscillator(),
        og = ac.createGain();
      o.type = "sine";
      o.frequency.setValueAtTime(dir > 0 ? 90 : 60, t + 0.12);
      o.frequency.exponentialRampToValueAtTime(dir > 0 ? 42 : 80, t + 0.5);
      o.connect(og);
      og.connect(out);
      env(og, t + 0.12, 0.02, 0.35, 0.4);
      o.start(t + 0.1);
      o.stop(t + 0.6);
    };
    let song = -1;
    const SONG = [
      { m: [73, 75, 77, 80, 77, 75, 73, 0], b: 49 },
      { m: [70, 73, 75, 73, 70, 68, 70, 0], b: 46 },
      { m: [73, 77, 80, 82, 80, 77, 75, 77], b: 54 },
      { m: [80, 0, 78, 77, 75, 73, 75, 0], b: 51 },
      { m: [68, 70, 73, 70, 75, 73, 77, 75], b: 49 },
      { m: [80, 82, 80, 77, 73, 75, 77, 0], b: 46 },
      { m: [82, 80, 77, 80, 75, 77, 73, 75], b: 54 },
      { m: [73, 0, 80, 0, 85, 0, 0, 0], b: 56 },
    ];
    const hat = t => {
      const src = ac.createBufferSource(),
        f = ac.createBiquadFilter(),
        g = ac.createGain();
      src.buffer = noise();
      f.type = "highpass";
      f.frequency.value = 7000;
      src.connect(f);
      f.connect(g);
      g.connect(out);
      env(g, t, 0.002, 0.08, 0.05);
      src.start(t, Math.random() * 0.5);
      src.stop(t + 0.08);
    };
    const osc = (type, f0, t, dur, vol, { f1 = f0, dest = out, a = 0.005 } = {}) => {
      const o = ac.createOscillator(),
        g = ac.createGain();
      o.type = type;
      o.frequency.setValueAtTime(f0, t);
      if (f1 !== f0) o.frequency.exponentialRampToValueAtTime(f1, t + dur);
      o.connect(g);
      g.connect(dest);
      env(g, t, a, vol, dur);
      o.start(t);
      o.stop(t + a + dur + 0.05);
    };
    const bell = (m, t, vol = 0.08, dur = 1.6) =>
      [
        [1, 1],
        [2.76, 0.4],
        [5.4, 0.18],
        [8.93, 0.08],
      ].forEach(([r, v]) => osc("sine", hz(m) * r, t, dur / r ** 0.4, vol * v));
    const click = (t, f = 3000, vol = 0.12, q = 4) => {
      const src = ac.createBufferSource(),
        bp = ac.createBiquadFilter(),
        g = ac.createGain();
      src.buffer = noise();
      bp.type = "bandpass";
      bp.frequency.value = f;
      bp.Q.value = q;
      src.connect(bp);
      bp.connect(g);
      g.connect(out);
      env(g, t, 0.001, vol, 0.03);
      src.start(t, Math.random() * 0.5);
      src.stop(t + 0.05);
    };
    const pad = (ms, t, dur, vol = 0.05, type = "sawtooth", cut = 1800) =>
      ms.forEach(m => {
        const f = ac.createBiquadFilter(),
          g = ac.createGain();
        f.type = "lowpass";
        f.frequency.setValueAtTime(300, t);
        f.frequency.linearRampToValueAtTime(cut, t + dur * 0.5);
        f.frequency.linearRampToValueAtTime(400, t + dur);
        [-9, 9].forEach(d => {
          const o = ac.createOscillator();
          o.type = type;
          o.frequency.value = hz(m);
          o.detune.value = d;
          o.connect(f);
          o.start(t);
          o.stop(t + dur + 0.1);
        });
        f.connect(g);
        g.connect(out);
        g.gain.setValueAtTime(0.0001, t);
        g.gain.exponentialRampToValueAtTime(vol, t + dur * 0.4);
        g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
      });
    const THEMES = {
      // title: wide major swell with a rising shimmer
      title: t => {
        pad([48, 55, 64, 71], t + 0.1, 3.2, 0.045);
        [72, 76, 79, 83, 86, 91].forEach((m, i) => bell(m, t + 0.5 + i * 0.11, 0.05, 1.4));
      },
      ship: t => {
        [
          [60, 0],
          [64, 0.12],
          [67, 0.24],
          [72, 0.4],
        ].forEach(([m, d]) => tone(m, t + 0.2 + d, { type: "sawtooth", dur: d > 0.3 ? 0.9 : 0.2, vol: 0.08, cutoff: 3600 }));
        tone(48, t + 0.6, { type: "triangle", dur: 0.9, vol: 0.18, cutoff: 900 });
        bell(84, t + 0.75, 0.07);
        bell(91, t + 0.9, 0.05);
      },
      // prompt: keyboard typing then a compile whir and success chime
      typing: () => {}, // live keystrokes + compile chime are played by the slide itself
      // compiler hardening: anvil strikes ringing out, then a low shield hum
      forge: t => {
        [0, 0.34, 0.68].forEach((d, i) => {
          click(t + 0.2 + d, 2400 + i * 400, 0.1, 6);
          [1, 2.76, 5.4].forEach(r => osc("sine", 523 * r, t + 0.2 + d, 0.6 - i * 0.1, 0.03 / r));
        });
        pad([40, 47, 52], t + 1.1, 2.4, 0.03, "triangle", 900);
      },
      // factory mindset: rhythmic metallic machinery
      factory: t => {
        for (let i = 0; i < 8; i++) {
          const tt = t + 0.2 + i * 0.16;
          if (i % 2 === 0) osc("sine", 120, tt, 0.18, 0.3, { f1: 45 });
          click(tt + 0.08, 6000, 0.08, 1);
          if (i % 4 === 2) [1, 1.48, 2.13].forEach(r => osc("square", 310 * r, tt, 0.12, 0.025));
        }
      },
      // human agent processes: looping sequencer motif, human heartbeat underneath
      loop: t => {
        const s = 0.14;
        [64, 67, 71, 67, 64, 67, 71, 74].forEach((m, i) => tone(m, t + 0.2 + i * s, { type: "triangle", dur: 0.22, vol: 0.09, cutoff: 2600 }));
        [0, 0.22, 0.56, 0.78].forEach(d => osc("sine", 70, t + 0.2 + d, 0.18, 0.35, { f1: 40 }));
      },
      // A soft bed of worker ticks around a gently breathing central hum
      radar: t => {
        const D = 7;
        const eg = ac.createGain(),
          lp = ac.createBiquadFilter(),
          lfo = ac.createOscillator(),
          lg = ac.createGain();
        lp.type = "lowpass";
        lp.frequency.value = 1400;
        eg.gain.setValueAtTime(0.0001, t);
        eg.gain.exponentialRampToValueAtTime(0.03, t + 1.5);
        eg.gain.setValueAtTime(0.03, t + D - 2);
        eg.gain.exponentialRampToValueAtTime(0.0001, t + D);
        lfo.frequency.value = 0.9;
        lg.gain.value = 0.012;
        lfo.connect(lg);
        lg.connect(eg.gain);
        lp.connect(eg);
        eg.connect(out);
        [
          [60, -4],
          [64, 4],
          [67, 0],
          [72, 3],
        ].forEach(([m, dt]) => {
          const o = ac.createOscillator();
          o.type = m === 60 ? "triangle" : "sine";
          o.frequency.value = hz(m);
          o.detune.value = dt;
          o.connect(lp);
          o.start(t);
          o.stop(t + D + 0.1);
        });
        lfo.start(t);
        lfo.stop(t + D + 0.1);
        const PENTA = [0, 2, 4, 7, 9];
        for (let i = 0; i < 70; i++) {
          const d = 0.4 + Math.random() * (D - 1.6),
            m = 79 + PENTA[(Math.random() * 5) | 0] + 12 * ((Math.random() * 2) | 0),
            g = ac.createGain(),
            pn = ac.createStereoPanner?.(),
            o = ac.createOscillator();
          o.type = "sine";
          o.frequency.setValueAtTime(hz(m) * 1.04, t + d);
          o.frequency.exponentialRampToValueAtTime(hz(m), t + d + 0.03);
          o.connect(g);
          if (pn) {
            pn.pan.value = Math.random() * 1.6 - 0.8;
            g.connect(pn);
            pn.connect(out);
          } else g.connect(out);
          env(g, t + d, 0.004, 0.008 + Math.random() * 0.014, 0.09 + Math.random() * 0.08);
          o.start(t + d);
          o.stop(t + d + 0.25);
        }
      },
      // cost management: coins dropping into a jar, then a gentle register "ding"
      coins: t => {
        for (let i = 0; i < 9; i++) {
          const d = 0.2 + i * 0.09 + Math.random() * 0.03;
          bell(84 + SCALE[(9 - i) % 6], t + d, 0.035, 0.5);
          click(t + d, 5200, 0.05, 6);
        }
        bell(91, t + 1.2, 0.06, 1.6);
        bell(96, t + 1.32, 0.04, 1.4);
      },
      // any engine: an ignition rev climbing into a bright two-note chime
      engine: t => {
        [43, 48, 55, 60].forEach((m, i) => tone(m, t + 0.1 + i * 0.14, { type: "sawtooth", dur: 0.22, vol: 0.07, cutoff: 1200 + i * 700 }));
        tone(36, t + 0.1, { type: "sine", dur: 0.9, vol: 0.18, cutoff: 500 });
        bell(84, t + 0.75, 0.06);
        bell(91, t + 0.9, 0.05, 1.8);
      },
      // model routing: a filtered-noise riser into a rising A major arpeggio over a soft pad
      router: t => {
        const src = ac.createBufferSource(),
          f = ac.createBiquadFilter(),
          g = ac.createGain(),
          s = 60 / 111 / 4;
        src.buffer = noise();
        f.type = "bandpass";
        f.Q.value = 2;
        f.frequency.setValueAtTime(300, t + 0.3);
        f.frequency.exponentialRampToValueAtTime(6000, t + 0.9);
        src.connect(f);
        f.connect(g);
        g.connect(out);
        g.gain.setValueAtTime(0.0001, t + 0.3);
        g.gain.exponentialRampToValueAtTime(0.08, t + 0.8);
        g.gain.exponentialRampToValueAtTime(0.0001, t + 1);
        src.start(t + 0.3, Math.random() * 0.2);
        src.stop(t + 1.05);
        [57, 61, 64, 69, 73].forEach((m, i) => tone(m, t + 0.9 + i * s, { type: "sawtooth", dur: i === 4 ? 0.9 : 0.16, vol: 0.05, cutoff: 3600, detune: 6 }));
        bell(81, t + 0.9 + 5 * s, 0.04, 1.4);
        pad([45, 52, 57, 61], t + 0.9, 2.2, 0.022, "triangle", 1600);
      },
      // enclaves: a heavy vault bolt sliding shut, then a low sealed hum
      vault: t => {
        [0, 0.18].forEach((d, i) => {
          click(t + 0.2 + d, 900 - i * 300, 0.12, 5);
          osc("sine", 90 - i * 20, t + 0.2 + d, 0.25, 0.25, { f1: 40 });
        });
        pad([38, 45, 50], t + 0.6, 2.6, 0.03, "triangle", 700);
        bell(86, t + 0.7, 0.03, 1.6);
      },
      // finale: a warm major swell with a cascade of bells radiating out
      finale: t => {
        pad([48, 55, 60, 64, 67], t + 0.1, 4.2, 0.04, "triangle", 2400);
        for (let i = 0; i < 24; i++) bell(72 + SCALE[i % 10], t + 0.4 + i * 0.07 + Math.random() * 0.03, 0.035, 1.2);
      },
    };
    const play = fn => {
      if (muted || !init()) return;
      fn(ac.currentTime + 0.01);
    };
    // Berlin techno bed for the post-preview acceleration: 4/4 kick, off-beat hats, acid line, claps, stabs.
    // k in (0..1] sets both tempo (128 → 146 BPM) and how many layers are in.
    const TZ = { k: 0, timer: null, next: 0, i: 0, bus: null, send: null };
    const ACID = [36, 36, 48, 36, 39, 36, 43, 46, 36, 36, 48, 51, 36, 39, 43, 36];
    const ACC = [1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 1, 0, 0, 1, 0];
    const tnoise = (t, dur, vol, type, f, q = 1, dest = TZ.bus) => {
      const src = ac.createBufferSource(),
        bf = ac.createBiquadFilter(),
        g = ac.createGain();
      src.buffer = noise();
      bf.type = type;
      bf.frequency.value = f;
      bf.Q.value = q;
      src.connect(bf);
      bf.connect(g);
      g.connect(dest);
      env(g, t, 0.001, vol, dur);
      src.start(t, Math.random() * 0.5);
      src.stop(t + dur + 0.05);
    };
    const kick = t => {
      const o = ac.createOscillator(),
        g = ac.createGain();
      o.type = "sine";
      o.frequency.setValueAtTime(160, t);
      o.frequency.exponentialRampToValueAtTime(48, t + 0.11);
      o.connect(g);
      g.connect(TZ.bus);
      env(g, t, 0.002, 0.95, 0.32);
      o.start(t);
      o.stop(t + 0.4);
      tnoise(t, 0.012, 0.25, "highpass", 3000);
    };
    const acid = (m, t, dur, cut, acc) => {
      const o = ac.createOscillator(),
        bf = ac.createBiquadFilter(),
        g = ac.createGain();
      o.type = "sawtooth";
      o.frequency.value = hz(m);
      bf.type = "lowpass";
      bf.Q.value = acc ? 18 : 11;
      bf.frequency.setValueAtTime(cut * (acc ? 1.8 : 1), t);
      bf.frequency.exponentialRampToValueAtTime(140, t + dur);
      o.connect(bf);
      bf.connect(g);
      g.connect(TZ.bus);
      env(g, t, 0.003, acc ? 0.16 : 0.1, dur);
      o.start(t);
      o.stop(t + dur + 0.05);
    };
    const stab = t =>
      [60, 63, 67, 70].forEach(m => {
        const o = ac.createOscillator(),
          bf = ac.createBiquadFilter(),
          g = ac.createGain();
        o.type = "sawtooth";
        o.frequency.value = hz(m);
        o.detune.value = (Math.random() - 0.5) * 16;
        bf.type = "lowpass";
        bf.frequency.setValueAtTime(3200, t);
        bf.frequency.exponentialRampToValueAtTime(500, t + 0.18);
        o.connect(bf);
        bf.connect(g);
        g.connect(TZ.send);
        env(g, t, 0.003, 0.035, 0.18);
        o.start(t);
        o.stop(t + 0.25);
      });
    const tsched = () => {
      while (TZ.next < ac.currentTime + 0.12) {
        const k = TZ.k,
          s16 = 60 / (128 + 18 * k) / 4,
          t = TZ.next,
          i = TZ.i % 16,
          bar = (TZ.i / 16) | 0;
        if (i % 4 === 0) kick(t);
        if (i % 4 === 2) tnoise(t, 0.11, 0.11, "highpass", 8000);
        if (k > 0.25 && (i === 4 || i === 12)) {
          [0, 0.011, 0.022].forEach(d => tnoise(t + d, d === 0.022 ? 0.14 : 0.02, 0.18, "bandpass", 1500, 1.2));
        }
        if (k > 0.45 && i % 4 !== 2) tnoise(t, 0.03, 0.035 + (i % 2 ? 0 : 0.02), "highpass", 9500);
        acid(ACID[i] + (bar % 4 === 3 ? 5 : 0), t, s16 * 0.9, 300 + 2600 * k * (0.6 + 0.4 * Math.sin(TZ.i / 24)), ACC[i]);
        if (k > 0.7 && (i === 3 || i === 10)) stab(t);
        if (k > 0.85 && i === 14 && bar % 2) tnoise(t, s16 * 2, 0.06, "bandpass", 600 + 400 * Math.random(), 3);
        TZ.next += s16;
        TZ.i++;
      }
    };
    const techno = k => {
      if (muted || (k > 0 && !init())) k = 0;
      TZ.k = Math.min(1, k);
      if (k > 0 && !TZ.timer) {
        const t = ac.currentTime;
        TZ.bus = ac.createGain();
        TZ.bus.gain.setValueAtTime(0.0001, t);
        TZ.bus.gain.exponentialRampToValueAtTime(1, t + 0.4);
        TZ.bus.connect(dry);
        TZ.send = ac.createGain();
        TZ.send.gain.value = 1;
        TZ.send.connect(TZ.bus);
        TZ.send.connect(master);
        TZ.next = t + 0.05;
        TZ.i = 0;
        TZ.timer = setInterval(tsched, 25);
        tsched();
      } else if (k <= 0 && TZ.timer) {
        clearInterval(TZ.timer);
        TZ.timer = null;
        const b = TZ.bus,
          t = ac.currentTime;
        b.gain.cancelScheduledValues(t);
        b.gain.setValueAtTime(b.gain.value, t);
        b.gain.exponentialRampToValueAtTime(0.0001, t + 0.35);
        setTimeout(() => {
          b.disconnect();
          TZ.send.disconnect();
        }, 600);
      }
    };
    return {
      unlock: init,
      cut,
      techno,
      // A soft rising blip for demo steps.
      pop: (() => {
        let lastPop = 0;
        return (n = 0) =>
          play(t => {
            if (t - lastPop < 0.07) return;
            lastPop = t;
            const f = hz(72 + SCALE[n % 8]);
            osc("sine", f * 0.7, t, 0.1, 0.035, { f1: f * 1.3 });
            bell(84 + SCALE[n % 6], t + 0.04, 0.008, 0.5);
          });
      })(),
      // feed notification: a soft two-note glass "ding" (E6 → B6), quieter than pop
      // a single soft keyboard keystroke (randomized so a run of them sounds like real typing)
      key: (() => {
        let last = 0;
        return (vol = 1) =>
          play(t => {
            if (t - last < 0.025) return;
            last = t;
            click(t, 1800 + Math.random() * 2400, 0.07 * vol, 2);
            if (Math.random() < 0.3) click(t + 0.012, 900 + Math.random() * 500, 0.03 * vol, 3);
          });
      })(),
      // Maintainer review: a soft stamp thud plus a warm major arpeggio
      review: (() => {
        let last = 0;
        return () =>
          play(t => {
            if (t - last < 0.3) return;
            last = t;
            osc("triangle", 110, t, 0.18, 0.06, { f1: 55 });
            click(t, 600, 0.05, 4);
            bell(67, t + 0.03, 0.03, 1.2);
            bell(71, t + 0.1, 0.025, 1.2);
            bell(74, t + 0.17, 0.025, 1.4);
          });
      })(),
      // compile: a short whir rising into a two-bell success chime
      compile: () =>
        play(t => {
          osc("sawtooth", 80, t, 0.4, 0.035, { f1: 640 });
          bell(79, t + 0.4, 0.05, 1);
          bell(84, t + 0.5, 0.04, 1.2);
        }),
      notify: (() => {
        let last = 0;
        return human =>
          play(t => {
            if (t - last < 0.4) return;
            last = t;
            bell(human ? 88 : 91, t, 0.012, 0.6);
            bell(human ? 95 : 98, t + 0.09, 0.01, 0.8);
            osc("sine", hz(human ? 76 : 79), t, 0.08, 0.015);
          });
      })(),
      // model routing: a tiny dot per routed call; expensive = deep bass, economy = high tick
      route: (() => {
        let last = 0;
        return m =>
          play(t => {
            if (t - last < 0.09) return;
            last = t;
            const f = hz([36, 60, 84][m] + (Math.random() < 0.5 ? 0 : 7));
            osc(m ? "sine" : "triangle", f * (m ? 1.2 : 1.6), t, m ? 0.06 : 0.16, [0.05, 0.025, 0.014][m], { f1: f });
          });
      })(),
      // model routing slide: every run is a note, and its pitch is the model it got: high glass for a small model,
      // a mallet for mid-size, bass for frontier, walking A major pentatonic. Notes snap to 16ths at 111 BPM, so the
      // 270 ms echo lands on the eighth notes.
      mr: (() => {
        const S = 60 / 111 / 4,
          WALK = [0, 4, 7, 9, 12, 9, 7, 4];
        let g0 = 0,
          n = 0,
          lastFlap = 0;
        const grid = t => (g0 ? g0 + Math.ceil((t - g0) / S) * S : (g0 = t));
        return {
          reset() {
            g0 = n = 0;
          },
          // one split-flap tick
          flap: () =>
            play(t => {
              if (t - lastFlap < 0.04) return;
              lastFlap = t;
              click(t, 2400 + Math.random() * 2000, 0.03, 5);
            }),
          pick: tier =>
            play(t => {
              const g = grid(t),
                w = WALK[n++ % WALK.length];
              if (tier === 0) {
                bell(81 + w, g, 0.035, 0.9);
                osc("sine", hz(93 + w), g + S, 0.12, 0.012);
              } else if (tier === 1) {
                tone(57 + w, g, { type: "triangle", dur: 0.35, vol: 0.1, cutoff: 2600, detune: 3 });
                tone(64 + w, g + S, { type: "triangle", dur: 0.3, vol: 0.07, cutoff: 2600, detune: 3 });
              } else {
                tone(45, g, { type: "triangle", dur: 0.6, vol: 0.16, cutoff: 700, detune: 3 });
                osc("sine", hz(33), g, 0.6, 0.12);
                bell(69 + w, g + 2 * S, 0.02, 1);
              }
            }),
          // measured savings: bells cascading down into a soft A add9 chord and a two-note receipt
          saved: () =>
            play(t => {
              [93, 90, 88, 85, 81, 78, 76, 73].forEach((m, i) => bell(m, t + 0.05 + i * 0.07, 0.028, 0.9));
              pad([45, 52, 59, 61], t + 0.55, 2.6, 0.03, "triangle", 2000);
              bell(88, t + 0.9, 0.045, 1.4);
              bell(93, t + 1.02, 0.035, 1.6);
            }),
          // a week of runs: one soft mallet note per day, climbing, and a chord when the week is full
          day: i =>
            play(t => {
              tone([69, 71, 73, 76, 78, 81, 83][i], t, { type: "triangle", dur: 0.25, vol: 0.05, cutoff: 3000, detune: 3 });
              if (i === 6) [81, 85, 88].forEach((m, j) => bell(m, t + 0.12 + j * 0.05, 0.025, 1.6));
            }),
        };
      })(),
      slide: (n, dir, theme) =>
        play(t => {
          if (theme === "omen") return; // security: only the dark drone (played on enter)
          whoosh(t, dir);
          if (THEMES[theme]) return THEMES[theme](t);
          // original 8-bit loop: each transition plays the next 2-beat phrase
          const ph = SONG[(song = (song + (dir > 0 ? 1 : SONG.length - 1)) % SONG.length)],
            s16 = 60 / 152 / 4,
            t1 = t + 0.18;
          ph.m.forEach((m, i) => m && tone(m, t1 + i * s16, { type: "square", dur: s16 * 1.6, vol: 0.07, cutoff: 5200, detune: 5 }));
          for (let i = 0; i < 4; i++) tone(ph.b + (i % 2 ? 12 : 0), t1 + i * s16 * 2, { type: "triangle", dur: s16 * 1.8, vol: 0.16, cutoff: 1400, detune: 2 });
          for (let i = 0; i < 8; i += 2) hat(t1 + (i + 1) * s16);
        }),
      omen: () =>
        play(t => {
          [36, 36.1, 43].forEach((m, i) => {
            const o = ac.createOscillator(),
              f = ac.createBiquadFilter(),
              g = ac.createGain();
            o.type = "sawtooth";
            o.frequency.value = hz(m);
            f.type = "lowpass";
            f.Q.value = 8;
            f.frequency.setValueAtTime(120, t + 0.4);
            f.frequency.linearRampToValueAtTime(520, t + 1.8);
            f.frequency.exponentialRampToValueAtTime(90, t + 4);
            o.connect(f);
            f.connect(g);
            g.connect(out);
            g.gain.setValueAtTime(0.0001, t + 0.4);
            g.gain.exponentialRampToValueAtTime(i === 2 ? 0.05 : 0.11, t + 1.6);
            g.gain.exponentialRampToValueAtTime(0.0001, t + 4.2);
            o.start(t + 0.4);
            o.stop(t + 4.3);
          });
          const b = ac.createOscillator(),
            bg = ac.createGain();
          b.type = "sine";
          b.frequency.setValueAtTime(70, t + 0.45);
          b.frequency.exponentialRampToValueAtTime(28, t + 1.6);
          b.connect(bg);
          bg.connect(out);
          env(bg, t + 0.45, 0.01, 0.6, 1.4);
          b.start(t + 0.45);
          b.stop(t + 2);
        }),
      step: (dir = 1) =>
        play(t => {
          const m = 72 + SCALE[(Math.random() * 6) | 0];
          tone(dir > 0 ? m : m - 5, t, { type: "square", dur: 0.18, vol: 0.06, cutoff: 4200, detune: 4 });
          tone(dir > 0 ? m + 12 : m - 17, t + 0.06, { type: "triangle", dur: 0.25, vol: 0.05, cutoff: 5000, detune: 2 });
        }),
      toggle() {
        muted = !muted;
        if (muted) techno(0);
        localStorage.setItem("aw-mute", muted ? "1" : "0");
        if (!muted) this.step();
        return muted;
      },
      get muted() {
        return muted;
      },
    };
  })();
  window.sfx = sfx;
  snd.classList.toggle("off", sfx.muted);

  // ---------- navigation ----------
  let cur = -1,
    redT = null;
  // entering a "red" slide forward: the current slide first fades to red, then the red slide slides in from the right
  function go(n) {
    n = Math.max(0, Math.min(slides.length - 1, n));
    if (n === cur || redT) return;
    if (cur >= 0 && n > cur && slides[n].red && !slides[cur].red && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
      wash.classList.add("on", "cover");
      redT = setTimeout(() => {
        redT = null;
        show(n, "left");
      }, 1000);
      return;
    }
    show(n);
  }
  function show(n, horiz) {
    const dir = cur >= 0 && n < cur ? -1 : 1;
    wash.classList.toggle("on", !!slides[n].red);
    gwash.classList.toggle("on", !!slides[n].green);
    sfx.cut();
    sfx.techno(slides[n].techno || 0);
    if (cur >= 0) sfx.slide(n, dir, slides[n].sound);
    // "up"/"down" on a slide: it enters that way going forward, and reverses going back
    const flip = { up: "down", down: "up" };
    const vert = horiz || cur < 0 ? null : dir > 0 && flip[slides[n].trans] ? slides[n].trans : dir < 0 && flip[slides[cur].trans] ? flip[slides[cur].trans] : null;
    const TR = ["up-in", "up-out", "down-in", "down-out", "left-in"];
    if (cur >= 0 && horiz) {
      // already hidden under the red wash: drop it instantly so it doesn't flash back when the wash moves behind the stage
      const old = slides[cur].el;
      clearTimeout(old._lt);
      old.classList.remove("active", "leaving", ...TR);
    } else if (cur >= 0) {
      const old = slides[cur].el;
      old.classList.remove("active", ...TR);
      old.style.setProperty("--dx", dir > 0 ? "100%" : "-100%");
      old.classList.add("leaving");
      if (vert) old.classList.add(vert + "-out");
      clearTimeout(old._lt);
      old._lt = setTimeout(() => old.classList.remove("leaving", ...TR), vert ? 1000 : 800);
    }
    const el = slides[n].el;
    clearTimeout(el._lt);
    el.classList.remove(...TR);
    if (vert) el.classList.add(vert + "-in");
    if (horiz) el.classList.add(horiz + "-in");
    wash.classList.remove("cover");
    el.style.setProperty("--dx", cur < 0 ? "0%" : dir > 0 ? "100%" : "-100%");
    el.classList.remove("leaving");
    void el.offsetWidth; // restart CSS animations on re-entry
    el.classList.add("active");
    cur = n;
    slides[n].enter?.(dir);
    progress.style.transform = `scaleX(${(n + 1) / slides.length})`;
    [...dots.children].forEach((d, i) => d.classList.toggle("on", i === n));
    prevBtn.disabled = n === 0;
    nextBtn.disabled = n === slides.length - 1;
    count.textContent = `${String(n + 1).padStart(2, "0")} / ${String(slides.length).padStart(2, "0")}`;
    notes.textContent = slides[n].notes || "";
    history.replaceState(null, "", `#${n + 1}`);
  }
  const next = () => {
      if (slides[cur].onNext?.()) sfx.step(1);
      else go(cur + 1);
    },
    prev = () => {
      if (slides[cur].onPrev?.()) sfx.step(-1);
      else go(cur - 1);
    };

  addEventListener("keydown", e => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if ((e.key === "Enter" || e.key === " ") && e.target.closest?.("a, button, input, textarea, select, [contenteditable]")) return;
    if (slides[cur].onKey?.(e)) {
      e.preventDefault();
      return;
    }
    if (["ArrowRight", "ArrowDown", "PageDown", " ", "Enter"].includes(e.key)) {
      e.preventDefault();
      next();
    } else if (["ArrowLeft", "ArrowUp", "PageUp", "Backspace"].includes(e.key)) {
      e.preventDefault();
      prev();
    } else if (e.key === "Home") go(0);
    else if (e.key === "End") go(slides.length - 1);
    else if (e.key === "f" || e.key === "F") document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen?.();
    else if (e.key === "n" || e.key === "N") notes.classList.toggle("on");
    else if (e.key === "m" || e.key === "M") toggleSound();
  });
  addEventListener("click", e => {
    if (e.target.closest("a, button")) return;
    e.clientX < innerWidth / 3 ? prev() : next();
  });
  let tx = null;
  addEventListener("touchstart", e => (tx = e.touches[0].clientX), { passive: true });
  addEventListener("touchend", e => {
    if (tx == null) return;
    const dx = e.changedTouches[0].clientX - tx;
    tx = null;
    if (Math.abs(dx) > 50) dx < 0 ? next() : prev();
  });
  addEventListener("hashchange", () => go((parseInt(location.hash.slice(1), 10) || 1) - 1));

  // ---------- animated background ----------
  const ctx = canvas.getContext("2d");
  const hex = c => [1, 3, 5].map(i => parseInt(c.slice(i, i + 2), 16));
  let colors = slides[0].palette.map(hex),
    gAng = 0;
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

    const target = (slides[cur] || slides[0]).palette.map(hex);
    colors = REDUCED ? target : colors.map((c, i) => c.map((v, k) => v + (target[i][k] - v) * 0.03));

    ctx.globalCompositeOperation = "source-over";
    ctx.fillStyle = "#0d1117";
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

    // Blueprint grid: 32px minor, 160px major lines, anchored centre-top.
    // It turns to 45° (around the screen centre) while the red "Safety and control" wash is up,
    // and to -45° while the green "Safe outputs" / "Private enclaves" wash is up.
    if (gridc.width !== bw || gridc.height !== bh) {
      gridc.width = bw;
      gridc.height = bh;
    }
    const g = gridc.getContext("2d");
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, cw, ch);
    const gTarget = wash.classList.contains("on") ? Math.PI / 4 : gwash.classList.contains("on") ? -Math.PI / 4 : 0;
    gAng = REDUCED ? gTarget : gAng + (gTarget - gAng) * 0.035;
    if (Math.abs(gTarget - gAng) < 1e-4) gAng = gTarget;
    const k = Math.min(cw / W, ch / H),
      minor = 32 * k,
      D = Math.hypot(cw, ch) / 2 + minor;
    g.translate(cw / 2, ch / 2);
    g.rotate(gAng);
    const line = i => (((i % 5) + 5) % 5 ? "rgba(240,246,252,0.028)" : "rgba(240,246,252,0.07)");
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
})();
