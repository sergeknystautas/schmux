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
 * Play an urgent alert sound for states that need immediate user attention.
 * Two-tone high-low pattern — distinct and interruptive.
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
 * Play a gentle completion chime for positive outcomes.
 * Rising two-note pattern — informational, not urgent.
 */
export async function playCompletionSound(): Promise<void> {
  try {
    await ensureAudioContextResumed();
    const ctx = getAudioContext();

    const now = ctx.currentTime;

    // First note (lower) — soft onset
    const osc1 = ctx.createOscillator();
    const gain1 = ctx.createGain();
    osc1.connect(gain1);
    gain1.connect(ctx.destination);
    osc1.type = 'sine';
    osc1.frequency.setValueAtTime(523, now); // C5
    gain1.gain.setValueAtTime(0, now);
    gain1.gain.linearRampToValueAtTime(0.15, now + 0.03);
    gain1.gain.linearRampToValueAtTime(0, now + 0.15);
    osc1.start(now);
    osc1.stop(now + 0.15);

    // Second note (higher) — rising = positive
    const osc2 = ctx.createOscillator();
    const gain2 = ctx.createGain();
    osc2.connect(gain2);
    gain2.connect(ctx.destination);
    osc2.type = 'sine';
    osc2.frequency.setValueAtTime(659, now + 0.12); // E5
    gain2.gain.setValueAtTime(0, now + 0.12);
    gain2.gain.linearRampToValueAtTime(0.15, now + 0.15);
    gain2.gain.linearRampToValueAtTime(0, now + 0.3);
    osc2.start(now + 0.12);
    osc2.stop(now + 0.3);
  } catch (e) {
    console.warn('Failed to play completion sound:', e);
  }
}

/**
 * States that need immediate user attention (urgent sound).
 */
const ATTENTION_STATES = new Set(['Needs Input', 'Needs Attention', 'Error']);

/**
 * States that indicate positive completion (gentle sound).
 */
const COMPLETION_STATES = new Set(['Completed']);

/**
 * Determine which sound to play for a nudge state, if any.
 */
export function soundForState(state: string | undefined): 'attention' | 'completion' | null {
  if (state === undefined) return null;
  if (ATTENTION_STATES.has(state)) return 'attention';
  if (COMPLETION_STATES.has(state)) return 'completion';
  return null;
}

/**
 * Play the appropriate sound for a nudge state.
 * Returns without playing if the state doesn't warrant a sound.
 */
async function playSoundForState(state: string | undefined): Promise<void> {
  const sound = soundForState(state);
  if (sound === 'attention') {
    await playAttentionSound();
  } else if (sound === 'completion') {
    await playCompletionSound();
  }
}

/**
 * Check if a nudge state should trigger an attention sound.
 * @deprecated Use soundForState() instead for proper sound differentiation.
 */
export function isAttentionState(state: string | undefined): boolean {
  return state !== undefined && ATTENTION_STATES.has(state);
}
