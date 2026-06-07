import { writeFileSync, unlinkSync, existsSync, mkdirSync } from 'fs';
import { execFile } from 'child_process';
import { join } from 'path';
import { tmpdir } from 'os';

const TEMP_DIR = join(tmpdir(), 'claude-speak-audio');

export class AudioPlayer {
  constructor() {
    this.currentProcess = null;
    if (!existsSync(TEMP_DIR)) {
      mkdirSync(TEMP_DIR, { recursive: true });
    }
  }

  /**
   * Play an audio buffer. Returns a promise that resolves when playback finishes.
   * @param {Buffer} audioBuffer - Raw audio data
   * @param {string} format - Audio format ('wav', 'mp3')
   */
  play(audioBuffer, format = 'wav') {
    return new Promise((resolve, reject) => {
      const ext = format === 'mp3' ? '.mp3' : '.wav';
      const tmpFile = join(TEMP_DIR, `chunk-${Date.now()}${ext}`);

      writeFileSync(tmpFile, audioBuffer);

      // Use afplay on macOS (built-in, no dependencies)
      this.currentProcess = execFile('afplay', [tmpFile], (error) => {
        this.currentProcess = null;
        // Clean up temp file
        try {
          if (existsSync(tmpFile)) unlinkSync(tmpFile);
        } catch {}

        if (error && error.killed) {
          resolve(); // stopped intentionally
        } else if (error) {
          reject(error);
        } else {
          resolve();
        }
      });
    });
  }

  stop() {
    if (this.currentProcess) {
      this.currentProcess.kill();
      this.currentProcess = null;
    }
  }
}
