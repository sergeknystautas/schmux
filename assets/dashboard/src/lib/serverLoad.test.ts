import { getServerLoad, getServerLoadVersion, updateServerLoad } from './serverLoad';

describe('serverLoad store', () => {
  it('stores the value and bumps the version', () => {
    const v0 = getServerLoadVersion();
    updateServerLoad({ one: 2.22, five: 3.18, fifteen: 3.28 });
    expect(getServerLoadVersion()).toBe(v0 + 1);
    expect(getServerLoad()).toEqual({ one: 2.22, five: 3.18, fifteen: 3.28 });
  });

  it('null clears the value and bumps the version', () => {
    updateServerLoad({ one: 1, five: 2, fifteen: 3 });
    const v0 = getServerLoadVersion();
    updateServerLoad(null);
    expect(getServerLoadVersion()).toBe(v0 + 1);
    expect(getServerLoad()).toBeNull();
  });
});
