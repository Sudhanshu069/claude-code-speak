/**
 * Base TTS provider interface.
 * All providers must implement synthesize(text) → Promise<Buffer>
 */
export class BaseTTSProvider {
  constructor(config) {
    this.config = config;
  }

  /**
   * Convert text to audio buffer.
   * @param {string} text - The text to synthesize
   * @returns {Promise<{audio: Buffer, format: string}>} Audio data and format info
   */
  async synthesize(text) {
    throw new Error('synthesize() must be implemented by provider');
  }

  /**
   * Validate credentials/connectivity.
   * @returns {Promise<{ok: boolean, error?: string}>}
   */
  async validate() {
    throw new Error('validate() must be implemented by provider');
  }

  /**
   * Get the file extension for the audio format this provider returns.
   */
  get audioExtension() {
    return 'wav';
  }
}
