/** Stable, content-agnostic URL for a file inside a workspace. */
export function getWorkspaceFileJumpUrl(workspaceId: string, filePath: string): string {
  return `/jump/${encodeURIComponent(workspaceId)}/${encodeURIComponent(filePath)}`;
}

export interface WorkspaceFileLinkTarget {
  filePath: string;
  href: string;
}

/**
 * Turn an agent-emitted absolute workspace path into a stable schmux URL.
 * Relative and non-file links are left untouched.
 */
export function resolveWorkspaceFileLink(
  href: string | undefined,
  workspaceId: string | undefined,
  workspacePath: string | undefined
): WorkspaceFileLinkTarget | undefined {
  if (!href || !workspaceId || !workspacePath) return undefined;

  let candidate = href;
  if (candidate.startsWith('file://')) {
    try {
      candidate = new URL(candidate).pathname;
    } catch {
      return undefined;
    }
  }

  try {
    candidate = decodeURIComponent(candidate);
  } catch {
    return undefined;
  }

  // Codex file citations may carry a line or line+column suffix.
  candidate = candidate.replace(/:\d+(?::\d+)?$/, '');

  const root = workspacePath.replace(/\/+$/, '');
  const prefix = `${root}/`;
  if (!candidate.startsWith(prefix)) return undefined;

  const relativePath = candidate.slice(prefix.length);
  if (!relativePath || relativePath.split('/').includes('..')) return undefined;
  return {
    filePath: relativePath,
    href: getWorkspaceFileJumpUrl(workspaceId, relativePath),
  };
}
