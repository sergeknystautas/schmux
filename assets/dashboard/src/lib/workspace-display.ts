/**
 * Minimal workspace shape consumed by `workspaceDisplayLabel`. Both
 * `WorkspaceResponse` (manual, in `types.ts`) and `WorkspaceResponseItem`
 * (generated, in `types.generated.ts`) satisfy this structurally.
 */
interface WorkspaceDisplayInput {
  id: string;
  branch: string;
  label?: string;
}

/**
 * Resolve the display string for a workspace.
 *
 * Fallback chain:
 *   1. `ws.label` (if non-empty after trim)
 *   2. `computedBranch` (caller-supplied; lets remote-aware logic compose)
 *   3. `ws.branch` (raw)
 *   4. `ws.id` (the on-disk workspace ID — final fallback for sapling)
 */
export function workspaceDisplayLabel(ws: WorkspaceDisplayInput, computedBranch?: string): string {
  const label = ws.label?.trim();
  if (label) return label;
  if (computedBranch) return computedBranch;
  if (ws.branch) return ws.branch;
  return ws.id;
}
