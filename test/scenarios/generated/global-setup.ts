/**
 * Global setup is no longer needed — each Playwright worker starts its own
 * isolated daemon via the worker-scoped fixture in fixtures.ts.
 * This file is kept as a no-op for backward compatibility.
 */
export default async function globalSetup(): Promise<void> {
  // No-op: per-worker daemon fixtures handle setup/teardown.
}
