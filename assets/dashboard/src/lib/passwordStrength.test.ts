import { describe, it, expect } from 'vitest';
import { passwordStrength } from './passwordStrength';

describe('passwordStrength', () => {
  it('returns weak for short password (<8 chars)', () => {
    expect(passwordStrength('abc')).toBe('weak');
    expect(passwordStrength('1234567')).toBe('weak');
    expect(passwordStrength('a')).toBe('weak');
  });

  it('returns weak for empty string', () => {
    expect(passwordStrength('')).toBe('weak');
  });

  it('returns weak for all same character', () => {
    expect(passwordStrength('aaaaaaaa')).toBe('weak');
    expect(passwordStrength('zzzzzzzzzzz')).toBe('weak');
  });

  it('returns weak for sequential digits', () => {
    expect(passwordStrength('12345678')).toBe('weak');
    expect(passwordStrength('23456789')).toBe('weak');
  });

  it('returns strong for long password (>=12 chars, letters only)', () => {
    expect(passwordStrength('abcdefghijkl')).toBe('strong');
    expect(passwordStrength('longpasswordhere')).toBe('strong');
  });

  it('returns strong for mixed letters+digits (>=8 chars)', () => {
    expect(passwordStrength('abcd1234')).toBe('strong');
    expect(passwordStrength('pass99wo')).toBe('strong');
  });

  it('returns ok for letters only, 8-11 chars', () => {
    expect(passwordStrength('abcdefgh')).toBe('ok');
    expect(passwordStrength('helloworld!')).toBe('ok');
  });

  it('returns ok for digits only, 8+ chars, no weak pattern', () => {
    expect(passwordStrength('99887766')).toBe('ok');
  });

  it('returns ok for special chars only, 8 chars', () => {
    expect(passwordStrength('!@#$%^&*')).toBe('ok');
  });
});
