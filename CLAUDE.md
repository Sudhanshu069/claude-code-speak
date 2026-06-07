# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`claude-code-speak` is a real-time text-to-speech companion for Claude Code CLI. It runs as a background daemon that listens for Claude Code's text output and speaks it aloud using a TTS provider.

## Architecture

Two runtime components communicate over a Unix domain socket (`/tmp/claude-speak.sock`):

1. **Hook script** (`bin/claude-speak-hook.js`) — Installed as a Claude Code hook. Reads the session transcript, extracts new assistant text since last invocation (tracked via byte-offset state files in `/tmp/claude-speak-state/`), and sends it to the daemon. Must complete quickly to avoid blocking Claude's output (hard timeout: 3s).

2. **Daemon** (`bin/claude-speak.js` → `src/daemon.js`) — Long-running process with two text ingestion paths:
   - **TranscriptWatcher** (`src/transcript-watcher.js`) — Polls a JSONL transcript file every 200ms, emits `text` events for new assistant messages. Deduplicates by UUID.
   - **IPC fallback** — Receives text from hooks via Unix socket when no transcript is being watched.

### Data Flow

```
Claude Code transcript (JSONL)
  → TranscriptWatcher (poll-based) OR Hook → IPC socket
  → TextProcessor (sentence splitting, markdown/noise filtering)
  → [Optional] Narrator (LLM rephrasing via Gemini)
  → TTS Provider (synthesize to audio buffer)
  → AudioQueue (sequence-ordered FIFO)
  → AudioPlayer (afplay on macOS)
```

### Key Modules

- `src/daemon.js` — Orchestrator. Wires together all components, handles session switching, auto-detects most recent session on startup.
- `src/ipc.js` — Unix socket IPC. Newline-delimited JSON protocol. Exports `IPCServer` (daemon) and `sendToSocket` (hook client).
- `src/text-processor.js` — Buffers streaming text, splits at sentence boundaries, strips markdown/URLs/code blocks, filters noise.
- `src/audio-queue.js` — Sequence-ordered FIFO. Accepts `(seq, audioPromise)` pairs; plays in order regardless of when TTS responses arrive.
- `src/player.js` — Plays audio via `afplay` (macOS). Writes temp files to `/tmp/claude-speak-audio/`.
- `src/tts.js` — Provider factory. `src/providers/` contains implementations extending `BaseTTSProvider`:
  - `macos.js` — macOS `say` command (default, zero config)
  - `google.js` — Google Cloud TTS (requires `GOOGLE_APPLICATION_CREDENTIALS`)
  - `elevenlabs.js` — ElevenLabs API (requires `ELEVENLABS_API_KEY`)
- `src/narrator.js` — Narrator factory. Optional LLM-based rephrasing of text before TTS.
  - `src/narrators/gemini.js` — Uses Gemini API to summarize assistant output into concise spoken narration (requires `GEMINI_API_KEY`).
- `src/transcript-watcher.js` — Polls JSONL transcript files, emits new assistant text blocks.
- `src/sessions.js` — Discovers Claude Code sessions from `~/.claude/projects/`.
- `src/config.js` — Config from `~/.claude-speak/config.json`. Exports `SOCKET_PATH`.
- `src/setup.js` — Interactive setup wizard: validates TTS, tests playback, installs hook into `~/.claude/settings.json`.

## Commands

```bash
npm i                           # install dependencies
npm start                       # start the daemon
npm run setup                   # configure TTS provider and install hook

node bin/claude-speak.js start             # start daemon
node bin/claude-speak.js start -p macos    # start with specific TTS provider
node bin/claude-speak.js start -l          # pick a session interactively
node bin/claude-speak.js start --narrator  # enable LLM narrator mode
node bin/claude-speak.js setup             # run setup wizard
node bin/claude-speak.js sessions          # list discovered sessions
node bin/claude-speak.js providers         # list available TTS providers
```

### Adding a TTS Provider

Create `src/providers/yourprovider.js` extending `BaseTTSProvider` with `synthesize(text)` and `validate()` methods, then register in `src/tts.js`.

### Adding a Narrator

Create `src/narrators/yournarrator.js` with a `narrate(text)` method, then register in `src/narrator.js`.

## Tech Stack

- Node.js >= 18, ES modules (`"type": "module"`)
- `commander` for CLI, `@google-cloud/text-to-speech` (optional dep) for Google TTS
- macOS-specific: `afplay` for playback, `say` for macOS TTS
- No test framework, no TypeScript, no bundler
