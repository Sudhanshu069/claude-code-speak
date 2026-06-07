import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'fs';
import { join, resolve } from 'path';
import { homedir } from 'os';
import { createProvider, listProviders } from './tts.js';
import { loadConfig, saveConfig } from './config.js';
import { AudioPlayer } from './player.js';

const CLAUDE_SETTINGS_DIR = join(homedir(), '.claude');
const CLAUDE_SETTINGS_FILE = join(CLAUDE_SETTINGS_DIR, 'settings.json');

export async function runSetup(options = {}) {
  console.log('Claude Code Speak — Setup\n');

  const config = loadConfig();
  if (options.provider) {
    config.provider = options.provider;
  }

  // Step 1: Show provider info
  console.log(`TTS Provider: ${config.provider}`);
  console.log(`Available providers: ${listProviders().join(', ')}\n`);

  // Step 2: Validate TTS credentials
  console.log('Validating TTS credentials...');
  const provider = createProvider(config);
  const validation = await provider.validate();

  if (!validation.ok) {
    console.error(`TTS validation failed: ${validation.error}`);
    console.log('');

    if (config.provider === 'google') {
      console.log('Setup instructions for Google Cloud TTS:');
      console.log('  1. Create a Google Cloud project');
      console.log('  2. Enable the Text-to-Speech API');
      console.log('  3. Create a service account key');
      console.log('  4. Set GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json');
    } else if (config.provider === 'elevenlabs') {
      console.log('Setup instructions for ElevenLabs:');
      console.log('  1. Sign up at elevenlabs.io (paid plan required for API)');
      console.log('  2. Get your API key from settings');
      console.log('  3. Set ELEVENLABS_API_KEY=your-key');
    } else if (config.provider === 'macos') {
      console.log('macOS say command should be available by default.');
      console.log('Try: say "hello" in your terminal.');
    }
    return false;
  }

  console.log('TTS credentials valid!\n');

  // Step 3: Test audio playback
  console.log('Testing audio playback...');
  try {
    const result = await provider.synthesize('Claude Code Speak is ready.');
    const player = new AudioPlayer();
    await player.play(result.audio, result.format);
    console.log('Audio playback works!\n');
  } catch (err) {
    console.error(`Audio playback failed: ${err.message}`);
    return false;
  }

  // Step 4: Install Claude Code hook
  console.log('Installing Stop hook...');
  const hookInstalled = installHook();
  if (hookInstalled) {
    console.log('Hook installed successfully!\n');
  } else {
    console.log('Hook installation failed — you may need to add it manually.\n');
  }

  // Step 5: Save config
  saveConfig(config);
  console.log('Configuration saved.\n');

  console.log('Setup complete! Start the daemon with:');
  console.log('  claude-speak\n');
  console.log('Then use Claude Code normally — you\'ll hear it speak.\n');

  return true;
}

function installHook() {
  try {
    // Resolve the absolute path to our hook script
    const hookScriptPath = resolve(
      new URL('../bin/claude-speak-hook.js', import.meta.url).pathname
    );

    // Read existing settings or create new
    let settings = {};
    if (existsSync(CLAUDE_SETTINGS_FILE)) {
      settings = JSON.parse(readFileSync(CLAUDE_SETTINGS_FILE, 'utf-8'));
    }

    // Ensure hooks structure exists
    if (!settings.hooks) {
      settings.hooks = {};
    }

    // Check if our hook is already installed
    const existingHooks = settings.hooks.Stop || [];
    const hookCommand = `node ${hookScriptPath}`;

    const alreadyInstalled = existingHooks.some((group) =>
      group.hooks?.some((h) => h.command?.includes('claude-speak-hook'))
    );

    if (alreadyInstalled) {
      console.log('  Hook already installed.');
      return true;
    }

    // Add our hook
    existingHooks.push({
      matcher: '*',
      hooks: [
        {
          type: 'command',
          command: hookCommand,
          timeout: 5,
        },
      ],
    });

    settings.hooks.Stop = existingHooks;

    // Write back
    if (!existsSync(CLAUDE_SETTINGS_DIR)) {
      mkdirSync(CLAUDE_SETTINGS_DIR, { recursive: true });
    }
    writeFileSync(CLAUDE_SETTINGS_FILE, JSON.stringify(settings, null, 2));

    return true;
  } catch (err) {
    console.error(`  Error installing hook: ${err.message}`);
    return false;
  }
}
