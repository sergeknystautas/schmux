# GitHub Auth CLI v2 Spec

**Goal**
Redesign `schmux auth github` to guide users through GitHub OAuth setup with clear explanations, prerequisite checking, and optional certificate generation. The CLI should be approachable for users who don't understand OAuth/TLS, while still being fast for users who know what they're doing.

**Key Changes from v1**

- Explain the overall setup before asking questions
- Check prerequisites before collecting values
- Offer to generate TLS certificates (via mkcert)
- Use ANSI colors for visual hierarchy
- Re-running uses existing config values as defaults

---

## Visual Design

### Style Guidelines

- **Not a TUI**: Sequential questions, not ncurses-style full-screen interface
- **ANSI colors** for clarity (see color reference below)
- **Section dividers**: Use box-drawing characters (`━`) for visual separation
- **Consistent prompt format**: `Label [default]: ` with dim default
- **Graceful degradation**: Check if terminal supports colors; fall back to plain text if not

### Color Reference

| Element            | Color       | ANSI Code    | Example                                  |
| ------------------ | ----------- | ------------ | ---------------------------------------- |
| Section header bar | Cyan        | `\033[36m`   | `━━━━━━━━━━━━━━`                         |
| Section title      | Cyan + Bold | `\033[1;36m` | `GitHub Authentication Setup`            |
| Step title         | Cyan + Bold | `\033[1;36m` | `Step 1: Hostname`                       |
| Explanatory text   | Dim         | `\033[2m`    | `This will be the URL you type...`       |
| Prompt label       | Bold        | `\033[1m`    | `Hostname`                               |
| Default value      | Dim         | `\033[2m`    | `[schmux.local]`                         |
| User input         | Normal      | (none)       | User's typed text                        |
| Important values   | Bold        | `\033[1m`    | `https://schmux.local`                   |
| Success/checkmark  | Green       | `\033[32m`   | `✓ Certificates saved`                   |
| Warning            | Yellow      | `\033[33m`   | `⚠ Certificate not found`                |
| Error              | Red         | `\033[31m`   | `✗ mkcert failed`                        |
| URLs/paths         | Cyan        | `\033[36m`   | `https://github.com/settings/developers` |
| Code/commands      | Cyan        | `\033[36m`   | `brew install mkcert`                    |

Reset code: `\033[0m`

### Helper Functions (Implementation)

```go
const (
    reset   = "\033[0m"
    bold    = "\033[1m"
    dim     = "\033[2m"
    red     = "\033[31m"
    green   = "\033[32m"
    yellow  = "\033[33m"
    cyan    = "\033[36m"
)

func header(title string) {
    bar := strings.Repeat("━", 72)
    fmt.Printf("%s%s%s\n", cyan, bar, reset)
    fmt.Printf("%s%s  %s%s\n", bold, cyan, title, reset)
    fmt.Printf("%s%s%s\n\n", cyan, bar, reset)
}

func success(msg string) {
    fmt.Printf("%s✓ %s%s\n", green, msg, reset)
}

func warn(msg string) {
    fmt.Printf("%s⚠ %s%s\n", yellow, msg, reset)
}

func errMsg(msg string) {
    fmt.Printf("%s✗ %s%s\n", red, msg, reset)
}

func dimText(msg string) string {
    return dim + msg + reset
}

func prompt(label, defaultVal string) {
    if defaultVal != "" {
        fmt.Printf("%s%s%s %s[%s]%s: ", bold, label, reset, dim, defaultVal, reset)
    } else {
        fmt.Printf("%s%s%s: ", bold, label, reset)
    }
}
```

### Terminal Detection

Only use colors if stdout is a terminal:

```go
import "golang.org/x/term"

var useColors = term.IsTerminal(int(os.Stdout.Fd()))

func colorize(code, text string) string {
    if !useColors {
        return text
    }
    return code + text + reset
}
```

### Implementation: charmbracelet/huh

The CLI uses the [charmbracelet/huh](https://github.com/charmbracelet/huh) library for interactive prompts. This provides:

- **Input fields** with validation
- **Select menus** for choices
- **Confirm dialogs** for yes/no
- **Password input** with masking
- Built-in keyboard navigation (arrows, escape, etc.)
- Accessibility support

Example usage:

```go
import "github.com/charmbracelet/huh"

// Text input
var hostname string
huh.NewInput().
    Title("Dashboard hostname").
    Description("The URL you'll type in your browser").
    Placeholder("schmux.local").
    Value(&hostname).
    Validate(func(s string) error {
        if s == "" {
            return fmt.Errorf("hostname cannot be empty")
        }
        return nil
    }).
    Run()

// Select menu
var choice string
huh.NewSelect[string]().
    Title("TLS certificates").
    Options(
        huh.NewOption("Generate automatically", "generate"),
        huh.NewOption("I have my own", "manual"),
    ).
    Value(&choice).
    Run()

// Yes/No confirmation
var confirm bool
huh.NewConfirm().
    Title("Save configuration?").
    Affirmative("Yes").
    Negative("No").
    Value(&confirm).
    Run()

// Password input
var secret string
huh.NewInput().
    Title("Client Secret").
    EchoMode(huh.EchoModePassword).
    Value(&secret).
    Run()
```

**Navigation:**

- Arrow keys to navigate options
- Enter to confirm
- Escape to cancel
- Tab to move between fields in a form group

### Example Visual (with color annotations)

```
[cyan]━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━[/cyan]
[bold+cyan]  GitHub Authentication Setup[/bold+cyan]
[cyan]━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━[/cyan]

[dim]GitHub auth lets you log into the schmux dashboard using your GitHub account.

To set this up, you'll need:[/dim]
  [dim]1.[/dim] A hostname for the dashboard [dim](e.g., schmux.local)[/dim]
  [dim]2.[/dim] TLS certificates for HTTPS
  [dim]3.[/dim] A GitHub OAuth App

[cyan]━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━[/cyan]
[bold+cyan]  Step 1: Hostname[/bold+cyan]
[cyan]━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━[/cyan]

[dim]What hostname will you use to access the dashboard?
This will be the URL you type in your browser (e.g., https://schmux.local).[/dim]

[bold]Hostname[/bold] [dim][schmux.local][/dim]: _
```

### Symbols

| Symbol | Usage               | Unicode |
| ------ | ------------------- | ------- |
| `━`    | Section divider bar | U+2501  |
| `✓`    | Success             | U+2713  |
| `✗`    | Error               | U+2717  |
| `⚠`    | Warning             | U+26A0  |
| `→`    | Arrow (for steps)   | U+2192  |

---

## Flow

### Phase 1: Introduction

Display:

- Title: "GitHub Authentication Setup"
- Brief explanation of what GitHub auth does
- List the 3 things needed (hostname, TLS certs, GitHub OAuth App)

### Phase 2: Hostname Selection

**Prompt**: What hostname will you use?

- Default: existing `public_base_url` hostname, or `schmux.local` if not set
- Validate: must be a valid hostname (not empty, no spaces, etc.)
- Store as `https://<hostname>` for `public_base_url`

**Note**: If user enters a full URL, extract just the hostname.

### Phase 3: TLS Certificate Setup

**Check existing config first**:

- If `tls.cert_path` and `tls.key_path` are already set and files exist, show current paths and ask if they want to keep them

**Prompt**: Do you have TLS certificates for `<hostname>`?

Options:

1. **Yes, I have certificates** → Prompt for cert and key paths
2. **No, generate them for me** → Certificate generation flow
3. **No, and I'll set them up later** → Skip (will show warning at end)

#### Certificate Generation Flow

1. Check if `mkcert` is installed (`which mkcert`)
2. If not installed:

   ```
   ⚠ mkcert is not installed.

   Install it first:
     macOS:   brew install mkcert
     Linux:   See https://github.com/FiloSottile/mkcert#installation

   Then run this command again.
   ```

   Exit with code 1.

3. If installed, check if CA is installed (`mkcert -CAROOT` and check for rootCA.pem)
4. If CA not installed:

   ```
   mkcert needs to install its CA certificate (one-time setup).
   This may prompt for your password.

   Install CA now? [Y/n]:
   ```

   Run `mkcert -install` if confirmed.

5. Generate certificates:

   ```
   Generating certificates for schmux.local...
   ```

   - Create `~/.schmux/tls/` directory if needed
   - Run: `mkcert -cert-file ~/.schmux/tls/<hostname>.pem -key-file ~/.schmux/tls/<hostname>-key.pem <hostname>`
   - Verify files were created
   - Display success: `✓ Certificates saved to ~/.schmux/tls/`

6. Set cert/key paths to the generated files

#### Manual Certificate Path Flow

- Prompt for cert path (default: existing value)
- Prompt for key path (default: existing value)
- Validate files exist (warn if not, don't block)

### Phase 4: GitHub OAuth App Setup

**Check existing secrets first**:

- If `client_id` exists in secrets, show masked version and ask if they want to update

**Prompt**: Have you created a GitHub OAuth App?

Options:

1. **Yes** → Proceed to credentials
2. **No, help me create one** → Show creation guide

#### GitHub OAuth App Creation Guide

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Creating a GitHub OAuth App
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. Open: https://github.com/settings/developers

2. Click "New OAuth App" and enter:

   Application name:        schmux (or anything you like)
   Homepage URL:            https://schmux.local
   Authorization callback:  https://schmux.local/auth/callback

3. Click "Register application"

4. On the next page, click "Generate a new client secret"

5. Copy the Client ID and Client Secret

Press Enter when you're ready to continue...
```

Wait for Enter, then proceed to credentials.

#### Credential Collection

- Prompt for Client ID (default: existing value if set)
- Prompt for Client Secret (hidden input, no default shown for security)
  - If existing secret exists: "Client Secret (leave blank to keep existing):"

### Phase 5: Additional Settings

**Network Access**

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Additional Settings
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Network access allows other devices on your local network to reach the dashboard.
Without it, only this machine can connect.

Enable network access? [y/N]:
```

Default: existing value

**Session TTL**

```
Session TTL is how long you stay logged in before needing to re-authenticate.

Session TTL (minutes) [1440]:
```

Default: existing value or 1440 (24 hours)

### Phase 6: Validation and Summary

**Validate**:

- Certificate file exists and is readable
- Key file exists and is readable
- Certificate hostname matches `public_base_url` hostname (SAN or CN)
- Client ID is not empty
- Client Secret is not empty (or existing secret exists)

**Show summary**:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Configuration Summary
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Dashboard URL:     https://schmux.local
  TLS Certificate:   ~/.schmux/tls/schmux.local.pem
  TLS Key:           ~/.schmux/tls/schmux.local-key.pem
  GitHub Client ID:  Iv1.abc123...
  Network Access:    No
  Session TTL:       1440 minutes (24 hours)

✓ Certificate is valid for schmux.local
```

**Show warnings** (if any):

```
⚠ Warnings:
  - Certificate file not found: ~/.schmux/tls/schmux.local.pem
  - Certificate does not match hostname schmux.local
```

**Confirm save**:

- If no warnings: `Save configuration? [Y/n]:`
- If warnings: `Save anyway? [y/N]:` (default No)

### Phase 7: Save and Next Steps

**Save**:

- Write to `config.json` (under `access_control`)
- Write to `secrets.json` (under `auth.github`)

**Show next steps**:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Setup Complete
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Configuration saved

Next steps:
  1. Add schmux.local to /etc/hosts if you haven't already:
     echo "127.0.0.1 schmux.local" | sudo tee -a /etc/hosts

  2. Restart the daemon:
     ./schmux stop && ./schmux start

  3. Open https://schmux.local:7337 in your browser
```

---

## Re-running Behavior

When running `schmux auth github` on an already-configured system:

1. All prompts default to existing values from config/secrets
2. Hostname defaults to hostname extracted from `public_base_url`
3. Cert/key paths default to existing `tls.cert_path` and `tls.key_path`
4. Client ID defaults to existing value (shown)
5. Client Secret: if existing secret exists, blank input keeps it
6. Network access defaults to existing `network_access` value
7. Session TTL defaults to existing `session_ttl_minutes` value

Users can press Enter through all prompts to keep existing configuration, or change individual values.

---

## Data Written

### config.json

```json
{
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "tls": {
      "cert_path": "~/.schmux/tls/schmux.local.pem",
      "key_path": "~/.schmux/tls/schmux.local-key.pem"
    }
  },
  "access_control": {
    "enabled": true,
    "provider": "github",
    "session_ttl_minutes": 1440
  }
}
```

### secrets.json

```json
{
  "auth": {
    "github": {
      "client_id": "Iv1.abc123...",
      "client_secret": "..."
    }
  },
  "models": { ... }
}
```

---

## Error Handling

- **mkcert not installed**: Show install instructions, exit 1
- **mkcert -install fails**: Show error, suggest manual CA installation
- **Certificate generation fails**: Show mkcert error output, exit 1
- **User presses Ctrl+C**: Exit cleanly with no changes
- **Invalid hostname**: Show error, re-prompt
- **File write fails**: Show error, exit 1

---

## Implementation Notes

- Use `golang.org/x/term` for hidden password input (already in use)
- ANSI colors: use escape codes directly or simple helper functions
- Check terminal capability before using colors (`os.Getenv("TERM")` or `term.IsTerminal()`)
- Certificate generation uses `exec.Command("mkcert", ...)`
- Paths should support `~` expansion on save (already implemented in config loader)
