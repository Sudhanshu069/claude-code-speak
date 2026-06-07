import { GeminiNarrator } from './narrators/gemini.js';

const NARRATORS = {
  gemini: GeminiNarrator,
};

export function createNarrator(config) {
  const name = config.narrator?.provider || 'gemini';
  const NarratorClass = NARRATORS[name];
  if (!NarratorClass) {
    throw new Error(`Unknown narrator provider: "${name}". Available: ${Object.keys(NARRATORS).join(', ')}`);
  }
  return new NarratorClass(config);
}

export function listNarrators() {
  return Object.keys(NARRATORS);
}
