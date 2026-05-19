export function resolveRelativePath(src: string, referenceFilePath: string): string | null {
  if (/^([a-z][a-z0-9+.-]*:|\/\/)/i.test(src)) return null;
  const parts = src.startsWith('/')
    ? src.slice(1).split('/')
    : [...referenceFilePath.split('/').slice(0, -1), ...src.split('/')];
  const stack: string[] = [];
  for (const p of parts) {
    if (p === '..') stack.pop();
    else if (p !== '.' && p !== '') stack.push(p);
  }
  return stack.join('/');
}

function rewriteCssUrls(css: string, workspaceId: string, htmlFilePath: string): string {
  return css.replace(/url\(\s*(['"]?)([^)'"]+)\1\s*\)/g, (_match, quote, rawUrl) => {
    const resolved = resolveRelativePath(rawUrl, htmlFilePath);
    if (resolved === null) return _match;
    const apiUrl = `/api/file/${workspaceId}/${encodeURIComponent(resolved)}`;
    return `url('${apiUrl}')`;
  });
}

const ASSET_LINK_SELECTOR = [
  'img[src]',
  'link[rel="stylesheet"][href]',
  'link[rel="icon"][href]',
  'source[src]',
  'video[src]',
  'video[poster]',
  'audio[src]',
].join(', ');

export function rewriteHtmlRelativePaths(
  html: string,
  workspaceId: string,
  htmlFilePath: string
): string {
  const parser = new DOMParser();
  const doc = parser.parseFromString(html, 'text/html');

  for (const el of doc.querySelectorAll(ASSET_LINK_SELECTOR)) {
    for (const attr of ['src', 'href', 'poster']) {
      const val = el.getAttribute(attr);
      if (!val) continue;
      const resolved = resolveRelativePath(val, htmlFilePath);
      if (resolved === null) continue;
      el.setAttribute(attr, `/api/file/${workspaceId}/${encodeURIComponent(resolved)}`);
    }
  }

  for (const style of doc.querySelectorAll('style')) {
    if (style.textContent) {
      style.textContent = rewriteCssUrls(style.textContent, workspaceId, htmlFilePath);
    }
  }

  for (const el of doc.querySelectorAll('[style]')) {
    const styleVal = el.getAttribute('style');
    if (styleVal && /url\(/.test(styleVal)) {
      el.setAttribute('style', rewriteCssUrls(styleVal, workspaceId, htmlFilePath));
    }
  }

  return new XMLSerializer().serializeToString(doc);
}
