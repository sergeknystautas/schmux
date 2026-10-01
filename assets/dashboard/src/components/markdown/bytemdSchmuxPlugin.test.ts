import { describe, it, expect } from 'vitest';
import { mapCursorOffset, rewriteRelativeImages } from './bytemdSchmuxPlugin';

describe('mapCursorOffset', () => {
  it('keeps an offset inside the unchanged prefix', () => {
    expect(mapCursorOffset('hello world', 'hello brave world', 3)).toBe(3);
  });
  it('shifts an offset that sits past the change by the length delta', () => {
    expect(mapCursorOffset('hello world', 'hello brave world', 9)).toBe(15);
  });
  it('clamps an offset that fell inside a removed region', () => {
    expect(mapCursorOffset('abcdefgh', 'abgh', 5)).toBe(2);
  });
  it('clamps to the new length', () => {
    expect(mapCursorOffset('abcdef', 'ab', 6)).toBe(2);
  });
});

describe('rewriteRelativeImages', () => {
  const img = (src: string) => ({
    type: 'element',
    tagName: 'img',
    properties: { src },
    children: [],
  });
  it('rewrites relative sources to the authenticated file route', () => {
    const tree = { type: 'root', children: [img('./pics/a.png'), img('../b.png')] };
    rewriteRelativeImages(tree as never, 'ws-1', 'docs/readme.md');
    expect(tree.children.map((c) => c.properties.src)).toEqual([
      '/api/file/ws-1/docs%2Fpics%2Fa.png',
      '/api/file/ws-1/b.png',
    ]);
  });
  it('treats a leading slash as workspace-root, like the read-only viewer does', () => {
    const tree = { type: 'root', children: [img('/assets/logo.png')] };
    rewriteRelativeImages(tree as never, 'ws-1', 'docs/readme.md');
    expect(tree.children.map((c) => c.properties.src)).toEqual([
      '/api/file/ws-1/assets%2Flogo.png',
    ]);
  });
  it('leaves absolute, data, and escaping sources alone', () => {
    const tree = {
      type: 'root',
      children: [
        img('https://x/y.png'),
        img('data:image/png;base64,AA=='),
        img('../../../etc/x.png'),
      ],
    };
    rewriteRelativeImages(tree as never, 'ws-1', 'docs/readme.md');
    expect(tree.children.map((c) => c.properties.src)).toEqual([
      'https://x/y.png',
      'data:image/png;base64,AA==',
      '../../../etc/x.png',
    ]);
  });
});
