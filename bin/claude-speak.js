#!/usr/bin/env node

import { program } from 'commander';
import { Daemon } from '../src/daemon.js';
import { runSetup } from '../src/setup.js';
import { discoverSessions } from '../src/sessions.js';
import { listProviders } from '../src/tts.js';
import readline from 'readline';

program
  .name('claude-speak')
  .description('Real-time text-to-speech companion for Claude Code')
  .version('1.0.0');

program
  .command('start', { isDefault: true })
  .description('Start the speak daemon')
  .option('-p, --provider <name>', `TTS provider (${listProviders().join(', ')})`)
  .option('-s, --session <id>', 'Listen to a specific session ID')
  .option('-l, --list', 'List available sessions and pick one')
  .option('-n, --narrator', 'Enable narrator mode (LLM rephrases output before speaking)')
  .option('--narrator-provider <name>', 'Narrator LLM provider (default: gemini)')
  .action(async (options) => {
    // If --list, show session picker first
    if (options.list) {
      const picked = await pickSession();
      if (!picked) {
        console.log('No session selected.');
        process.exit(0);
      }
      options.session = picked.sessionId;
      options.transcriptPath = picked.transcriptPath;
    }

    const daemon = new Daemon({
      provider: options.provider,
      session: options.session,
      transcriptPath: options.transcriptPath,
      narrator: options.narrator,
    });

    // Graceful shutdown
    const shutdown = async () => {
      await daemon.stop();
      process.exit(0);
    };
    process.on('SIGINT', shutdown);
    process.on('SIGTERM', shutdown);

    await daemon.start();

    // Interactive controls
    setupControls(daemon);
  });

program
  .command('setup')
  .description('Configure TTS provider and install Claude Code hook')
  .option('-p, --provider <name>', `TTS provider (${listProviders().join(', ')})`)
  .action(async (options) => {
    const success = await runSetup({ provider: options.provider });
    process.exit(success ? 0 : 1);
  });

program
  .command('sessions')
  .description('List discovered Claude Code sessions')
  .action(() => {
    const sessions = discoverSessions();
    if (sessions.length === 0) {
      console.log('No sessions found.');
      return;
    }
    console.log('Recent Claude Code sessions:\n');
    for (const s of sessions.slice(0, 20)) {
      console.log(`  ${s.sessionId.slice(0, 8)}  ${s.projectName}  (${s.lastActiveFormatted})`);
    }
    console.log(`\nTotal: ${sessions.length} sessions`);
  });

program
  .command('providers')
  .description('List available TTS providers')
  .action(() => {
    console.log('Available TTS providers:');
    for (const p of listProviders()) {
      console.log(`  - ${p}`);
    }
  });

program.parse();

async function pickSession() {
  const sessions = discoverSessions();
  if (sessions.length === 0) {
    console.log('No sessions found.');
    return null;
  }

  console.log('\nSelect a Claude Code session to listen to:\n');
  const display = sessions.slice(0, 15);
  display.forEach((s, i) => {
    console.log(`  ${i + 1}. ${s.sessionId.slice(0, 8)}  ${s.projectName}  (${s.lastActiveFormatted})`);
  });
  console.log(`  0. Listen to all sessions\n`);

  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  return new Promise((resolve) => {
    rl.question('Enter number: ', (answer) => {
      rl.close();
      const num = parseInt(answer, 10);
      if (num === 0) {
        resolve(null);
      } else if (num >= 1 && num <= display.length) {
        resolve(display[num - 1]);
      } else {
        resolve(null);
      }
    });
  });
}

function setupControls(daemon) {
  if (!process.stdin.isTTY) return;

  readline.emitKeypressEvents(process.stdin);
  process.stdin.setRawMode(true);

  console.log('Controls: [p]ause/resume  [s]witch session  [q]uit\n');

  let paused = false;
  process.stdin.on('keypress', async (str, key) => {
    if (key.ctrl && key.name === 'c') {
      await daemon.stop();
      process.exit(0);
    }

    switch (key.name) {
      case 'p':
        paused = !paused;
        if (paused) {
          daemon.audioQueue.pause();
          console.log('[Paused]');
        } else {
          daemon.audioQueue.resume();
          console.log('[Resumed]');
        }
        break;

      case 's': {
        // Quick session switch
        const sessions = discoverSessions();
        if (sessions.length === 0) {
          console.log('No sessions found.');
          break;
        }
        console.log('\nSessions:');
        sessions.slice(0, 10).forEach((s, i) => {
          const active = daemon.activeSession === s.sessionId ? ' *' : '';
          console.log(`  ${i + 1}. ${s.sessionId.slice(0, 8)} ${s.projectName}${active}`);
        });
        console.log('  0. All sessions');
        // Note: full interactive switching would need readline,
        // but for now we just show the list. User can restart with -s flag.
        console.log('Restart with -s <id> to switch, or -l to pick interactively.\n');
        break;
      }

      case 'q':
        await daemon.stop();
        process.exit(0);
    }
  });
}
