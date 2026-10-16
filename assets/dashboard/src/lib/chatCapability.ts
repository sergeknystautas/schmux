import type { Model, RunnerInfo } from './types';

// targetSupportsChat reports whether a spawn target resolves to an available
// runner that declares the chat capability. A model resolves to its first
// available runner; a bare tool name resolves to itself.
export function targetSupportsChat(
  target: string,
  models: Model[] | undefined,
  runners: Record<string, RunnerInfo> | undefined
): boolean {
  const model = models?.find((m) => m.id === target);
  const runner = model ? model.runners.find((r) => runners?.[r]?.available) : target;
  return runner !== undefined && (runners?.[runner]?.capabilities ?? []).includes('chat');
}
