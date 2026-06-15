import net from 'net';
import { existsSync, unlinkSync, chmodSync, lstatSync, mkdirSync } from 'fs';
import { dirname } from 'path';
import { EventEmitter } from 'events';
import { SOCKET_PATH } from './config.js';

// Cap per-connection buffering so a client that never sends a newline
// can't grow daemon memory without bound. Real messages are a sentence or two.
const MAX_BUFFER_BYTES = 1024 * 1024; // 1 MB

export class IPCServer extends EventEmitter {
  constructor() {
    super();
    this.server = null;
    this.clients = new Set();
  }

  start() {
    return new Promise((resolve, reject) => {
      // The socket lives in the per-user config dir (owner-owned, 0700), not in
      // world-writable /tmp, so no other local user can pre-create or symlink
      // this path to intercept transcript text or hijack/DoS the daemon.
      try {
        mkdirSync(dirname(SOCKET_PATH), { recursive: true, mode: 0o700 });
      } catch (err) {
        reject(err);
        return;
      }

      // Clean up a stale socket from a previous run. lstat does NOT follow
      // symlinks; only ever unlink a real socket, and refuse to bind through
      // anything else (a symlink or regular file would be unexpected in our
      // owner-only dir and must never be followed).
      try {
        const st = lstatSync(SOCKET_PATH);
        if (st.isSocket()) {
          unlinkSync(SOCKET_PATH);
        } else {
          reject(new Error(
            `Refusing to use ${SOCKET_PATH}: it exists but is not a socket. Remove it and try again.`
          ));
          return;
        }
      } catch (err) {
        if (err.code !== 'ENOENT') {
          reject(err);
          return;
        }
        // ENOENT: nothing there yet — fine.
      }

      this.server = net.createServer((socket) => {
        this.clients.add(socket);
        let buffer = '';

        socket.on('data', (data) => {
          buffer += data.toString();
          if (buffer.length > MAX_BUFFER_BYTES) {
            // Oversized, newline-less stream — drop the connection.
            buffer = '';
            socket.destroy();
            return;
          }
          const lines = buffer.split('\n');
          buffer = lines.pop();

          for (const line of lines) {
            if (line.trim()) {
              try {
                const msg = JSON.parse(line);
                this.emit('message', msg);
              } catch {
                // skip malformed messages
              }
            }
          }
        });

        socket.on('close', () => {
          this.clients.delete(socket);
        });

        socket.on('error', () => {
          this.clients.delete(socket);
        });
      });

      this.server.on('error', reject);
      this.server.listen(SOCKET_PATH, () => {
        // Restrict the socket to the owning user. On macOS the filesystem
        // permission is enforced on connect, so this blocks other local
        // accounts from injecting speech into the daemon.
        try {
          chmodSync(SOCKET_PATH, 0o600);
        } catch {
          // best-effort; listen already succeeded
        }
        resolve();
      });
    });
  }

  stop() {
    return new Promise((resolve) => {
      for (const client of this.clients) {
        client.destroy();
      }
      this.clients.clear();

      if (this.server) {
        this.server.close(() => {
          if (existsSync(SOCKET_PATH)) {
            unlinkSync(SOCKET_PATH);
          }
          resolve();
        });
      } else {
        resolve();
      }
    });
  }
}

export function sendToSocket(data) {
  // Resolves true when the message was handed to the daemon, false when the
  // daemon was unreachable or the send timed out. Callers (the hook) use this
  // to avoid advancing past text that was never delivered. Never rejects.
  return new Promise((resolve) => {
    let settled = false;
    const done = (delivered) => {
      if (settled) return;
      settled = true;
      resolve(delivered);
    };

    // Only ever write transcript text to a real socket. lstat does not follow
    // symlinks; if the path is missing (daemon down) or not a socket, report
    // non-delivery rather than connecting/writing through an unexpected path.
    try {
      if (!lstatSync(SOCKET_PATH).isSocket()) {
        done(false);
        return;
      }
    } catch {
      done(false);
      return;
    }

    const client = net.createConnection(SOCKET_PATH, () => {
      client.write(JSON.stringify(data) + '\n');
      client.end();
      done(true);
    });

    client.on('error', () => {
      // Daemon not running / unreachable.
      done(false);
    });

    // Timeout to avoid blocking Claude's display; treat as non-delivery.
    client.setTimeout(100, () => {
      client.destroy();
      done(false);
    });
  });
}
