// Notification sound utility for nudge state changes

let audioContext: AudioContext | null = null;

function getAudioContext(): AudioContext {
  if (!audioContext) {
    audioContext = new AudioContext();
  }
  return audioContext;
}

let warmupRegistered = false;

/**
 * Registers a one-time user interaction listener to pre-warm the AudioContext.
 * Browsers suspend AudioContext until a user gesture (click, keydown, etc.).
 * Without this, the first notification sound may be silently lost.
 */
export function warmupAudioContext(): void {
  if (warmupRegistered) return;
  warmupRegistered = true;

  const resume = () => {
    const ctx = getAudioContext();
    if (ctx.state === 'suspended') {
      ctx.resume();
    }
    // Remove all listeners after first interaction
    document.removeEventListener('click', resume);
    document.removeEventListener('keydown', resume);
    document.removeEventListener('touchstart', resume);
  };

  document.addEventListener('click', resume, { once: true });
  document.addEventListener('keydown', resume, { once: true });
  document.addEventListener('touchstart', resume, { once: true });
}

async function ensureAudioContextResumed(): Promise<void> {
  const ctx = getAudioContext();
  if (ctx.state === 'suspended') {
    await ctx.resume();
  }
}

/**
 * Play a notification sound for attention-required states.
 * Uses a two-tone alert sound that's distinct from the terminal bell.
 */
export async function playAttentionSound(): Promise<void> {
  try {
    await ensureAudioContextResumed();
    const ctx = getAudioContext();

    // Two-tone alert: high-low pattern
    const now = ctx.currentTime;

    // First tone (higher pitch)
    const osc1 = ctx.createOscillator();
    const gain1 = ctx.createGain();
    osc1.connect(gain1);
    gain1.connect(ctx.destination);
    osc1.type = 'sine';
    osc1.frequency.setValueAtTime(880, now); // A5
    gain1.gain.setValueAtTime(0, now);
    gain1.gain.linearRampToValueAtTime(0.25, now + 0.02);
    gain1.gain.linearRampToValueAtTime(0, now + 0.15);
    osc1.start(now);
    osc1.stop(now + 0.15);

    // Second tone (lower pitch) - slight delay
    const osc2 = ctx.createOscillator();
    const gain2 = ctx.createGain();
    osc2.connect(gain2);
    gain2.connect(ctx.destination);
    osc2.type = 'sine';
    osc2.frequency.setValueAtTime(660, now + 0.12); // E5
    gain2.gain.setValueAtTime(0, now + 0.12);
    gain2.gain.linearRampToValueAtTime(0.25, now + 0.14);
    gain2.gain.linearRampToValueAtTime(0, now + 0.3);
    osc2.start(now + 0.12);
    osc2.stop(now + 0.3);
  } catch (e) {
    // Silently fail if audio is not available
    console.warn('Failed to play notification sound:', e);
  }
}

/**
 * States that should trigger an attention sound.
 */
export const ATTENTION_STATES = new Set(['Needs Authorization', 'Error']);

/**
 * Check if a nudge state should trigger an attention sound.
 */
export function isAttentionState(state: string | undefined): boolean {
  return state !== undefined && ATTENTION_STATES.has(state);
}
