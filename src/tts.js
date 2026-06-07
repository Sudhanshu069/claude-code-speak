import { GoogleTTSProvider } from './providers/google.js';
import { ElevenLabsTTSProvider } from './providers/elevenlabs.js';
import { MacOSTTSProvider } from './providers/macos.js';

const PROVIDERS = {
  google: GoogleTTSProvider,
  elevenlabs: ElevenLabsTTSProvider,
  macos: MacOSTTSProvider,
};

export function createProvider(config) {
  const name = config.provider || 'google';
  const ProviderClass = PROVIDERS[name];
  if (!ProviderClass) {
    throw new Error(`Unknown TTS provider: "${name}". Available: ${Object.keys(PROVIDERS).join(', ')}`);
  }
  return new ProviderClass(config);
}

export function listProviders() {
  return Object.keys(PROVIDERS);
}
