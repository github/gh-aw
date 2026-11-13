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
  .slide.active { visibility: visible; pointer-events: auto; animation: wipe-in 1s cubic-bezier(.7,0,.2,1) both; }
  .slide.leaving { visibility: visible; animation: fade-out .7s cubic-bezier(.6,0,.4,1) both; }
  @keyframes wipe-in { from { clip-path: circle(0% at var(--ox, 100%) 50%); } to { clip-path: circle(150% at var(--ox, 100%) 50%); } }
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
  @keyframes up-in { from { transform: translateY(100%); } to { transform: none; } }
  @keyframes up-out { to { transform: translateY(-100%); } }
  @keyframes down-in { from { transform: translateY(-100%); } to { transform: none; } }
  @keyframes down-out { to { transform: translateY(100%); } }
  @keyframes fade-out { to { opacity: 0; transform: scale(.985); filter: blur(8px); } }

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
  .ga-copy h2 { margin: 0; font-size: 116px; line-height: .92; font-variation-settings: "wght" 700, "wdth" 100; }
  .ga-copy h2 .l-grad .ch { background-image: linear-gradient(90deg, #3fb950, #4493f8, #ab7df8, #3fb950); }
  .ga-copy .meta { font: 500 26px/1.4 var(--mono); color: #9198a1; }
  .ga-copy .meta a { color: #4493f8; text-decoration: none; }
  .ga-copy .meta a:hover { text-decoration: underline; }
  .active .ga-copy .meta { animation: rise .8s cubic-bezier(.2,.9,.2,1) both 1s; }
  .badge { align-self: flex-start; font: 500 22px/1 "Mona Sans"; color: #3fb950; background: #2ea04326;
    border: 1px solid #2ea04366; padding: 10px 18px; border-radius: 2em; box-shadow: 0 0 30px #2ea04333; }
  .active .badge { animation: rise .7s both .1s, pulse 2.4s ease-in-out infinite 1s; }
  @keyframes pulse { 50% { box-shadow: 0 0 50px #2ea04388; } }
  .shot-wrap { position: absolute; right: 90px; top: 150px; width: 1080px; height: 780px; perspective: 1800px; }
  .shot { position: absolute; inset: 0; border-radius: 12px; overflow: hidden; background: #0d1117; border: 1px solid #3d444d;
    box-shadow: 0 60px 140px -20px #000, 0 0 0 1px #000, 0 0 120px -30px #3fb950;
    transform: rotateY(-14deg) rotateX(6deg); }
  .active .shot { animation: shot-in 1.4s cubic-bezier(.2,.9,.2,1) both .35s, float 7s ease-in-out infinite 1.8s; }
  @keyframes shot-in { from { opacity: 0; transform: translateY(120px) rotateY(-24deg) rotateX(16deg) scale(.95); } }
  @keyframes float { 0%, 100% { transform: rotateY(-14deg) rotateX(6deg) translateY(0); } 50% { transform: rotateY(-9deg) rotateX(3deg) translateY(-8px); } }
  .shot .bar { height: 48px; display: flex; align-items: center; gap: 10px; padding: 0 20px; background: #151b23; border-bottom: 1px solid #3d444d; }
  .shot .bar i { width: 14px; height: 14px; border-radius: 50%; }
  .shot .bar .url { margin-left: 18px; flex: 1; font: 500 17px/32px var(--mono); color: #9198a1; background: #0d1117;
    border-radius: 6px; padding: 0 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .shot img { display: block; width: 100%; height: calc(100% - 48px); object-fit: cover; object-position: 50% 0%; }
  .active .shot img { animation: pan 14s ease-in-out infinite alternate 2s; }
  @keyframes pan { to { object-position: 50% 100%; } }
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
  .fm-shifts { position: absolute; left: 114px; top: 500px; width: 580px; display: flex; flex-direction: column; gap: 22px; }
  .fm-shift { display: grid; grid-template-columns: 56px 1fr; column-gap: 16px; opacity: 0; }
  .active .fm-shift { animation: rise .8s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 180ms + 800ms); }
  .fm-shift i { grid-row: span 2; font-size: 40px; font-style: normal; line-height: 1; }
  .fm-shift b { font-size: 34px; font-variation-settings: "wght" 700, "wdth" 100; color: var(--c); }
  .fm-shift span { font-size: 21px; color: #9198a1; line-height: 1.35; }
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
  .active .sec-box { animation: rise .9s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 220ms + 400ms); }
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
  .active .sec-box.out { animation: rise .9s cubic-bezier(.2,.9,.2,1) both 840ms, calm 4s ease-in-out infinite 2s; }
  @keyframes calm { 0%, 100% { box-shadow: 0 0 60px -14px #4493f888, inset 0 0 80px -40px #4493f855; } 50% { box-shadow: 0 0 110px 0 #4493f855, inset 0 0 100px -40px #4493f877; } }
  .sec-box.sandbox { background: #0b1a10f0; border-width: 3px; z-index: 2; }
  .active .sec-box.sandbox { animation: rise .9s cubic-bezier(.2,.9,.2,1) both 620ms, safe 3.2s ease-in-out infinite 1.6s; }
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
  .sec-arrow { position: absolute; top: 570px; width: 100px; text-align: center; font-size: 60px; color: #3d444d; }
  .active .sec-arrow { animation: nudge 1.6s ease-in-out infinite; }
  @keyframes nudge { 50% { transform: translateX(10px); color: #9198a1; } }
  .sec-ent { position: absolute; left: 110px; top: 268px; display: flex; align-items: center; gap: 14px; }
  .sec-ent i { font: 600 18px/1 var(--mono); letter-spacing: .25em; text-transform: uppercase; color: #d29922; font-style: normal; margin-right: 10px; }
  .sec-ent span { font-size: 24px; padding: 8px 20px; border-radius: 99px; border: 1px solid #d2992266; background: #d2992214; color: #f0f6fc; opacity: 0; }
  .active .sec-ent span { animation: rise .7s cubic-bezier(.2,.9,.2,1) both calc(var(--i) * 120ms + 1200ms); }
  .confetti { position: absolute; inset: 0; width: 1920px; height: 1080px; pointer-events: none; z-index: 5; }
  .sec-lane { position: absolute; left: 0; top: 880px; width: 1920px; height: 120px; }

  /* --- prompt to factory --- */
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
    const conf = h("canvas", { class: "confetti", width: 1920, height: 1080 });
    const cctx = conf.getContext("2d");
    const CC = ["#3fb950", "#4493f8", "#a371f7", "#f778ba", "#d29922", "#ffffff", "#56d364"];
    let bits = [],
      cLast = 0;
    const burst = (x, y, dir, n) => {
      for (let i = 0; i < n; i++) {
        const a = -Math.PI / 2 + dir * (0.35 + Math.random() * 0.55),
          v = 14 + Math.random() * 16;
        bits.push({
          x,
          y,
          vx: Math.cos(a) * v,
          vy: Math.sin(a) * v,
          r: Math.random() * 6,
          vr: (Math.random() - 0.5) * 0.4,
          w: 10 + Math.random() * 10,
          hh: 5 + Math.random() * 6,
          c: CC[(Math.random() * CC.length) | 0],
          ph: Math.random() * 6,
        });
      }
    };
    slides.push({
      sound: "victory",
      trans: "up",
      techno: 1,
      enter() {
        bits = [];
        cLast = 0;
        setTimeout(() => {
          burst(-20, 1100, 1, 140);
          burst(1940, 1100, -1, 140);
        }, 200);
        setTimeout(() => {
          burst(960, 1120, 0.15, 60);
          burst(960, 1120, -0.15, 60);
        }, 900);
      },
      tick() {
        const now = performance.now(),
          dt = cLast ? Math.min(3, (now - cLast) / 16.7) : 1;
        cLast = now;
        cctx.clearRect(0, 0, 1920, 1080);
        bits = bits.filter(b => b.y < 1140);
        for (const b of bits) {
          b.vx *= 0.985 ** dt;
          b.vy = b.vy * 0.985 ** dt + 0.38 * dt;
          b.x += b.vx * dt;
          b.y += b.vy * dt;
          b.r += b.vr * dt;
          b.ph += 0.15 * dt;
          cctx.save();
          cctx.translate(b.x + Math.sin(b.ph) * 6, b.y);
          cctx.rotate(b.r);
          cctx.scale(1, Math.cos(b.ph));
          cctx.fillStyle = b.c;
          cctx.fillRect(-b.w / 2, -b.hh / 2, b.w, b.hh);
          cctx.restore();
        }
      },
      palette: ["#238636", "#1f6feb", "#8957e5", "#2ea043"],
      notes:
        "GitHub Agentic Workflows compiles Markdown into GitHub Actions workflows. This diagram is an illustrative example, not a screenshot of a real repository or workflow run. See the public documentation for setup and availability.",
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "ga-bg" }, h("div", {}, ga + ga), h("div", {}, ga + ga)),
        h(
          "div",
          { class: "ga-copy" },
          h("div", { class: "badge" }, "● GITHUB ACTIONS"),
          h("h2", {}, letters("Built on", "l-grad"), h("br"), letters("GitHub"), h("br"), letters("Actions.")),
          h("div", { class: "meta" }, "Markdown workflows. Built-in guardrails.", h("br"), h("a", { href: DOCS_URL, target: "_blank", rel: "noopener" }, "Read the documentation"))
        ),
        h(
          "div",
          { class: "shot-wrap" },
          h(
            "div",
            { class: "shot" },
            h("div", { class: "bar" }, h("i", { style: { background: "#ff5f57" } }), h("i", { style: { background: "#febc2e" } }), h("i", { style: { background: "#28c840" } }), h("div", { class: "url" }, DOCS_URL.replace("https://", ""))),
            h("img", { src: "assets/workflow-run.svg", alt: "Illustrative workflow: Markdown compiles into GitHub Actions, with an isolated agent and separate safe outputs" }),
            h("div", { class: "glare" })
          )
        ),
        conf
      ),
    });
  }

  // Prompt to factory: prompt → markdown → issue (from the gh-aw hero)
  {
    resetIndex();
    const PROMPT = "Create a daily agentic workflow that gives me an issue summary";
    const MD = [
      ["---", "#656c76"],
      ["on: ", "#ff7b72", "daily", "#a5d6ff"],
      ["safe-outputs:", "#ff7b72"],
      ["  create-issue:", "#7ee787"],
      ["---", "#656c76"],
      ["Summarise yesterday's issues. Flag", "#f0f6fc"],
      ["anything that needs a reply.", "#f0f6fc"],
    ];
    const MDLEN = MD.reduce((n, l) => n + l[0].length + (l[2] || "").length + 1, 0);
    const msg = h("div", { class: "msg" }),
      reply = h("div", { class: "reply" }, "✓ Added ", h("code", {}, "daily-issue-summary.md"), " and opened a pull request.");
    const pre = h("pre", {}),
      ypre = h("pre", {});
    // what gh aw compile emits: a hardened, pinned GitHub Actions workflow
    const YML = [
      ["name: Daily Issue Summary", "#f0f6fc"],
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
    const lis = [h("li", {}, n1, " new issues, ", n2, " closed"), h("li", {}, n3, " waiting on a maintainer reply"), h("li", {}, h("b", {}, "#482"), " blocked on design review")];
    const cards = [
      h("div", { class: "pf-card pf-prompt" }, h("div", { class: "cap" }, "01 · Prompt"), h("div", { class: "sb" }, "9:41", h("span", {}, "▂▄▆ ▮")), h("div", { class: "you" }, "Copilot"), msg, reply),
      h("div", { class: "pf-card pf-md" }, h("div", { class: "cap" }, "02 · Markdown"), h("div", { class: "file" }, "📄 .github/workflows/daily-issue-summary.md"), pre),
      h("div", { class: "pf-card pf-yml" }, h("div", { class: "cap" }, "03 · Lock file"), h("div", { class: "file" }, "⚙ daily-issue-summary​.lock.yml"), ypre, go),
      h("div", { class: "pf-card pf-issue" }, h("div", { class: "cap" }, "04 · Issue"), h("h4", {}, "Issue summary — Oct 2 ", h("small", {}, "#241"), h("span", { class: "open" }, "◉ Open")), h("ul", {}, lis)),
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
      snd = { p: 0, m: -2, go: false, card: [] };
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
        "Prompt to factory: ask your coding agent in plain language, it writes a markdown workflow, then gh aw compile — a deterministic Go compiler, no AI involved — turns the .md into a hardened .lock.yml Actions workflow with pinned actions, a firewall and safe-output jobs. Same markdown in, same YAML out, so it is reviewable and diffable, and the next morning the issue summary shows up. Time to agent: about 1 minute from prompt to a running workflow. This mirrors the gh-aw hero page.",
      enter() {
        t0 = 0;
        snd = { p: 0, m: -2, go: false, card: [] };
        [...cards, ...links, ...tags, reply, go, ...lis].forEach(e => e.classList.remove("on"));
        msg.replaceChildren(caret());
        pre.replaceChildren();
        ypre.replaceChildren();
      },
      tick(t) {
        if (!t0) t0 = t;
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
        const mdEnd = 4.2 + MDLEN / 45;
        on(links[1], mdEnd + 0.3);
        on(tags[1], mdEnd + 0.5);
        on(cards[2], mdEnd + 0.5);
        const yk = Math.max(0, Math.min(YML.length, Math.floor((e - mdEnd - 0.8) * 22))); // compiles in a blink
        if (ypre.childElementCount !== yk) ypre.replaceChildren(...YML.slice(0, yk).flatMap(([l, c]) => [h("span", { style: { color: c } }, l), "\n"]));
        const ymlEnd = mdEnd + 0.8 + YML.length / 22;
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
        n1.textContent = Math.round(9 * ez);
        n2.textContent = Math.round(6 * ez);
        n3.textContent = Math.round(2 * ez);
      },
      el: h("section", { class: "slide" }, h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "How it works"), h("h2", {}, letters("Prompt to "), letters("factory", "l-grad"))), tta, links, tags, cards),
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
              items.map(x => h("li", {}, x))
            )
          )
        ),
        [0, 1].map(i => h("div", { class: "sec-arrow", style: { left: `${BX[i] + 500}px` } }, "→")),
        lane
      ),
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
  // Human Agent Processes: Repo Assist & unslopper swimlanes
  {
    resetIndex();
    const shifts = [
      ["🏭", "Code → process", "Turn recurring maintenance into reviewed workflows", "#ab7df8"],
      ["🧹", "Merge now, unslop later", "Cleanup agents simplify, refactor and restyle the repo every day", "#f778ba"],
      ["🌈", "A teammate on every repo", "Repo Assist triages, investigates, fixes and reports monthly", "#3fb950"],
    ];
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
      {
        key: "Unslopper",
        sub: "continuous cleanup loop",
        c: "#f778ba",
        pick: "Daily sweep",
        tasks: [
          ["Code simplifier", "pr"],
          ["Dead code remover", "pr"],
          ["Duplicate detector", "issue"],
          ["Semantic refactor", "pr"],
          ["Unbloat docs", "pr"],
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
    const agent = (x, y, r = 30) => `<circle cx="${x}" cy="${y}" r="${r}" fill="url(#fmAgent)"/>${ico("copilot", x, y + 1, r * 1.15)}`;
    const pill = (x, y, w, label, cls = "") => `<g class="fm-pill ${cls}"><rect x="${x}" y="${y - 23}" width="${w}" height="46" rx="23"/><text x="${x + 15 + w / 2}" y="${y + 8}">${label}</text></g>`;
    const svgFor = L => {
      let g = `<defs><radialGradient id="fmAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
        <marker id="fmArrP" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#ab7df8"/></marker>
        <marker id="fmArrB" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0L10 5L0 10z" fill="#4493f8"/></marker></defs>`;
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
        g += `<path class="fm-wire p" id="fmIn${i}" d="M240 ${SY} H272 V${y} H${300 - 4}" marker-end="url(#fmArrP)" style="opacity:${y === SY ? 1 : 0}"/>`;
        g += `<path class="fm-wire p" d="M272 ${y} H296" marker-end="url(#fmArrP)"/>`;
        g += `<path class="fm-wire p" id="fmOut${i}" d="M610 ${y} H${OX - 2}" marker-end="url(#fmArrP)"/>`;
        g += `<path class="fm-wire b" id="fmRev${i}" d="M${OX + 58} ${y} H${OX + 72} V${MY} H${MX - 42}" marker-end="url(#fmArrB)"/>`;
        g += `<g class="fm-task" id="fmTask${i}">${pill(320, y, 290, L.tasks[i][0])}${agent(320, y)}</g>`;
        g += `<g class="fm-out" id="fmOutI${i}"><rect x="${OX}" y="${y - 30}" width="58" height="60" rx="12"/>${ico(L.tasks[i][1], OX + 29, y, 30)}</g>`;
      });
      g += `<path class="fm-wire b" id="fmLoop" d="M${MX} ${MY + 38} V${VH - 20} H52 V${SY + 36}" marker-end="url(#fmArrB)"/>`;
      g += `<g class="fm-sched">${pill(40, SY, 200, L.pick)}${agent(40, SY)}</g>`;
      g += `<g class="fm-watch">${ico("stopwatch", 150, 530, 64, "#f0f6fc")}<line id="fmHand" x1="150" y1="532" x2="150" y2="512" stroke="#f0f6fc" stroke-width="4" stroke-linecap="round"/></g>`;
      g += `<g class="fm-maint"><g class="fm-pill b"><rect x="${MX}" y="${MY - 23}" width="172" height="46" rx="23"/><text x="${MX + 38 + 67}" y="${MY + 8}">Maintainer</text></g>
        <circle cx="${MX}" cy="${MY}" r="38" fill="#0d1117" stroke="#4493f8" stroke-width="3"/>${ico("person", MX, MY, 46)}</g>`;
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
      const medY = d => {
        const f = Math.min(D1 - D0, Math.max(0, d - D0)),
          i = Math.floor(f);
        return y(med[i] + (med[Math.min(i + 1, med.length - 1)] - med[i]) * (f - i));
      };
      const drop = Math.round(100 - med[med.length - 1]);
      const svgFor = () => {
        let g = `<defs><radialGradient id="fmAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
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
        g += `<g id="bdBot" class="bd-bot"><circle cx="0" cy="0" r="22" fill="url(#fmAgent)"/>${ico("copilot", 0, 1, 26)}</g>`;
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
    const makeFlow = (svg, cap, loops) => {
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
        svg.innerHTML = svgFor(L);
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
    const hapCap = h("div", { class: "fm-cap" });
    const hapSvg = newSvg("fm-svg");
    const hapFlow = makeFlow(hapSvg, hapCap, [0, 1]);
    slides.push({
      sound: "loop",
      trans: "down",
      palette: ["#bf3989", "#8957e5", "#238636", "#9e6a03"],
      notes:
        "Human Agent Processes. Replace recurring manual fixes with reviewed processes. Repo Assist illustrates repository maintenance; the unslopper illustrates continuous cleanup. Each loop is scheduled, the agent picks tasks and proposes labels, comments or pull requests, and a maintainer reviews the result: SCHEDULE → ACT → REVIEW. These are illustrative workflows, not activity attributed to particular people or teams.",
      enter() {
        hapFlow.reset();
      },
      tick(t) {
        hapFlow.step(t);
      },
      el: h(
        "section",
        { class: "slide" },
        h("div", { class: "slide-head" }, h("div", { class: "kicker" }, "Illustrative maintenance loops"), h("h2", {}, letters("Human Agent "), letters("Processes", "l-grad"))),
        h("div", { class: "fm-quote" }, "Build the ", h("em", {}, "factory"), " that works for you."),
        h(
          "div",
          { class: "fm-shifts" },
          shifts.map(([ic, b, sp, c], i) => h("div", { class: "fm-shift", "--i": i, "--c": c }, h("i", {}, ic), h("b", {}, b), h("span", {}, sp)))
        ),
        hapCap,
        hapSvg
      ),
    });

    // Repo Assist: the maintenance loop next to its backlog burndown
    resetIndex();
    const raCap = h("div", { class: "fm-cap ra-cap-flow" });
    const raSvg = newSvg("fm-svg ra-flow");
    const raFlow = makeFlow(raSvg, raCap, [0]);
    const bdSvg = newSvg("fm-svg ra-chart");
    const bd = makeBurndown(bdSvg);
    const BD_PERIOD = 15000;
    let bdT0 = 0,
      bdRun = -1;
    slides.push({
      sound: "loop",
      trans: "down",
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
      <circle cx="${x}" cy="${y}" r="34" fill="#0d1117" stroke="#4493f8" stroke-width="3"/>${ico("person", x, y, 34, "#79c0ff")}</g>`;
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
    let g = `<defs><radialGradient id="mraAgent" cx="35%" cy="30%" r="80%"><stop offset="0" stop-color="#c297ff"/><stop offset=".55" stop-color="#8957e5"/><stop offset="1" stop-color="#3b1d8f"/></radialGradient>
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
  const gridc = h("canvas", { class: "gridc" });
  document.body.append(canvas, wash, gridc, stage, progress, dots, count, snd, notes);

  const fit = () => {
    const s = Math.min(innerWidth / W, innerHeight / H);
    stage.style.transform = `scale(${s}) translate(-50%, -50%)`;
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
    let ac = null,
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
      out = ac.createGain();
      out.gain.value = 0.5;
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
      out.connect(comp);
      out.connect(dl);
      dl.connect(lp);
      lp.connect(fb);
      fb.connect(dl);
      lp.connect(wet);
      wet.connect(comp);
      comp.connect(ac.destination);
      return ac;
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
      // Party poppers, a brass fanfare and a sparkle of confetti bells
      victory: t => {
        [0.12, 0.3].forEach(d => {
          const src = ac.createBufferSource(),
            f = ac.createBiquadFilter(),
            g = ac.createGain();
          src.buffer = noise();
          f.type = "bandpass";
          f.frequency.value = 1800;
          f.Q.value = 0.8;
          src.connect(f);
          f.connect(g);
          g.connect(out);
          env(g, t + d, 0.002, 0.35, 0.12);
          src.start(t + d, Math.random() * 0.5);
          src.stop(t + d + 0.2);
        });
        const b = 0.11,
          br = { type: "sawtooth", vol: 0.09, cutoff: 4200, detune: 9 };
        [
          [67, 0],
          [67, b],
          [67, 2 * b],
          [72, 3 * b, 0.5],
          [67, 3 * b + 0.55, 0.14],
          [72, 3 * b + 0.72, 0.14],
        ].forEach(([m, d, dur = 0.09]) => tone(m, t + 0.35 + d, { ...br, dur }));
        const c = t + 0.35 + 3 * b + 0.88;
        [72, 76, 79, 84].forEach(m => tone(m, c, { ...br, dur: 1.3, vol: 0.06 }));
        tone(48, c, { type: "triangle", dur: 1.3, vol: 0.2, cutoff: 900 });
        tone(36, c, { type: "sine", dur: 1.4, vol: 0.2, cutoff: 600 });
        for (let i = 0; i < 18; i++) bell(88 + SCALE[(Math.random() * SCALE.length) | 0], t + 0.2 + Math.random() * 2.2, 0.025, 0.8);
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
        TZ.send.connect(out);
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
      old.classList.add("leaving");
      if (vert) old.classList.add(vert + "-out");
      clearTimeout(old._lt);
      old._lt = setTimeout(() => old.classList.remove("leaving", ...TR), vert ? 1000 : 700);
    }
    const el = slides[n].el;
    clearTimeout(el._lt);
    el.classList.remove(...TR);
    if (vert) el.classList.add(vert + "-in");
    if (horiz) el.classList.add(horiz + "-in");
    wash.classList.remove("cover");
    el.style.setProperty("--ox", dir > 0 ? "100%" : "0%");
    el.classList.remove("leaving");
    void el.offsetWidth; // restart CSS animations on re-entry
    el.classList.add("active");
    cur = n;
    slides[n].enter?.(dir);
    progress.style.transform = `scaleX(${(n + 1) / slides.length})`;
    [...dots.children].forEach((d, i) => d.classList.toggle("on", i === n));
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
    // It turns to 45° (around the screen centre) while the red "Safety and control" wash is up.
    if (gridc.width !== bw || gridc.height !== bh) {
      gridc.width = bw;
      gridc.height = bh;
    }
    const g = gridc.getContext("2d");
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, cw, ch);
    const gTarget = wash.classList.contains("on") ? Math.PI / 4 : 0;
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
