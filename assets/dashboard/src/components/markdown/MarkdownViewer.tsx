import { useMemo } from 'react';
import type { ImgHTMLAttributes } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getWorkspaceFileUrl } from '../../lib/api';
import { resolveRelativePath } from '../../lib/pathUtils';

export interface MarkdownViewerProps {
  workspaceId: string;
  filePath: string;
  content: string;
}

// MarkdownViewer is the read-only rendering the page used before the editor:
// remote workspaces and files the editor refuses (too large, not UTF-8).
export default function MarkdownViewer({ workspaceId, filePath, content }: MarkdownViewerProps) {
  const components = useMemo(
    () => ({
      img: ({ src, alt, ...rest }: ImgHTMLAttributes<HTMLImageElement>) => {
        if (typeof src !== 'string') {
          return <img src={src} alt={alt} {...rest} />;
        }
        const resolved = resolveRelativePath(src, filePath);
        const finalSrc = resolved === null ? src : getWorkspaceFileUrl(workspaceId, resolved);
        return <img src={finalSrc} alt={alt} {...rest} />;
      },
    }),
    [workspaceId, filePath]
  );
  return (
    <div className="markdown-preview-content" data-testid="markdown-viewer">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
