const CSRF_COOKIE_NAME = 'schmux_csrf';

/**
 * Reads the CSRF token from the schmux_csrf cookie.
 * Returns empty string if the cookie is not set.
 */
export function getCSRFToken(): string {
  const match = document.cookie.split('; ').find((row) => row.startsWith(CSRF_COOKIE_NAME + '='));
  if (!match) return '';
  return match.split('=').slice(1).join('=') || '';
}

/**
 * Returns headers object with X-CSRF-Token set if the CSRF cookie exists.
 * Merge this into your fetch headers for CSRF-protected endpoints.
 */
export function csrfHeaders(): Record<string, string> {
  const token = getCSRFToken();
  if (!token) return {};
  return { 'X-CSRF-Token': token };
}
