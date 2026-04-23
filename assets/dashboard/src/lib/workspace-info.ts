import type { WorkspaceResponse } from './types';

export type WorkspaceInfoRow =
  | { kind: 'text'; value: string; small?: boolean }
  | { kind: 'commits'; behind: number; ahead: number };

export function buildWorkspaceInfoRows(workspace: WorkspaceResponse): WorkspaceInfoRow[] {
  const rows: WorkspaceInfoRow[] = [];

  rows.push({ kind: 'text', value: workspace.branch });

  const name =
    typeof workspace.label === 'string' && workspace.label.length > 0
      ? workspace.label
      : workspace.id;
  rows.push({ kind: 'text', value: name });

  rows.push({ kind: 'text', value: workspace.repo, small: true });

  const isGit = !workspace.vcs || workspace.vcs === 'git';
  if (isGit) {
    const ahead = workspace.ahead ?? 0;
    const behind = workspace.behind ?? 0;
    rows.push({ kind: 'commits', behind, ahead });
  }

  return rows;
}
