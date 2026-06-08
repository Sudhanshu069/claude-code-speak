# claude-says

Stop staring at your terminal waiting for Claude Code to finish. **claude-says** reads Claude's output aloud in real-time so you can step away, stretch, or keep working — and just listen.

Built this because I was tired of babysitting my screen every time Claude Code was making changes. Now my laptop tells me what Claude is building.

## Install

```bash
npm install -g claude-says
```

## Quick Start

```bash
# 1. Setup (installs Claude Code hook, tests audio)
claude-speak setup

# 2. Start the daemon in one terminal
claude-speak

# 3. Use Claude Code in another terminal as normal
claude
```

That's it. When Claude responds, you'll hear it spoken aloud.

## Why?

- Claude Code runs can take minutes — you shouldn't have to watch text scroll the entire time
- You might miss when Claude asks for input or confirmation
- Sometimes you just want to code from your couch

## TTS Providers

| Provider | Setup | Latency | Cost |
|----------|-------|---------|------|
| `macos` (default) | None | Lowest (local) | Free |
| `google` | API key required | ~1-2s/sentence | Pay per use |
| `elevenlabs` | API key required (paid plan) | ~0.5-1s | Pay per use |

```bash
# Use a specific provider
claude-speak setup --provider macos
claude-speak --provider google
```

### macOS (default)
Works out of the box using the built-in `say` command. No API keys needed.

```bash
# Pick a voice
claude-speak voices              # List English voices
claude-speak voices --all        # List all 177 voices
claude-speak --voice "Daniel"    # Use a specific voice

# Control speech rate (words per minute, default: 200)
claude-speak --rate 150          # Slower
claude-speak --rate 250          # Faster

# Combine options
claude-speak --voice "Karen" --rate 150
```

You can also set these permanently in `~/.claude-speak/config.json`:
```json
{ "macos": { "voice": "Daniel", "rate": 150 } }
```

### Google Cloud TTS
```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
claude-speak setup --provider google
```

### ElevenLabs
```bash
export ELEVENLABS_API_KEY=your-key
claude-speak setup --provider elevenlabs
```

## Commands

```bash
claude-speak              # Start daemon (listen to all sessions)
claude-speak -l           # Pick a session interactively
claude-speak -s <id>      # Listen to a specific session
claude-speak -p <name>    # Use a specific TTS provider
claude-speak --narrator   # Enable LLM narrator mode (summarizes output)
claude-speak --voice "Daniel"  # Use a specific macOS voice
claude-speak --rate 150   # Adjust speech rate (words per minute)
claude-speak setup        # Configure provider and install hook
claude-speak sessions     # List Claude Code sessions
claude-speak providers    # List available TTS providers
claude-speak voices       # List available macOS voices
```

## Controls (while daemon is running)

| Key | Action |
|-----|--------|
| `p` | Pause / Resume |
| `s` | Show sessions |
| `q` | Quit |

## How It Works

1. A `Stop` hook in Claude Code fires after each response
2. The hook reads the session transcript and extracts the assistant's text
3. Text is sent to the `claude-speak` daemon via Unix socket IPC
4. The daemon splits text into sentences, strips markdown noise, generates audio via TTS, and plays it through an ordered queue

## Adding a Provider

Create `src/providers/yourprovider.js` extending `BaseTTSProvider`:

```js
import { BaseTTSProvider } from './base.js';

export class YourProvider extends BaseTTSProvider {
  async synthesize(text) {
    // Convert text to audio buffer
    return { audio: buffer, format: 'mp3' };
  }

  async validate() {
    return { ok: true };
  }
}
```

Register it in `src/tts.js`.

## License

MIT
