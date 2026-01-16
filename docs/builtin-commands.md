# Built-in Commands Feature Specification

## Overview

This spec describes the built-in commands feature, which provides a library of pre-defined command templates that users can quickly run without having to configure custom commands in their config file.

Note: The config schema now uses `run_targets` and `quick_launch` instead of `agents`/`commands`. References below should be read with that mapping in mind.

## Problem Statement

Users want to run common AI coding tasks (code review, git commit, etc.) quickly without having to:
1. Manually create custom command entries in `~/.schmux/config.json`
2. Remember the exact prompts for common operations
3. Re-configure commands when setting up schmux on a new machine

## Requirements

1. Built-in commands should be version-controlled in the codebase
2. Built-in commands should appear in both spawn locations:
   - The spawn dropdown (quick-run menu on workspaces)
   - The spawn wizard (multi-step spawn form)
3. Built-in commands should be merged with user-defined commands
4. User commands that duplicate built-in command names should be filtered out (built-ins take precedence)
5. The feature should work in production (installed binary) and development

## Initial Built-in Commands

The following commands were specified as the initial set:

```json
[
  {
    "name": "code review - local",
    "prompt": "please use the code review ai plugin to evaluate the local changes"
  },
  {
    "name": "code review - branch",
    "prompt": "please use the code review ai plugin to evaluate the current branch"
  },
  {
    "name": "git commit",
    "prompt": "please create a thorough git commit. do not include the generated and co-authored lines. please keep the message focused whenever possible on the features, not just describe code changes."
  },
  {
    "name": "merge in main",
    "prompt": "please merge main into this branch. Each individual commit should be preserved, and resolve as many merge conflicts as you can without user input."
  }
]
```

## Implementation Details

### Backend (Go)

#### File: `internal/dashboard/builtin_commands.json`

Stores the built-in command templates. Each command has:
- `name`: Display name for the command
- `prompt`: The prompt text that will be sent to the AI agent

#### File: `internal/dashboard/handlers.go`

**Added import:**
```go
import (
    "embed"
    // ... other imports
)

//go:embed builtin_commands.json
var builtinCommandsFS embed.FS
```

**Added type:**
```go
// BuiltinCommand represents a built-in command template.
// The Prompt field contains the text that will be sent as the command to the agent.
type BuiltinCommand struct {
    Name   string `json:"name"`
    Prompt string `json:"prompt"`
}
```

**Added handler:**
```go
// handleBuiltinCommands returns the list of built-in commands.
func (s *Server) handleBuiltinCommands(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Try embedded file first (production), fall back to filesystem (development)
    var data []byte
    var readErr error
    data, readErr = builtinCommandsFS.ReadFile("builtin_commands.json")
    if readErr != nil {
        // Fallback to filesystem for development
        candidates := []string{
            "./internal/dashboard/builtin_commands.json",
            filepath.Join(filepath.Dir(os.Args[0]), "../internal/dashboard/builtin_commands.json"),
        }
        for _, candidate := range candidates {
            data, readErr = os.ReadFile(candidate)
            if readErr == nil {
                break
            }
        }
        if readErr != nil {
            log.Printf("[builtin-commands] failed to read commands file: %v", readErr)
            http.Error(w, "Failed to load built-in commands", http.StatusInternalServerError)
            return
        }
    }

    var commands []BuiltinCommand
    if err := json.Unmarshal(data, &commands); err != nil {
        log.Printf("[builtin-commands] failed to parse commands: %v", err)
        http.Error(w, "Failed to parse built-in commands", http.StatusInternalServerError)
        return
    }

    // Validate and filter commands
    validCommands := make([]BuiltinCommand, 0, len(commands))
    for _, cmd := range commands {
        if strings.TrimSpace(cmd.Name) == "" {
            log.Printf("[builtin-commands] skipping command with empty name")
            continue
        }
        if strings.TrimSpace(cmd.Prompt) == "" {
            log.Printf("[builtin-commands] skipping command %q with empty prompt", cmd.Name)
            continue
        }
        validCommands = append(validCommands, cmd)
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(validCommands)
}
```

#### File: `internal/dashboard/server.go`

**Added route registration:**
```go
mux.HandleFunc("/api/builtin-commands", s.withCORS(s.handleBuiltinCommands))
```

Place this route alongside other API routes (around line 64).

### Frontend (React)

#### File: `assets/dashboard/src/lib/api.js`

**Added API function:**
```javascript
/**
 * Fetches the list of built-in commands.
 * Returns a list of command templates with names and prompts.
 * @returns {Promise<Array<{name: string, prompt: string}>>}
 */
export async function getBuiltinCommands() {
  const response = await fetch('/api/builtin-commands');
  if (!response.ok) {
    throw new Error('Failed to fetch built-in commands');
  }
  return response.json();
}
```

**Added utility function:**
```javascript
/**
 * Merges user commands with built-in commands.
 * User commands come from config as {name, command, agentic: false}.
 * Built-in commands come from the API as {name, prompt}.
 *
 * This function:
 * 1. Converts built-in commands to match user command format
 * 2. Filters out user commands that duplicate built-in command names
 * 3. Returns built-in commands first, then user commands
 *
 * @param {Array<{name: string, command: string, agentic?: boolean}>} userCommands - Commands from user config
 * @param {Array<{name: string, prompt: string}>} builtinCommands - Built-in commands from API
 * @returns {Array<{name: string, command: string, agentic: boolean}>} Merged command list
 */
export function mergeCommands(userCommands, builtinCommands) {
  // Get set of built-in command names for deduplication
  const builtinNames = new Set(builtinCommands.map(c => c.name));

  // Convert built-in commands to user command format
  const builtinConverted = builtinCommands.map(bc => ({
    name: bc.name,
    command: bc.prompt,  // Note: prompt becomes command for spawning
    agentic: false
  }));

  // Filter out user commands that duplicate built-in names
  const userFiltered = userCommands.filter(c => !builtinNames.has(c.name));

  // Return built-in first, then user commands
  return [...builtinConverted, ...userFiltered];
}
```

#### File: `assets/dashboard/src/components/WorkspacesList.jsx`

**Import updates:**
```javascript
import { disposeSession, disposeWorkspace, openVSCode, getBuiltinCommands, mergeCommands } from '../lib/api.js';
import React, { useState, useEffect } from 'react';  // Added useEffect
```

**State addition:**
```javascript
const [builtinCommands, setBuiltinCommands] = useState([]);
```

**Effect to fetch built-in commands:**
```javascript
// Fetch built-in commands on mount
useEffect(() => {
  let active = true;
  const fetchBuiltinCommands = async () => {
    try {
      const commands = await getBuiltinCommands();
      if (active) {
        setBuiltinCommands(commands);
      }
    } catch (err) {
      // Silently fail - built-in commands are optional
      console.warn('Failed to fetch built-in commands:', err);
    }
  };
  fetchBuiltinCommands();
  return () => { active = false; };
}, []);
```

**Updated commands useMemo:**
```javascript
// Merge user commands (from config) with built-in commands
const commands = React.useMemo(() => {
  const userCommands = (config?.agents || []).filter(a => a.agentic === false);
  return mergeCommands(userCommands, builtinCommands);
}, [config?.agents, builtinCommands]);
```

#### File: `assets/dashboard/src/routes/SpawnPage.jsx`

**Import updates:**
```javascript
import { getConfig, spawnSessions, getBuiltinCommands, mergeCommands } from '../lib/api.js';
```

**Updated load effect (within the existing useEffect):**
```javascript
// Fetch built-in commands
let builtinCommands = [];
try {
  builtinCommands = await getBuiltinCommands();
} catch (err) {
  console.warn('Failed to fetch built-in commands:', err);
  // Continue without built-in commands
}

// Merge user commands with built-in commands using shared utility
setCommands(mergeCommands(userCommandItems, builtinCommands));
```

## Testing

### Backend Tests

**File: `internal/dashboard/handlers_test.go`**

Added comprehensive tests for `handleBuiltinCommands`:

```go
func TestHandleBuiltinCommands(t *testing.T) {
    cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
    st := state.New("")
    statePath := t.TempDir() + "/state.json"
    wm := workspace.New(cfg, st, statePath)
    sm := session.New(cfg, st, statePath, wm)
    server := NewServer(cfg, st, statePath, sm, wm)

    t.Run("GET request returns commands", func(t *testing.T) {
        // ... validates 200 response, non-empty results, valid structure
    })

    t.Run("POST request is rejected", func(t *testing.T) {
        // ... validates 405 Method Not Allowed
    })

    t.Run("response contains expected commands", func(t *testing.T) {
        // ... validates presence of all expected command names
    })
}
```

## API Endpoint

### GET /api/builtin-commands

**Response:**
```json
[
  {
    "name": "code review - local",
    "prompt": "please use the code review ai plugin to evaluate the local changes"
  },
  ...
]
```

**Error responses:**
- `405 Method Not Allowed` - for non-GET requests
- `500 Internal Server Error` - if file cannot be read or parsed

## Data Flow

1. Frontend component mounts → calls `getBuiltinCommands()`
2. Backend reads from embedded `builtin_commands.json` (production) or filesystem (development)
3. Backend validates and filters commands, returns JSON array
4. Frontend receives built-in commands
5. Frontend calls `mergeCommands(userCommands, builtinCommands)`
6. Merged list is displayed in UI (spawn dropdown or spawn wizard)

## Edge Cases Handled

1. **Empty name/prompt**: Commands with empty or whitespace-only names or prompts are filtered out on the backend with a warning log
2. **Duplicate names**: User commands that share a name with a built-in command are filtered out (built-ins take precedence)
3. **API failure**: If `/api/builtin-commands` fails, the frontend continues with just user commands (logs a warning)
4. **Production deployment**: Using `embed` ensures the JSON file is compiled into the binary
5. **Development mode**: Filesystem fallback allows editing the JSON file without recompiling during development

## Notes for Re-implementation

When implementing this in the new architecture:

1. **Field naming**: The `prompt` field in built-in commands becomes the `command` field when spawning. This is because built-in commands are templates/prompts that get sent to the agent.

2. **User command format**: User commands from config have `{name, command, agentic: false}` format.

3. **Built-in command format**: Built-in commands from API have `{name, prompt}` format.

4. **Spawn behavior**: When spawning with a built-in command, the `prompt` is used as the `prompt` parameter in the spawn request (for agentic agents) or as the `command` (for non-agentic agents).

5. **Duplicate handling**: The decision was made to have built-in commands take precedence over user commands with the same name. This ensures users get the "official" built-in version even if they've created a custom command with the same name.

6. **Testing**: Tests should verify the actual command names from the JSON file, not just count. This ensures the JSON file is correctly structured.

7. **Error handling**: The feature degrades gracefully - if built-in commands fail to load, the system still works with user commands only.
