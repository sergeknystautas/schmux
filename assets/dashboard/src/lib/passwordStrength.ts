type PasswordStrength = 'weak' | 'ok' | 'strong';

const WEAK_PATTERNS = [
  /^(.)\1+$/, // all same character
  /^(0123|1234|2345|3456|4567|5678|6789|7890)/, // sequential digits
];

export function passwordStrength(password: string): PasswordStrength {
  if (password.length < 8) return 'weak';
  for (const pattern of WEAK_PATTERNS) {
    if (pattern.test(password)) return 'weak';
  }
  const hasMixed = /[a-zA-Z]/.test(password) && /[0-9]/.test(password);
  if (password.length >= 12 || hasMixed) return 'strong';
  return 'ok';
}
