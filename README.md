# claude-says

[![npm version](https://img.shields.io/npm/v/claude-says.svg)](https://www.npmjs.com/package/claude-says)
[![license](https://img.shields.io/npm/l/claude-says.svg)](./LICENSE)
[![node](https://img.shields.io/node/v/claude-says.svg)](https://nodejs.org)
[![platform: macOS](https://img.shields.io/badge/platform-macOS-black.svg)](#requirements)

Stop staring at your terminal waiting for Claude Code to finish. **claude-says** reads Claude's output aloud in real-time so you can step away, stretch, or keep working — and just listen.

Built this because babysitting the screen every time Claude Code made changes got old. Now the laptop says what Claude is building.

> **macOS only** for now — playback uses `afplay` and the default voice uses the built-in `say` command.

## Install

```bash
npm install -g claude-says
```

> **Lean install (recommended):** the Google Cloud TTS provider pulls in a large optional dependency tree (~100 packages, ~15 MB). If you use the built-in macOS `say` voice (the default) or ElevenLabs, skip it:
>
> ```bash
> npm install -g claude-says --omit=optional
> ```
>
> You can add it later for Google Cloud TTS: `npm install -g @google-cloud/text-to-speech`.

## Quick Start

```bash
# 1. Setup (installs the Claude Code hook, tests audio)
claude-says setup

# 2. Start the daemon in one terminal
claude-says

# 3. Use Claude Code in another terminal as normal
claude
```

That's it. When Claude responds, you'll hear it spoken aloud.

> Just installed and the `claude-says` command isn't found / won't tab-complete? Run `rehash` (zsh) or open a new terminal — your shell is caching its command list. See [Troubleshooting](#troubleshooting).

## Why?

- Claude Code runs can take minutes — you shouldn't have to watch text scroll the whole time.
- You might miss when Claude asks for input or confirmation.
- Sometimes you just want to code from the couch.

## Requirements

- **macOS** (uses `afplay` for playback, `say` for the default voice)
- **Node.js >= 18**
- **Claude Code CLI** installed (`claude-says setup` registers a `Stop` hook with it)

## TTS Providers

| Provider | Setup | Latency | Cost |
|----------|-------|---------|------|
| `macos` (default) | None | Lowest (local) | Free |
| `google` | API key required | ~1–2s / sentence | Pay per use |
| `elevenlabs` | API key required (paid plan) | ~0.5–1s | Pay per use |

```bash
# Use a specific provider
claude-says setup --provider macos
claude-says --provider google
```

### macOS (default)

Works out of the box using the built-in `say` command. No API keys needed.

```bash
# Pick a voice
claude-says voices              # List English voices
claude-says voices --all        # List all voices
claude-says --voice "Daniel"    # Use a specific voice

# Control speech rate (words per minute, default: 200)
claude-says --rate 150          # Slower
claude-says --rate 250          # Faster

# Combine options
claude-says --voice "Karen" --rate 150
```

### Google Cloud TTS

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
claude-says setup --provider google
```

### ElevenLabs

```bash
export ELEVENLABS_API_KEY=your-key
claude-says setup --provider elevenlabs
```

## Narrator mode (optional)

Instead of reading Claude's output verbatim, narrator mode runs the text through an LLM that rephrases it into a short, spoken-friendly summary before it's voiced — less "reading markdown out loud," more "a colleague telling you what just happened." Currently powered by Google Gemini.

```bash
export GEMINI_API_KEY=your-key
claude-says --narrator
```

## Commands

```bash
claude-says              # Start daemon (listen to all sessions)
claude-says -l           # Pick a session interactively
claude-says -s <id>      # Listen to a specific session
claude-says -p <name>    # Use a specific TTS provider
claude-says --narrator   # Enable LLM narrator mode (summarizes output)
claude-says --voice "Daniel"  # Use a specific macOS voice
claude-says --rate 150   # Adjust speech rate (words per minute)
claude-says setup        # Configure provider and install hook
claude-says sessions     # List Claude Code sessions
claude-says providers    # List available TTS providers
claude-says voices       # List available macOS voices
```

## Controls (while the daemon is running)

| Key | Action |
|-----|--------|
| `p` | Pause / Resume |
| `s` | Switch session |
| `q` | Quit |

## Configuration

Settings live in `~/.claude-says/config.json` and are merged over the defaults. You only need to include the keys you want to change.

```json
{
  "provider": "macos",
  "macos": { "voice": "Samantha", "rate": 200 },
  "google": {
    "voice": "en-US-Neural2-D",
    "languageCode": "en-US",
    "audioEncoding": "LINEAR16",
    "sampleRateHertz": 24000
  },
  "elevenlabs": {
    "voiceId": "21m00Tcm4TlvDq8ikWAM",
    "modelId": "eleven_turbo_v2_5"
  },
  "textProcessor": { "minChunkLength": 10, "maxChunkLength": 500 },
  "narrator": {
    "enabled": false,
    "provider": "gemini",
    "gemini": { "model": "gemini-2.5-flash" }
  }
}
```

CLI flags (`--voice`, `--rate`, `--provider`, `--narrator`) override the config file for that run.

## How It Works

1. A `Stop` hook in Claude Code fires after each response.
2. The hook reads the session transcript and extracts the new assistant text.
3. Text reaches the `claude-says` daemon — either directly (the daemon watches the transcript file) or via Unix-socket IPC from the hook.
4. The daemon splits text into sentences, strips markdown/code/URL noise, optionally rephrases it via the narrator, synthesizes audio via the TTS provider, and plays it through a sequence-ordered queue.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full design.

## Troubleshooting

- **`claude-says: command not found` right after install, or no tab-completion** — your shell cached its command list. Run `rehash` (zsh) / `hash -r` (bash), or open a new terminal. Confirm it's installed with `claude-says --version`.
- **Nothing is spoken** — make sure the daemon is running (`claude-says`) *and* that `claude-says setup` installed the Stop hook. Test audio with `claude-says voices`.
- **Stray `node_modules` / `package.json` in your home folder** — that comes from running `npm install <pkg>` (without `-g`) while `cd`'d into `~`. Always install global CLIs with `npm install -g`, and never run a bare `npm install` from your home directory.

## Extending

### Adding a TTS provider

Create `src/providers/yourprovider.js` extending `BaseTTSProvider`:

```js
import { BaseTTSProvider } from './base.js';

export class YourProvider extends BaseTTSProvider {
  async synthesize(text) {
    // Convert text to an audio buffer
    return { audio: buffer, format: 'mp3' };
  }

  async validate() {
    return { ok: true };
  }
}
```

Register it in `src/tts.js`.

### Adding a narrator

Create `src/narrators/yournarrator.js` with a `narrate(text)` method, then register it in `src/narrator.js`.

## Maintainers

- **Abhishek Raj** ([@abhishek141001](https://github.com/abhishek141001)) — original author
- **Sudhanshu Singh** ([@Sudhanshu069](https://github.com/Sudhanshu069)) — maintainer

Issues and PRs welcome at [github.com/abhishek141001/claude-says](https://github.com/abhishek141001/claude-says).

## License

MIT
