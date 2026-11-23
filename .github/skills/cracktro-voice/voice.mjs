#!/usr/bin/env node
// Generated voice track for the cracktro kiosk voice (V): one WebM/Opus file per slide, docs/public/cracktro/voice/<id>.webm,
// plus the lookup table voice/index.js (window.CracktroVoice) mapping each slide id to [caption hash, duration s].
// The runtime plays a slide's file only when its hash matches the slide's current captions, else falls back to
// browser speech synthesis, so a stale track never speaks the wrong words. A caption edit regenerates one file.
//
// Run from the repository root:
//   node .github/skills/cracktro-voice/voice.mjs check              # dependency-free sync report; exit 1 when stale
//   node .github/skills/cracktro-voice/voice.mjs build [--force] [--slides a,b] [--jobs N] [--threads N] [--deps DIR] [--cache DIR] [--ffmpeg PATH]
// build needs kokoro-js and ffmpeg-static (see SKILL.md for setup); synthesized sentences are cached in --cache.
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { availableParallelism, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

const ROOT = resolve(fileURLToPath(new URL("../../..", import.meta.url)));
const PUBLIC = join(ROOT, "docs/public");
const VOICE_DIR = join(PUBLIC, "cracktro/voice");
const TABLE = join(VOICE_DIR, "index.js");
const CONFIG_FILE = fileURLToPath(new URL("./voice.json", import.meta.url));
const { loadSlides, captionHash } = await import(pathToFileURL(join(ROOT, "docs/src/lib/cracktro/slides.js")).href);

const CONFIG_TEXT = readFileSync(CONFIG_FILE, "utf8");
const CONFIG = JSON.parse(CONFIG_TEXT);
// Any change to voices, timing, encoding or lexicon invalidates every slide file.
const FINGERPRINT = createHash("sha256").update(JSON.stringify(CONFIG)).digest("hex").slice(0, 12);

export const voiceFile = id => join(VOICE_DIR, `${id}.webm`);

export function readTable(file = TABLE) {
  if (!existsSync(file)) return { version: CONFIG.version, config: "", slides: {} };
  const scope = {};
  new Function("window", readFileSync(file, "utf8"))(scope);
  return scope.CracktroVoice;
}

// Sync report: slides missing or stale in the table, entries for removed slides, missing or orphan files.
export function check(slides = loadSlides(), table = readTable()) {
  const problems = [];
  if (table.config !== FINGERPRINT) problems.push(`voice.json changed (table ${table.config || "none"}, config ${FINGERPRINT}): rebuild every slide`);
  const ids = new Set(slides.map(s => s.id));
  for (const s of slides) {
    const e = table.slides[s.id];
    if (!e) problems.push(`#${s.id}: no voice file in the table`);
    else if (e[0] !== captionHash(s.captions)) problems.push(`#${s.id}: captions changed since its voice was generated`);
    else if (!existsSync(voiceFile(s.id))) problems.push(`#${s.id}: missing voice/${s.id}.webm`);
    else if (!(e[1] > 0)) problems.push(`#${s.id}: bad duration ${e[1]}`);
  }
  for (const id of Object.keys(table.slides)) if (!ids.has(id)) problems.push(`#${id}: entry for a removed slide`);
  if (existsSync(VOICE_DIR)) for (const f of readdirSync(VOICE_DIR)) if (f !== "index.js" && !(f.endsWith(".webm") && ids.has(f.slice(0, -5)))) problems.push(`voice/${f}: orphan file`);
  return problems;
}

// Text as spoken: lexicon substitutions (longest first, whole tokens) so acronyms and product names read correctly.
// Captions stay unchanged; only the synthesized text differs.
export function spoken(text, lexicon = CONFIG.lexicon) {
  const keys = Object.keys(lexicon).sort((a, b) => b.length - a.length);
  const esc = s => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp(`(?<![\\w-])(?:${keys.map(esc).join("|")})(?![\\w-])`, "g");
  return text.replace(re, m => lexicon[m]);
}
const sentences = text => text.match(/[^.!?]+(?:[.!?]+["')\]]*|$)/g)?.map(s => s.trim()).filter(Boolean) ?? [text];

function wav(samples, rate) {
  const b = Buffer.alloc(44 + samples.length * 2);
  b.write("RIFF", 0);
  b.writeUInt32LE(36 + samples.length * 2, 4);
  b.write("WAVEfmt ", 8);
  b.writeUInt32LE(16, 16);
  b.writeUInt16LE(1, 20);
  b.writeUInt16LE(1, 22);
  b.writeUInt32LE(rate, 24);
  b.writeUInt32LE(rate * 2, 28);
  b.writeUInt16LE(2, 32);
  b.writeUInt16LE(16, 34);
  b.write("data", 36);
  b.writeUInt32LE(samples.length * 2, 40);
  for (let i = 0; i < samples.length; i++) b.writeInt16LE(Math.max(-32768, Math.min(32767, Math.round(samples[i] * 32767))), 44 + i * 2);
  return b;
}

// Decoded duration in seconds, read from ffmpeg's null-muxer progress (ffmpeg-static ships no ffprobe).
function decodedSeconds(ffmpeg, file) {
  const { stderr } = spawnSync(ffmpeg, ["-hide_banner", "-stats", "-i", file, "-f", "null", "-"], { encoding: "utf8" });
  const m = [...String(stderr).matchAll(/time=(\d+):(\d+):([\d.]+)/g)].pop();
  return m ? +m[1] * 3600 + +m[2] * 60 + +m[3] : NaN;
}

function deps(o) {
  const dir = resolve(o.deps);
  const req = createRequire(join(dir, "package.json"));
  try {
    const kokoroMain = req.resolve("kokoro-js");
    // transformers as kokoro-js resolves it, so the model class matches
    const transformers = createRequire(kokoroMain).resolve("@huggingface/transformers");
    return { kokoro: kokoroMain, transformers, ffmpeg: o.ffmpeg || req("ffmpeg-static") };
  } catch {
    throw new Error(`missing generator dependencies in ${dir}; run: npm install --prefix "${dir}" ${CONFIG.engine.package} ffmpeg-static@5.3.0`);
  }
}

const clipKey = (text, p) => createHash("sha256").update(JSON.stringify([CONFIG.engine, p.voice, p.speed, text])).digest("hex");

// Every [text, persona] clip a slide list needs, in order; the clip cache is keyed by clipKey.
function clips(slides) {
  const out = [];
  for (const s of slides)
    for (const [turn, line] of s.captions.entries()) for (const sentence of sentences(spoken(line))) out.push([sentence, CONFIG.personas[turn % CONFIG.personas.length]]);
  return out;
}

// Loads Kokoro with a bounded ONNX thread pool (so parallel workers don't oversubscribe) and returns a cached say().
async function loadVoice(o) {
  const d = deps(o);
  const load = async f => ((m => m.default ?? m)(await import(pathToFileURL(f).href)));
  const { KokoroTTS } = await load(d.kokoro);
  const { StyleTextToSpeech2Model, AutoTokenizer } = await load(d.transformers);
  const { engine } = CONFIG;
  const session_options = o.threads ? { intraOpNumThreads: +o.threads, interOpNumThreads: 1 } : undefined;
  const [model, tokenizer] = await Promise.all([StyleTextToSpeech2Model.from_pretrained(engine.model, { dtype: engine.dtype, device: "cpu", session_options }), AutoTokenizer.from_pretrained(engine.model)]);
  const tts = new KokoroTTS(model, tokenizer);
  const cache = resolve(o.cache);
  mkdirSync(cache, { recursive: true });
  const say = async (text, p) => {
    const file = join(cache, `${clipKey(text, p)}.f32`);
    if (existsSync(file)) return new Float32Array(new Uint8Array(readFileSync(file)).buffer);
    const audio = await tts.generate(text, { voice: p.voice, speed: p.speed });
    if (audio.sampling_rate !== engine.sampleRate) throw new Error(`model rate ${audio.sampling_rate} != config ${engine.sampleRate}`);
    const data = Float32Array.from(audio.audio);
    // atomic write: parallel workers share the cache
    writeFileSync(`${file}.${process.pid}`, Buffer.from(data.buffer));
    renameSync(`${file}.${process.pid}`, file);
    return data;
  };
  return { say, ffmpeg: d.ffmpeg };
}

// Worker: synthesize shard k of N of the uncached clips into the cache.
async function synth(o) {
  const ids = o.slides ? new Set(o.slides.split(",")) : null;
  const all = clips(loadSlides().filter(s => !ids || ids.has(s.id)));
  const seen = new Set();
  const todo = all.filter(([t, p]) => {
    const k = clipKey(t, p);
    return !seen.has(k) && seen.add(k) && !existsSync(join(resolve(o.cache), `${k}.f32`));
  });
  const mine = todo.filter((_, i) => i % +o.jobs === +o.shard);
  if (!mine.length) return 0;
  const { say } = await loadVoice(o);
  for (const [i, [t, p]] of mine.entries()) {
    await say(t, p);
    if (i % 10 === 9) console.log(`shard ${o.shard}: ${i + 1}/${mine.length}`);
  }
  return 0;
}

// Fill the clip cache with N worker processes, each with its own model and a slice of the cores.
async function parallelSynth(o, slides) {
  const threads = o.threads || String(Math.max(1, Math.floor(availableParallelism() / o.jobs)));
  const ids = slides.map(s => s.id).join(",");
  const t0 = Date.now();
  console.log(`synthesizing with ${o.jobs} workers x ${threads} threads`);
  const runs = Array.from({ length: +o.jobs }, (_, k) =>
    new Promise((ok, fail) => {
      const args = [fileURLToPath(import.meta.url), "synth", `--shard=${k}`, `--jobs=${o.jobs}`, `--threads=${threads}`, `--deps=${o.deps}`, `--cache=${o.cache}`, `--slides=${ids}`];
      const child = spawn(process.execPath, args, { stdio: ["ignore", "inherit", "pipe"] });
      let err = "";
      child.stderr.on("data", d => (err = (err + d).slice(-2000)));
      // onnxruntime-node can abort during process teardown after all work is written; the cache is the result
      child.on("exit", () => ok());
      child.on("error", fail);
    })
  );
  await Promise.all(runs);
  const left = clips(slides).filter(([t, p]) => !existsSync(join(resolve(o.cache), `${clipKey(t, p)}.f32`))).length;
  if (left) throw new Error(`${left} clips still missing after parallel synthesis; rerun build (cached clips are kept)`);
  console.log(`synthesized in ${((Date.now() - t0) / 1000).toFixed(0)}s`);
}

async function build(o) {
  const slides = loadSlides();
  const old = readTable();
  // A slide is current when the config, its caption hash and its file are unchanged.
  const want = o.slides ? new Set(o.slides.split(",").map(id => id.trim())) : null;
  const stale = slides.filter(s => (!want || want.has(s.id)) && (o.force || old.config !== FINGERPRINT || old.slides[s.id]?.[0] !== captionHash(s.captions) || !existsSync(voiceFile(s.id))));
  const table = { version: CONFIG.version, config: FINGERPRINT, slides: {} };
  if (old.config === FINGERPRINT) for (const s of slides) if (old.slides[s.id] && !stale.includes(s)) table.slides[s.id] = old.slides[s.id];
  console.log(`config ${FINGERPRINT}; ${slides.length} slides, ${stale.length} to generate`);

  if (stale.length) {
    mkdirSync(VOICE_DIR, { recursive: true });
    if (+o.jobs > 1) await parallelSynth(o, stale);
    const { say, ffmpeg } = await loadVoice(o);
    const { personas, timing, encode } = CONFIG;
    const rate = CONFIG.engine.sampleRate;
    const silence = s => new Float32Array(Math.round(s * rate));
    // trim leading/trailing near-silence the model adds, so pauses come only from timing
    const trim = a => {
      let i = 0,
        j = a.length;
      while (i < j && Math.abs(a[i]) < 0.002) i++;
      while (j > i && Math.abs(a[j - 1]) < 0.002) j--;
      return a.subarray(Math.max(0, i - Math.round(0.02 * rate)), Math.min(a.length, j + Math.round(0.05 * rate)));
    };
    for (const s of stale) {
      const parts = [silence(timing.lead)];
      for (const [turn, line] of s.captions.entries()) {
        if (turn) parts.push(silence(timing.turn));
        const p = personas[turn % personas.length];
        for (const [k, sentence] of sentences(spoken(line)).entries()) {
          if (k) parts.push(silence(timing.sentence));
          parts.push(trim(await say(sentence, p)));
        }
      }
      parts.push(silence(timing.tail));
      const all = new Float32Array(parts.reduce((n, p) => n + p.length, 0));
      let off = 0,
        sum = 0,
        n = 0,
        peak = 0;
      for (const p of parts) all.set(p, off), (off += p.length);
      // one gain per slide: target RMS (dBFS) over speech, never past -1 dBFS peak, so every slide plays equally loud
      for (const x of all) if (Math.abs(x) > 0.01) (sum += x * x), n++, (peak = Math.max(peak, Math.abs(x)));
      const gain = Math.min(10 ** (encode.loudness / 20) / Math.sqrt(sum / Math.max(1, n)), 10 ** (-1 / 20) / Math.max(peak, 1e-6));
      for (let i = 0; i < all.length; i++) all[i] *= gain;
      const tmp = join(resolve(o.cache), `${s.id}.${process.pid}.wav`);
      writeFileSync(tmp, wav(all, rate));
      const out = voiceFile(s.id);
      execFileSync(ffmpeg, ["-hide_banner", "-loglevel", "error", "-y", "-i", tmp, "-ac", "1", "-c:a", encode.codec, "-b:a", encode.bitrate, "-vbr", "on", "-application", encode.application, "-compression_level", "10", "-map_metadata", "-1", "-fflags", "+bitexact", "-flags:a", "+bitexact", "-f", "webm", out]);
      rmSync(tmp);
      const duration = +(all.length / rate).toFixed(3);
      const decoded = decodedSeconds(ffmpeg, out);
      if (!(Math.abs(decoded - duration) < 0.1)) throw new Error(`#${s.id}: encoded ${decoded}s, expected ${duration}s`);
      table.slides[s.id] = [captionHash(s.captions), duration];
      console.log(`#${s.id}: ${duration}s, ${(statSync(out).size / 1024).toFixed(0)} KiB`);
    }
  }
  // drop files of removed slides, write the table in deck order
  const ids = new Set(slides.map(s => s.id));
  if (existsSync(VOICE_DIR)) for (const f of readdirSync(VOICE_DIR)) if (f !== "index.js" && !(f.endsWith(".webm") && ids.has(f.slice(0, -5)))) rmSync(join(VOICE_DIR, f));
  const ordered = { version: table.version, config: table.config, slides: {} };
  for (const s of slides) if (table.slides[s.id]) ordered.slides[s.id] = table.slides[s.id];
  writeFileSync(
    TABLE,
    `// Generated by .github/skills/cracktro-voice/voice.mjs from slides.md Captions and voice.json; do not edit.\n// Synthetic speech: Kokoro-82M (Apache-2.0). slides: id -> [caption hash, duration s] of voice/<id>.webm.\nwindow.CracktroVoice = ${JSON.stringify(ordered, null, 0).replace(/\],"/g, '],\n  "')};\n`
  );
  const problems = check(slides, ordered);
  problems.forEach(p => console.log(`stale: ${p}`));
  return problems.length ? 1 : 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const { values: o, positionals } = parseArgs({
    allowPositionals: true,
    options: {
      force: { type: "boolean", default: false },
      deps: { type: "string", default: join(process.env.TMPDIR || tmpdir(), "cracktro-voice-deps") },
      cache: { type: "string", default: join(process.env.TMPDIR || tmpdir(), "cracktro-voice-cache") },
      ffmpeg: { type: "string" },
      jobs: { type: "string", default: String(Math.max(1, Math.min(6, Math.floor(availableParallelism() / 3)))) },
      threads: { type: "string" },
      shard: { type: "string", default: "0" },
      slides: { type: "string" },
    },
  });
  const cmd = positionals[0] || "check";
  // report errors ourselves: the phonemizer's WASM runtime rethrows uncaught errors as a dump of its own source
  try {
  if (cmd === "check") {
    const problems = check();
    problems.forEach(p => console.log(p));
    console.log(problems.length ? `${problems.length} voice problems; run: node .github/skills/cracktro-voice/voice.mjs build` : "voice track in sync");
    process.exitCode = problems.length ? 1 : 0;
  } else if (cmd === "build") process.exitCode = await build(o);
  else if (cmd === "synth") process.exitCode = await synth(o);
  else throw new Error(`unknown command ${cmd}: use check, build or synth`);
  } catch (e) {
    console.error(e.stack || e);
    process.exit(1);
  }
}
