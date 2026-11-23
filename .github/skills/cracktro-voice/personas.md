# Cracktro voice personas

The kiosk captions (`slides.md` Captions) are a two-host GitHub developer-relations show: two developer advocates walking a developer audience through a feature, like a livestream or conference demo. Every caption and every generated audio file uses these two identities. Change them only together with `voice.json` and a full rebuild.

Turn 1, 3, 5, ... is the **Host**; turn 2, 4, 6, ... is the **Builder**. The generator assigns voices by turn parity, so never put two consecutive lines in one speaker's mouth.

| | Host | Builder |
|---|---|---|
| Role | Developer advocate who frames the topic and asks the questions the audience would ask. | Hands-on engineer advocate who answers with the mechanism, the example or the trade-off. |
| Voice (`voice.json`) | `am_eric`, speed 1.05 | `af_nova`, speed 1.1 |
| Tone | Bright, energetic, curious, like a YouTube developer channel; welcomes newcomers. | Calm, practical, precise; a little dry humour. |
| Typical turn | "Let's look at...", "Here's a question we hear a lot...", "Up next..." | "It works like this...", "In practice...", "Exactly, and..." |

They are roles, not people: never name them, give them a gender in text, or tie them to a real person, handle or mascot. Captions refer to the audience as "you", "folks" or "teams".

## Identity (both hosts)

- Developer-first: explain what it does, why it matters and when to use it; prefer a concrete example over an adjective.
- Friendly and corporate-safe: inclusive, plain language; no slang that needs explaining, sarcasm, fear-mongering, competitor comparisons, pricing promises or roadmap dates.
- Honest: say "experimental" or "research prototype" when the slide is; never overclaim ("always", "guaranteed", "zero risk").
- Light humour only on light slides; none on security, enterprise or cost incidents.
- Not a newscast: no "tonight", "desk", "breaking", "story", "correspondent", "good evening", "stay tuned".
- Section intros use "Up next, the <band> section."; the finale ends "Thanks for joining us, and happy building!"

## Writing for speech

- One or two short sentences per turn, under about 30 words; the voice pauses between sentences (`timing.sentence`) and turns (`timing.turn`).
- Spell out symbols a voice can't read: write "versus", not "vs."; "and", not "&"; avoid parentheses, slashes and emoji.
- Product names and acronyms stay as written; the generator rewrites them for speech through `voice.json` `lexicon` (for example `gh-aw` → "G H A W", `YAML` → "yammel"). Add a lexicon entry, not a respelling in the caption, when a new term reads wrong.
- Numbers in words when they are short ("two rounds"); keep figures that age out of captions, as for Summaries.

## Audio identity (`voice.json`)

| Setting | Value | Why |
|---|---|---|
| Engine | Kokoro-82M ONNX (`kokoro-js`), fp32, 24 kHz | Open model (Apache-2.0), runs offline on CPU, deterministic for identical text. |
| Voices | `am_eric` / `af_nova` | Fresh, expressive American English pair with contrasting pitch: a male interviewer and a female expert; `af_nova` sounds livelier and less synthetic than `af_heart`. |
| Speed | 1.05 / 1.1 | Creator-video pace: brisk but clear; each voice at the speed that sounds most natural for it. |
| Pauses | lead 0.15 s, sentence 0.15 s, turn 0.35 s, tail 0.4 s | Natural hand-offs; the tail is a beat before the kiosk moves on. |
| Loudness | -16 dBFS speech RMS, -1 dBFS peak cap, one gain per slide | Even level across slides, under the synth music. |

Any change to this file's voices, timing, encoding or lexicon changes the config fingerprint and requires rebuilding every slide file (`SKILL.md`).
