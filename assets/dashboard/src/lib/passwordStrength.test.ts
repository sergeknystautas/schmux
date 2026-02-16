import { describe, it, expect } from 'vitest';
import { passwordStrength } from './passwordStrength';

describe('passwordStrength', () => {
  it.each([
    ['123456', 'weak'],
    ['111111', 'weak'],
    ['abcdefg', 'weak'], // <8 chars
    ['abcdefgh', 'ok'], // 8 chars, single type
    ['99887766', 'ok'], // 8 digits, no pattern
    ['a1b2c3d4', 'strong'], // 8 chars, mixed types
    ['mySecurePin99', 'strong'], // 12+ chars
  ] as const)('passwordStrength(%s) = %s', (password, expected) => {
    expect(passwordStrength(password)).toBe(expected);
  });
});
