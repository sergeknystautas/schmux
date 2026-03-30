/**
 * Restore the pre-npx environment for the daemon process.
 *
 * When dev.sh runs `exec npx`, npm injects npm_config_*, npm_package_*,
 * npm_lifecycle_*, INIT_CWD, NODE, and modifies PATH. These vars break
 * npm commands inside spawned tmux sessions.
 *
 * dev.sh snapshots the pre-npx state into two env vars:
 * - SCHMUX_PRISTINE_PATH: PATH before npx prepended node_modules/.bin
 * - SCHMUX_PRISTINE_NPM_VARS: base64-encoded, NUL-delimited KEY=VALUE
 *   pairs of any npm_* vars that existed before npx
 *
 * This function strips the npx pollution and restores the originals.
 * When the snapshots are absent (not running via dev.sh), returns
 * process.env unchanged.
 */
export function cleanEnv(): Record<string, string> {
  const pristinePath = process.env.SCHMUX_PRISTINE_PATH;
  const pristineNpmB64 = process.env.SCHMUX_PRISTINE_NPM_VARS;

  // Not running via dev.sh — no snapshots, nothing to clean.
  if (pristinePath === undefined && pristineNpmB64 === undefined) {
    return { ...process.env } as Record<string, string>;
  }

  const env = { ...process.env } as Record<string, string>;

  // 1. Strip all npm_* vars (npx injected or overwrote these)
  for (const key of Object.keys(env)) {
    if (key.startsWith('npm_')) {
      delete env[key];
    }
  }

  // 2. Restore npm_* vars that existed before npx
  if (pristineNpmB64) {
    const decoded = Buffer.from(pristineNpmB64, 'base64').toString();
    // NUL-delimited KEY=VALUE pairs from `env -0 | grep -z '^npm_'`
    for (const entry of decoded.split('\0')) {
      if (!entry) continue;
      const eqIdx = entry.indexOf('=');
      if (eqIdx === -1) continue;
      const key = entry.substring(0, eqIdx);
      const value = entry.substring(eqIdx + 1);
      env[key] = value;
    }
  }

  // 3. Strip vars npx adds that never exist in a clean shell
  delete env.INIT_CWD;
  delete env.NODE;

  // 4. Restore PATH from snapshot
  if (pristinePath !== undefined) {
    env.PATH = pristinePath;
  }

  // 5. Remove our own meta-vars
  delete env.SCHMUX_PRISTINE_PATH;
  delete env.SCHMUX_PRISTINE_NPM_VARS;

  return env;
}
