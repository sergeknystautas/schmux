# Fenced quick launch preset

A user wants to launch a fenced session from a saved preset, without
going through the spawn wizard.

They configure a global quick launch preset with `fence: true` from the
Settings > Sessions tab, then on the home page they click the workspace's
`+` button and pick the preset from the Quick Launch section.

Because the scenarios Docker image does not ship the `fence` binary (only
the e2e image does — see `Dockerfile.e2e:18`), the spawn surfaces the
`fence not available` error rather than running a fenced session. The
honest assertion is that the preset's `fence: true` reaches the server's
fence gate; with the gate unavailable, the spawn fails loudly.

## Preconditions

- The daemon is running with at least one repository configured
- The repository has at least one branch
- A quick launch preset named `fenced-build` exists in global config,
  with `command: "echo hello"` and `fence: true`

## Verifications

- The home page `+` dropdown lists the `fenced-build` preset under
  "Quick Launch"
- Clicking `fenced-build` triggers a spawn that fails with a message
  containing `fence not available`
- No fenced session is created on the machine without the fence binary
