import { describe, expect, it } from 'vitest';
import { getWorkspaceFileJumpUrl, resolveWorkspaceFileLink } from './fileNavigation';

describe('workspace file navigation', () => {
  it('builds one content-agnostic jump URL', () => {
    expect(getWorkspaceFileJumpUrl('bach-godot-003', 'docs/live ops/steam.md')).toBe(
      '/jump/bach-godot-003/docs%2Flive%20ops%2Fsteam.md'
    );
  });

  it('rewrites an absolute path under the current workspace', () => {
    expect(
      resolveWorkspaceFileLink(
        '/Users/dev/workspaces/ws-1/docs/readme.md',
        'ws-1',
        '/Users/dev/workspaces/ws-1'
      )
    ).toEqual({
      filePath: 'docs/readme.md',
      href: '/jump/ws-1/docs%2Freadme.md',
    });
  });

  it('accepts encoded file URLs and removes source-location suffixes', () => {
    expect(
      resolveWorkspaceFileLink(
        'file:///Users/dev/workspaces/ws-1/docs/live%20ops/readme.md:12:4',
        'ws-1',
        '/Users/dev/workspaces/ws-1/'
      )
    ).toEqual({
      filePath: 'docs/live ops/readme.md',
      href: '/jump/ws-1/docs%2Flive%20ops%2Freadme.md',
    });
  });

  it.each([
    'https://example.com/readme.md',
    '/Users/dev/workspaces/another/readme.md',
    'docs/readme.md',
    '#usage',
  ])('does not resolve non-workspace link %s', (href) => {
    expect(resolveWorkspaceFileLink(href, 'ws-1', '/Users/dev/workspaces/ws-1')).toBeUndefined();
  });
});
