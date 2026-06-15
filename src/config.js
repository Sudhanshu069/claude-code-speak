import { readFileSync, writeFileSync, existsSync, mkdirSync, chmodSync } from 'fs';
import { homedir } from 'os';
import { join } from 'path';

const CONFIG_DIR = join(homedir(), '.claude-says');
const CONFIG_FILE = join(CONFIG_DIR, 'config.json');
// Keep the socket inside the per-user config dir (owner-owned, not world-
// writable) rather than shared /tmp, so another local user cannot pre-create
// or symlink the path to intercept transcript text or hijack/DoS the daemon.
const SOCKET_PATH = join(CONFIG_DIR, 'claude-says.sock');

const DEFAULT_CONFIG = {
  provider: 'macos',
  macos: {
    voice: 'Samantha',
    rate: 200,
  },
  google: {
    voice: 'en-US-Neural2-D',
    languageCode: 'en-US',
    audioEncoding: 'LINEAR16',
    sampleRateHertz: 24000,
  },
  elevenlabs: {
    voiceId: '21m00Tcm4TlvDq8ikWAM',
    modelId: 'eleven_turbo_v2_5',
  },
  playback: {
    method: 'afplay',
  },
  textProcessor: {
    minChunkLength: 10,
    maxChunkLength: 500,
  },
  narrator: {
    enabled: false,
    provider: 'gemini',
    gemini: {
      model: 'gemini-2.5-flash',
    },
  },
};

function isPlainObject(v) {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

// Recursively overlay `source` onto `target`, descending into nested plain
// objects. A shallow `{ ...DEFAULT_CONFIG, ...saved }` would let a partial user
// config (e.g. {"google":{"voice":"X"}}) replace a whole nested default block,
// silently dropping sibling keys like languageCode/sampleRateHertz. `target` is
// always a fresh clone of DEFAULT_CONFIG, so mutating it never corrupts the
// shared default singleton.
function deepMerge(target, source) {
  for (const key of Object.keys(source)) {
    // Never walk/assign prototype keys — JSON.parse makes "__proto__" an own key,
    // and target["__proto__"] = obj would mutate Object.prototype (prototype
    // pollution). The config is user-owned, but this keeps the merge safe anyway.
    if (key === '__proto__' || key === 'constructor' || key === 'prototype') continue;
    const s = source[key];
    if (isPlainObject(s) && isPlainObject(target[key])) {
      deepMerge(target[key], s);
    } else {
      target[key] = s;
    }
  }
  return target;
}

export function loadConfig() {
  const config = structuredClone(DEFAULT_CONFIG);
  if (!existsSync(CONFIG_FILE)) {
    return config;
  }
  try {
    const saved = JSON.parse(readFileSync(CONFIG_FILE, 'utf-8'));
    return deepMerge(config, saved);
  } catch {
    return config;
  }
}

export function saveConfig(config) {
  if (!existsSync(CONFIG_DIR)) {
    mkdirSync(CONFIG_DIR, { recursive: true, mode: 0o700 });
  }
  // Config can hold provider API keys — keep it owner-only. The mode option on
  // writeFileSync only applies when the file is created, so chmod afterwards to
  // also tighten a pre-existing world-readable config. The chmod is best-effort
  // (it can EPERM on a file owned by another uid, e.g. from a prior sudo run) and
  // must never fail the save — the written contents are the contract.
  writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2), { mode: 0o600 });
  try {
    chmodSync(CONFIG_FILE, 0o600);
  } catch {
    // best-effort hardening; contents already persisted
  }
}

export { SOCKET_PATH, CONFIG_DIR, CONFIG_FILE, DEFAULT_CONFIG };
