import { useMemo } from 'react';
import { Editor } from '@bytemd/react';
import gfm from '@bytemd/plugin-gfm';
import 'bytemd/dist/index.css';
import { schmuxPlugin } from './bytemdSchmuxPlugin';
import styles from '../../styles/markdownEditor.module.css';

export interface MarkdownEditorProps {
  value: string;
  onChange: (text: string) => void;
  workspaceId: string;
  filePath: string;
  // Tests only; production always uses ByteMD's "auto".
  mode?: 'auto' | 'split' | 'tab';
}

// MarkdownEditor is the whole ByteMD surface. `value` is the reducer's draft:
// ByteMD only calls CodeMirror setValue when the prop differs from the
// editor's text, so a typed change is a no-op here and an incoming document
// is a replacement. mode="auto" is ByteMD's own layout rule: side-by-side
// with its Write-only/Preview-only toggles above 800 px, tabs below.
export default function MarkdownEditor({
  value,
  onChange,
  workspaceId,
  filePath,
  mode = 'auto',
}: MarkdownEditorProps) {
  const plugins = useMemo(
    () => [gfm(), schmuxPlugin(workspaceId, filePath)],
    [workspaceId, filePath]
  );
  return (
    <div className={styles.editor} data-testid="markdown-editor">
      <Editor value={value} plugins={plugins} mode={mode} onChange={onChange} />
    </div>
  );
}
