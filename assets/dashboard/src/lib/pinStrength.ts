export type PinStrength = 'weak' | 'ok' | 'strong';

const WEAK_PATTERNS = [
  /^(.)\1+$/, // all same character
  /^(0123|1234|2345|3456|4567|5678|6789|7890)/, // sequential digits
];

export function pinStrength(pin: string): PinStrength {
  if (pin.length < 8) return 'weak';
  for (const pattern of WEAK_PATTERNS) {
    if (pattern.test(pin)) return 'weak';
  }
  const hasMixed = /[a-zA-Z]/.test(pin) && /[0-9]/.test(pin);
  if (pin.length >= 12 || hasMixed) return 'strong';
  return 'ok';
}
