import { test, expect } from './coverage-fixture';
import {
  createTestRepo,
  apiPost,
  apiGet,
  waitForHealthy,
  waitForTerminalOutput,
  sleep,
} from './helpers';
import { execSync } from 'child_process';

const BASE_URL = 'http://localhost:7337';
const SCHMUX_HOME = process.env.SCHMUX_HOME || `${process.env.HOME}/.schmux`;

test.describe('Remote clipboard image paste', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-remote-clipboard');

    // Write remote_flavors directly into the config file (not exposed in the
    // config update API). Then trigger a seedConfig API call which reloads the
    // config from disk, picking up the remote_flavors.
    const { writeFileSync, readFileSync } = await import('fs');
    execSync(`mkdir -p ${SCHMUX_HOME}`);
    let existingConfig: Record<string, unknown> = {};
    try {
      existingConfig = JSON.parse(readFileSync(`${SCHMUX_HOME}/config.json`, 'utf-8'));
    } catch {
      // File may not exist yet
    }
    existingConfig.remote_flavors = [
      {
        id: 'clipboard-test',
        flavor: 'mock:clipboard',
        display_name: 'Clipboard Test Host',
        workspace_path: '/tmp/test-workspace',
        vcs: 'git',
        connect_command: '/app/test/mock-remote.sh',
      },
    ];
    writeFileSync(`${SCHMUX_HOME}/config.json`, JSON.stringify(existingConfig, null, 2));

    // seedConfig triggers a config API POST which calls config.Reload(),
    // loading our remote_flavors from disk into the live config.
    const { seedConfig } = await import('./helpers');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'clipboard-agent',
          command: '/app/test/mock-clipboard-agent.sh',
        },
      ],
    });
  });

  test('image paste round-trip: browser to remote clipboard to agent', async () => {
    // Connect to the remote host
    const connectResp = await fetch(`${BASE_URL}/api/remote/hosts/connect`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ flavor_id: 'clipboard-test' }),
    });
    if (!connectResp.ok) {
      const errBody = await connectResp.text();
      throw new Error(`Connect failed (${connectResp.status}): ${errBody}`);
    }

    // Wait for connection to be established
    let connected = false;
    for (let i = 0; i < 30; i++) {
      await sleep(1000);
      const hosts =
        await apiGet<Array<{ id: string; flavor_id: string; status: string }>>('/api/remote/hosts');
      const host = hosts.find((h) => h.flavor_id === 'clipboard-test');
      if (host?.status === 'connected') {
        connected = true;
        break;
      }
    }
    expect(connected).toBe(true);

    // Spawn a remote session with the clipboard agent
    const spawnResp = await apiPost<Array<{ session_id: string }>>('/api/spawn', {
      remote_flavor_id: 'clipboard-test',
      repo: repoPath,
      targets: { 'clipboard-agent': 1 },
    });
    expect(spawnResp.length).toBeGreaterThan(0);
    const sessionId = spawnResp[0].session_id;
    expect(sessionId).toBeTruthy();

    // Wait for the session to be running
    let sessionReady = false;
    for (let i = 0; i < 30; i++) {
      await sleep(1000);
      const sessions = await apiGet<
        Array<{
          id: string;
          sessions: Array<{ id: string; running: boolean }>;
        }>
      >('/api/sessions');
      const allSessions = sessions.flatMap((ws) => ws.sessions);
      const sess = allSessions.find((s) => s.id === sessionId);
      if (sess?.running) {
        sessionReady = true;
        break;
      }
    }
    expect(sessionReady).toBe(true);

    // Wait for the agent to print "agent ready" (confirms it's accepting input)
    await waitForTerminalOutput(sessionId, 'agent ready', 15_000);

    // Create a small test image (1x1 red pixel PNG, 67 bytes)
    const testPng = new Uint8Array([
      0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44,
      0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90,
      0x77, 0x53, 0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8,
      0xcf, 0xc0, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33, 0x00, 0x00, 0x00,
      0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
    ]);

    // Base64-encode the PNG
    let binary = '';
    for (let i = 0; i < testPng.length; i++) {
      binary += String.fromCharCode(testPng[i]);
    }
    const imageBase64 = btoa(binary);

    // POST the image to the clipboard-paste endpoint
    const pasteResp = await fetch(`${BASE_URL}/api/clipboard-paste`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        sessionId: sessionId,
        imageBase64: imageBase64,
      }),
    });

    // Verify the API call succeeded
    const pasteBody = await pasteResp.json();
    expect(pasteResp.status).toBe(200);
    expect(pasteBody.status).toBe('ok');

    // Verify the agent received the image from the clipboard.
    // The mock-clipboard-agent.sh reads the X11 clipboard via xclip when it
    // gets Ctrl+V and prints "clipboard-image-received:<size>bytes".
    // The test PNG is 67 bytes; xclip -o | wc -c may report 67-69 depending
    // on trailing newline handling, so just verify a non-zero size was received.
    const output = await waitForTerminalOutput(sessionId, 'clipboard-image-received:', 15_000);
    // Extract the byte count and verify the image data was received (non-zero)
    const match = output.match(/clipboard-image-received:(\d+)bytes/);
    expect(match).not.toBeNull();
    const receivedBytes = parseInt(match![1], 10);
    expect(receivedBytes).toBeGreaterThanOrEqual(67);
    expect(receivedBytes).toBeLessThanOrEqual(69);
  });
});
