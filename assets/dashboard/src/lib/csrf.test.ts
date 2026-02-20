import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { getCSRFToken, csrfHeaders } from './csrf';

describe('getCSRFToken', () => {
  beforeEach(() => {
    // Clear cookies before each test
    document.cookie = 'schmux_csrf=; Max-Age=0';
  });

  afterEach(() => {
    document.cookie = 'schmux_csrf=; Max-Age=0';
  });

  it('returns the CSRF token from the schmux_csrf cookie', () => {
    document.cookie = 'schmux_csrf=test-csrf-token-123';
    expect(getCSRFToken()).toBe('test-csrf-token-123');
  });

  it('returns empty string when no CSRF cookie exists', () => {
    expect(getCSRFToken()).toBe('');
  });

  it('returns empty string when cookie value is empty', () => {
    document.cookie = 'schmux_csrf=';
    expect(getCSRFToken()).toBe('');
  });

  it('handles cookie with special characters (base64)', () => {
    // randomToken(32) produces base64-encoded values which may contain + / =
    const base64Token = 'abc+def/ghi=jkl';
    document.cookie = `schmux_csrf=${base64Token}`;
    expect(getCSRFToken()).toBe(base64Token);
  });

  it('picks the right cookie when multiple cookies exist', () => {
    document.cookie = 'schmux_auth=some-auth-value';
    document.cookie = 'schmux_csrf=the-csrf-token';
    document.cookie = 'other_cookie=other-value';
    expect(getCSRFToken()).toBe('the-csrf-token');
  });
});

describe('csrfHeaders', () => {
  beforeEach(() => {
    document.cookie = 'schmux_csrf=; Max-Age=0';
  });

  afterEach(() => {
    document.cookie = 'schmux_csrf=; Max-Age=0';
  });

  it('returns headers with X-CSRF-Token when cookie exists', () => {
    document.cookie = 'schmux_csrf=my-token';
    const headers = csrfHeaders();
    expect(headers['X-CSRF-Token']).toBe('my-token');
  });

  it('returns empty headers when no CSRF cookie', () => {
    const headers = csrfHeaders();
    expect(headers['X-CSRF-Token']).toBeUndefined();
  });
});
