---
name: speech-audio-encoding
description: Generate speech with an open local TTS model and encode it with ffmpeg as small, deterministic, web-playable Opus files.
---

# Speech audio encoding

Use this skill to turn text into speech audio that ships in a repository or a static site. It is based on the githubnext/agentics `workflows/shared/ffmpeg.md` guidance (Opus for voice, strip metadata, hash inputs for caching). For the cracktro deck, the `cracktro-voice` skill wraps all of this in one script.

## Rules

- Generate with a local open model: Kokoro-82M (`kokoro-js`, Apache-2.0, 24 kHz mono). No cloud TTS, keys or uploads. Write the model id, dtype and voice ids into a committed config so a rerun reproduces the voice.
- Install tools in `$TMPDIR`, never in the repository: `npm install --prefix "$TMPDIR/<name>-deps" kokoro-js@1.2.1 ffmpeg-static@5.3.0`. `ffmpeg-static` ships libopus; a system `ffmpeg` with `--enable-libopus` works too (`ffmpeg -hide_banner -encoders | grep opus`).
- Set `HF_HOME="$TMPDIR/hf"` so the model download (about 330 MB fp32, 90 MB q8) lands in scratch. The first run needs network; later runs are offline.
- Commit only the encoded files and a generated lookup table, never WAV, caches or `node_modules`.
- Label the output as synthetic speech in the generated table's header.

## Licenses (checked for kokoro-js 1.2.1)

| Component | License | Shipped? |
|---|---|---|
| Kokoro-82M weights and voices (`hexgrad/Kokoro-82M`) | Apache-2.0; commercial use allowed. Training data was permissive, public-domain or synthetic audio, plus under 12 h of CC BY audio (Koniwa, SIWIS) credited on the model card. | No, only the generated audio |
| ONNX conversion (`onnx-community/Kokoro-82M-v1.0-ONNX`) | Apache-2.0 | No |
| `kokoro-js`, `phonemizer`, `@huggingface/transformers` | Apache-2.0 | No |
| `onnxruntime-node` | MIT | No |
| `ffmpeg-static` (FFmpeg binary) | GPL-3.0-or-later | No: a build tool only; GPL does not cover its output files |

Only the encoded audio is committed. Credit the model in the generated file header (for example "Synthetic speech: Kokoro-82M (Apache-2.0)"). Re-check these licenses when you bump a version or switch models, and never use a voice clone of a real person.

## Generate

```js
import { KokoroTTS } from "kokoro-js";
const tts = await KokoroTTS.from_pretrained("onnx-community/Kokoro-82M-v1.0-ONNX", { dtype: "fp32", device: "cpu" });
const audio = await tts.generate("Hello, and welcome!", { voice: "af_nova", speed: 1.1 }); // audio.audio: Float32Array, audio.sampling_rate: 24000
```

- Synthesize one sentence at a time: long inputs drift in pace and are truncated near the model's token limit.
- Trim the model's leading and trailing near-silence, then insert your own pauses (silence buffers) so timing is controlled and identical everywhere.
- Rewrite acronyms and product names for speech with a lexicon (`gh-aw` → "G H A W") before synthesis; keep the displayed text unchanged.
- Cache each clip by `sha256(model, dtype, voice, speed, text)` in `$TMPDIR`; regenerate only what changed.
- Normalize once per output file (one gain for the whole file), targeting about -16 dBFS RMS over speech with a -1 dBFS peak cap, so clips inside a file keep their relative level.

## Encode

Concatenate in memory, write one 16-bit PCM WAV, then:

```bash
ffmpeg -hide_banner -loglevel error -y -i in.wav \
  -ac 1 -c:a libopus -b:a 32k -vbr on -application audio -compression_level 10 \
  -map_metadata -1 -fflags +bitexact -flags:a +bitexact -f webm out.webm
```

| Choice | Value | Why |
|---|---|---|
| Codec | Opus (`libopus`) | Best quality per bit for speech; decoded natively by Chrome, Edge, Firefox and Safari 15+. |
| Container | WebM (`.webm`, `audio/webm; codecs=opus`) | Broadest browser support for Opus, including Safari; Ogg works everywhere except older Safari. |
| Channels | mono (`-ac 1`) | Speech has no stereo image; halves the size. |
| Bitrate | 32 kbps VBR | Near-transparent for 24 kHz synthetic speech; 20k and below add a metallic, robotic edge to TTS, and 12k (agentics default) is audibly band-limited. About 240 KB per minute. |
| Application | `audio` | Full-band, natural timbre; `voip` favours intelligibility over naturalness and makes synthetic voices sound more robotic. |
| Determinism | `-map_metadata -1 -fflags +bitexact -flags:a +bitexact` | No encoder tags or timestamps, so identical input gives identical bytes and clean diffs. |

For a fallback where Opus is unavailable, AAC: `-c:a aac -b:a 32k -ac 1 -movflags +faststart out.m4a` (about twice the size). Feature-detect with `audio.canPlayType("audio/webm; codecs=opus")` and fall back to browser `speechSynthesis` rather than shipping both, unless the target audience needs it.

## Verify

- Duration: decode and compare to the PCM length (`ffmpeg -i out.webm -f null -` prints the final `time=`; `ffprobe -v error -show_entries format=duration -of csv=p=0 out.webm` if ffprobe exists). They must agree within 0.1 s, otherwise segment offsets in the lookup table are wrong.
- Listen to at least one file start, middle and end (`afplay`, or the browser).
- Record each file's duration and every segment's `[start, end]` in seconds (3 decimals) in the lookup table; seek with `audio.currentTime = start` and stop at `end`.
- Size: report KiB per file; investigate anything over about 200 KB per minute.
