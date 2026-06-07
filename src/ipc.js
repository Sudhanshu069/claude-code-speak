import net from 'net';
import { existsSync, unlinkSync } from 'fs';
import { EventEmitter } from 'events';
import { SOCKET_PATH } from './config.js';

export class IPCServer extends EventEmitter {
  constructor() {
    super();
    this.server = null;
    this.clients = new Set();
  }

  start() {
    return new Promise((resolve, reject) => {
      if (existsSync(SOCKET_PATH)) {
        unlinkSync(SOCKET_PATH);
      }

      this.server = net.createServer((socket) => {
        this.clients.add(socket);
        let buffer = '';

        socket.on('data', (data) => {
          buffer += data.toString();
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
      this.server.listen(SOCKET_PATH, () => resolve());
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
  return new Promise((resolve, reject) => {
    const client = net.createConnection(SOCKET_PATH, () => {
      client.write(JSON.stringify(data) + '\n');
      client.end();
      resolve();
    });

    client.on('error', (err) => {
      // Daemon not running — silently fail
      resolve();
    });

    // Timeout after 100ms to avoid blocking Claude's display
    client.setTimeout(100, () => {
      client.destroy();
      resolve();
    });
  });
}
