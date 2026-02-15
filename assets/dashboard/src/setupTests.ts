import '@testing-library/jest-dom';

// jsdom v28+ with Node's built-in localStorage doesn't expose Storage API methods.
// Provide a spec-compliant localStorage/sessionStorage for tests.
function createStorage(): Storage {
  let store: Record<string, string> = {};
  return {
    getItem(key: string) {
      return key in store ? store[key] : null;
    },
    setItem(key: string, value: string) {
      store[key] = String(value);
    },
    removeItem(key: string) {
      delete store[key];
    },
    clear() {
      store = {};
    },
    get length() {
      return Object.keys(store).length;
    },
    key(index: number) {
      return Object.keys(store)[index] ?? null;
    },
  };
}

Object.defineProperty(window, 'localStorage', { value: createStorage() });
Object.defineProperty(window, 'sessionStorage', { value: createStorage() });

// jsdom doesn't implement matchMedia. Provide a stub that defaults to no match.
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});
