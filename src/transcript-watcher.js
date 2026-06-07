import { statSync, openSync, readSync, closeSync } from 'fs';
import { EventEmitter } from 'events';

const POLL_INTERVAL_MS = 200;

/**
 * Watches a Claude Code transcript JSONL file in real-time via polling.
 * Emits 'text' events for each new assistant text block as it's written.
 * Uses polling instead of fs.watch() for reliable macOS support.
 */
export class TranscriptWatcher extends EventEmitter {
  constructor(transcriptPath) {
    super();
    this.path = transcriptPath;
    this.offset = 0;
    this.pollTimer = null;
    this.buffer = '';
    this.processedLines = new Set();
  }

  start() {
    // Start from the end of the file (only process new content)
    try {
      const stat = statSync(this.path);
      this.offset = stat.size;
    } catch {
      this.offset = 0;
    }

    // Poll for changes every 300ms
    this.pollTimer = setInterval(() => this._readNewContent(), POLL_INTERVAL_MS);

    this.emit('watching', { path: this.path });
  }

  stop() {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
  }

  _readNewContent() {
    try {
      const stat = statSync(this.path);
      if (stat.size <= this.offset) return;

      this.emit('newdata', { bytes: stat.size - this.offset });

      // Read only the new bytes
      const fd = openSync(this.path, 'r');
      const newSize = stat.size - this.offset;
      const buf = Buffer.alloc(newSize);
      readSync(fd, buf, 0, newSize, this.offset);
      closeSync(fd);

      this.offset = stat.size;

      // Append to line buffer and process complete lines
      this.buffer += buf.toString('utf-8');
      const lines = this.buffer.split('\n');
      this.buffer = lines.pop(); // keep incomplete last line in buffer

      for (const line of lines) {
        if (!line.trim()) continue;
        this._processLine(line);
      }
    } catch (err) {
      this.emit('error', err);
    }
  }

  _processLine(line) {
    try {
      const entry = JSON.parse(line);

      // Only process assistant messages with text content
      if (entry.type !== 'assistant') return;
      if (!entry.message?.content) return;

      // Use UUID to avoid processing the same message twice
      // (Claude may write partial then complete messages)
      const uuid = entry.uuid;
      if (uuid && this.processedLines.has(uuid)) return;
      if (uuid) this.processedLines.add(uuid);

      // Limit memory: keep only last 1000 UUIDs
      if (this.processedLines.size > 1000) {
        const entries = [...this.processedLines];
        this.processedLines = new Set(entries.slice(-500));
      }

      // Extract text blocks (skip tool_use blocks)
      for (const block of entry.message.content) {
        if (block.type === 'text' && block.text) {
          const sessionId = entry.sessionId || entry.session_id || 'unknown';
          this.emit('text', {
            session_id: sessionId,
            text: block.text,
            timestamp: Date.now(),
          });
        }
      }
    } catch {
      // Skip malformed lines
    }
  }
}
