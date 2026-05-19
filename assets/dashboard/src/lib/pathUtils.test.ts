import { describe, it, expect } from 'vitest';
import { resolveRelativePath, rewriteHtmlRelativePaths } from './pathUtils';

describe('resolveRelativePath', () => {
  it('resolves sibling path against the reference file directory', () => {
    expect(resolveRelativePath('watercolor.png', 'docs/mood/README.md')).toBe(
      'docs/mood/watercolor.png'
    );
  });

  it('resolves parent-relative paths', () => {
    expect(resolveRelativePath('../a.png', 'docs/sub/guide.md')).toBe('docs/a.png');
  });

  it('treats leading slash as workspace-root-absolute', () => {
    expect(resolveRelativePath('/a.png', 'docs/README.md')).toBe('a.png');
  });

  it('collapses ./ segments', () => {
    expect(resolveRelativePath('./a.png', 'docs/README.md')).toBe('docs/a.png');
  });

  it('returns null for http URLs', () => {
    expect(resolveRelativePath('https://example.com/a.png', 'docs/README.md')).toBeNull();
  });

  it('returns null for data URLs', () => {
    expect(resolveRelativePath('data:image/png;base64,AAA', 'docs/README.md')).toBeNull();
  });

  it('returns null for protocol-relative URLs', () => {
    expect(resolveRelativePath('//cdn.example.com/a.png', 'docs/README.md')).toBeNull();
  });

  it('resolves when reference file is at the workspace root', () => {
    expect(resolveRelativePath('a.png', 'README.md')).toBe('a.png');
  });
});

describe('rewriteHtmlRelativePaths', () => {
  it('rewrites relative img src to workspace file API', () => {
    const html = '<html><body><img src="logo.png"></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'docs/index.html');
    expect(result).toContain('src="/api/file/ws-1/docs%2Flogo.png"');
  });

  it('rewrites relative link href to workspace file API', () => {
    const html = '<html><head><link rel="stylesheet" href="style.css"></head><body></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'docs/index.html');
    expect(result).toContain('href="/api/file/ws-1/docs%2Fstyle.css"');
  });

  it('leaves external URLs unchanged', () => {
    const html = '<html><body><img src="https://example.com/pic.png"></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'index.html');
    expect(result).toContain('src="https://example.com/pic.png"');
  });

  it('leaves data URLs unchanged', () => {
    const html = '<html><body><img src="data:image/png;base64,AAA"></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'index.html');
    expect(result).toContain('src="data:image/png;base64,AAA"');
  });

  it('rewrites parent-relative paths', () => {
    const html = '<html><body><img src="../images/pic.png"></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'docs/sub/index.html');
    expect(result).toContain('src="/api/file/ws-1/docs%2Fimages%2Fpic.png"');
  });

  it('rewrites CSS url() in style blocks', () => {
    const html =
      '<html><head><style>body { background: url("bg.png"); }</style></head><body></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'docs/index.html');
    expect(result).toContain("url('/api/file/ws-1/docs%2Fbg.png')");
  });

  it('rewrites CSS url() in inline styles', () => {
    const html = '<html><body><div style="background: url(\'icon.png\')"></div></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'index.html');
    expect(result).toContain("url('/api/file/ws-1/icon.png')");
  });

  it('does not rewrite anchor href (non-asset links)', () => {
    const html = '<html><body><a href="other.html">link</a></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'index.html');
    expect(result).toContain('href="other.html"');
  });

  it('leaves script src unchanged', () => {
    const html = '<html><body><script src="app.js"></script></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'index.html');
    expect(result).toContain('src="app.js"');
  });

  it('rewrites unquoted CSS url() values', () => {
    const html =
      '<html><head><style>body { background: url(bg.png); }</style></head><body></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'docs/index.html');
    expect(result).toContain("url('/api/file/ws-1/docs%2Fbg.png')");
  });

  it('leaves srcset attributes unchanged', () => {
    const html =
      '<html><body><picture><source srcset="img-300.webp 300w, img-600.webp 600w"><img src="img.png"></picture></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'index.html');
    expect(result).toContain('srcset="img-300.webp 300w, img-600.webp 600w"');
    expect(result).toContain('src="/api/file/ws-1/img.png"');
  });

  it('handles workspace-root-absolute paths', () => {
    const html = '<html><body><img src="/assets/logo.png"></body></html>';
    const result = rewriteHtmlRelativePaths(html, 'ws-1', 'docs/index.html');
    expect(result).toContain('src="/api/file/ws-1/assets%2Flogo.png"');
  });
});
