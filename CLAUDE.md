# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`claude-code-speak` (npm package: `claude-says`) is a real-time text-to-speech companion for Claude Code CLI. It runs as a background daemon that listens for Claude Code's text output and speaks it aloud using a TTS provider. macOS-only (uses `afplay` for playback).

## Architecture

Two runtime components communicate over a Unix domain socket (`/tmp/claude-speak.sock`):

1. **Hook script** (`bin/claude-speak-hook.js`) — Installed as a Claude Code `Stop` hook. Reads the session transcript, extracts new assistant text since last invocation (tracked via byte-offset state files in `/tmp/claude-speak-state/`), and sends it to the daemon. Must complete within 3s to avoid blocking Claude's output.

2. **Daemon** (`bin/claude-speak.js` → `src/daemon.js`) — Long-running process with two text ingestion paths:
   - **TranscriptWatcher** (`src/transcript-watcher.js`) — Polls a JSONL transcript file every 200ms, emits `text` events for new assistant messages. Deduplicates by UUID.
   - **IPC fallback** — Receives text from hooks via Unix socket when no transcript is being watched.

### Data Flow

```
Claude Code transcript (JSONL)
  -> TranscriptWatcher (poll) OR Hook -> IPC socket
  -> TextProcessor (sentence splitting, markdown/noise filtering)
  -> [Optional] Narrator (LLM rephrasing via Gemini)
  -> TTS Provider (synthesize to audio buffer)
  -> AudioQueue (sequence-ordered FIFO)
  -> AudioPlayer (afplay)
```

### Key Modules

- `src/daemon.js` — Orchestrator. Wires all components together, handles session switching, auto-detects most recent session.
- `src/text-processor.js` — Buffers streaming text, splits at sentence boundaries, strips markdown/URLs/code blocks, filters noise.
- `src/audio-queue.js` — Sequence-ordered FIFO. Plays audio in order regardless of when TTS responses arrive.
- `src/ipc.js` — Unix socket IPC. Newline-delimited JSON protocol. Exports `IPCServer` (daemon) and `sendToSocket` (hook).
- `src/tts.js` — Provider factory. Providers in `src/providers/` extend `BaseTTSProvider` with `synthesize(text)` and `validate()`.
- `src/narrator.js` — Narrator factory. Narrators in `src/narrators/` rephrase text via LLM before TTS.
- `src/sessions.js` — Discovers Claude Code sessions from `~/.claude/projects/`.
- `src/config.js` — Config from `~/.claude-speak/config.json`. Exports `SOCKET_PATH`, `DEFAULT_CONFIG`.

### Runtime Paths

- Config: `~/.claude-speak/config.json`
- Socket: `/tmp/claude-speak.sock`
- Hook state: `/tmp/claude-speak-state/`
- Audio temp files: `/tmp/claude-speak-audio/`

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
node bin/debug-hook.js                     # debug hook execution manually
```

## Extending

### Adding a TTS Provider

Create `src/providers/yourprovider.js` extending `BaseTTSProvider` with `synthesize(text)` and `validate()` methods, then register in `src/tts.js`.

### Adding a Narrator

Create `src/narrators/yournarrator.js` with a `narrate(text)` method, then register in `src/narrator.js`.

## Tech Stack

- Node.js >= 18, ES modules (`"type": "module"`)
- `commander` for CLI, `@google-cloud/text-to-speech` (optional dep) for Google TTS
- macOS-specific: `afplay` for playback, `say` for macOS TTS
- No test framework, no TypeScript, no bundler
