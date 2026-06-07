#!/usr/bin/env node

/**
 * Claude Code hook script (PostToolUse + Stop).
 * Called after each tool call and when Claude finishes.
 * Reads new text from the transcript and forwards to daemon.
 * Uses a state file to track what's already been sent.
 */

import { sendToSocket } from '../src/ipc.js';
import { readFileSync, writeFileSync, existsSync } from 'fs';
import { join } from 'path';
import { tmpdir } from 'os';

const STATE_DIR = join(tmpdir(), 'claude-speak-state');
const DEBUG_LOG = join(tmpdir(), 'claude-speak-hook.log');

let input = '';

process.stdin.setEncoding('utf-8');
process.stdin.on('data', (chunk) => { input += chunk; });

process.stdin.on('end', async () => {
  try {
    const data = JSON.parse(input);
    const sessionId = data.session_id || 'unknown';
    const transcriptPath = data.transcript_path || '';

    if (!transcriptPath) {
      process.exit(0);
      return;
    }

    // State file tracks the byte offset we've already processed
    if (!existsSync(STATE_DIR)) {
      const { mkdirSync } = await import('fs');
      mkdirSync(STATE_DIR, { recursive: true });
    }
    const stateFile = join(STATE_DIR, `${sessionId}.offset`);
    let lastOffset = 0;
    if (existsSync(stateFile)) {
      lastOffset = parseInt(readFileSync(stateFile, 'utf-8'), 10) || 0;
    }

    // Read the transcript from where we left off
    const fullTranscript = readFileSync(transcriptPath, 'utf-8');
    const newContent = fullTranscript.slice(lastOffset);

    if (!newContent.trim()) {
      process.exit(0);
      return;
    }

    // Parse new lines and collect assistant text
    const lines = newContent.split('\n');
    const texts = [];

    for (const line of lines) {
      if (!line.trim()) continue;
      try {
        const entry = JSON.parse(line);
        if (entry.type === 'assistant' && entry.message?.content) {
          for (const block of entry.message.content) {
            if (block.type === 'text' && block.text) {
              texts.push(block.text);
            }
          }
        }
      } catch {}
    }

    // Update state with new offset
    writeFileSync(stateFile, String(fullTranscript.length));

    // Send all new text to daemon
    if (texts.length > 0) {
      const combined = texts.join(' ');
      await sendToSocket({
        type: 'text',
        session_id: sessionId,
        text: combined,
        timestamp: Date.now(),
      });
    }
  } catch (err) {
    try {
      writeFileSync(DEBUG_LOG, `[${new Date().toISOString()}] ERROR: ${err.message}\n`, { flag: 'a' });
    } catch {}
  }

  process.exit(0);
});

setTimeout(() => process.exit(0), 3000);
