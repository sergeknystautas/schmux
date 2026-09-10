import { describe, expect, it } from 'vitest';
import { getWorkspaceFileJumpUrl, rewriteWorkspaceFileHref } from './fileNavigation';

describe('workspace file navigation', () => {
  it('builds one content-agnostic jump URL', () => {
    expect(getWorkspaceFileJumpUrl('bach-godot-003', 'docs/live ops/steam.md')).toBe(
      '/jump/bach-godot-003/docs%2Flive%20ops%2Fsteam.md'
    );
  });

  it('rewrites an absolute path under the current workspace', () => {
    expect(
      rewriteWorkspaceFileHref(
        '/Users/dev/workspaces/ws-1/docs/readme.md',
        'ws-1',
        '/Users/dev/workspaces/ws-1'
      )
    ).toBe('/jump/ws-1/docs%2Freadme.md');
  });

  it('accepts encoded file URLs and removes source-location suffixes', () => {
    expect(
      rewriteWorkspaceFileHref(
        'file:///Users/dev/workspaces/ws-1/docs/live%20ops/readme.md:12:4',
        'ws-1',
        '/Users/dev/workspaces/ws-1/'
      )
    ).toBe('/jump/ws-1/docs%2Flive%20ops%2Freadme.md');
  });

  it.each([
    'https://example.com/readme.md',
    '/Users/dev/workspaces/another/readme.md',
    'docs/readme.md',
    '#usage',
  ])('leaves non-workspace link %s untouched', (href) => {
    expect(rewriteWorkspaceFileHref(href, 'ws-1', '/Users/dev/workspaces/ws-1')).toBe(href);
  });
});
