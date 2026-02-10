# Autoformatting Design

## Overview

Automated code formatting for all languages in the schmux codebase via two entry points:

- **`./format.sh`** - On-demand formatting of all files
- **`.git/hooks/pre-commit`** - Formats staged files before commit

## Formatters

| Language               | Tool     | Config          |
| ---------------------- | -------- | --------------- |
| Go                     | `gofmt`  | None (built-in) |
| TypeScript, JavaScript | Prettier | `.prettierrc`   |
| CSS                    | Prettier | `.prettierrc`   |
| Markdown               | Prettier | `.prettierrc`   |
| JSON                   | Prettier | `.prettierrc`   |

## Files to Create

```
schmux-003/
├── .prettierrc              # Prettier config (minimal)
├── .prettierignore          # Exclude generated/vendor files
├── format.sh                # On-demand: format entire codebase
└── .git/hooks/pre-commit    # Hook: format staged files only
```

## Configuration

### `.prettierrc`

```json
{
  "semi": true,
  "singleQuote": true,
  "tabWidth": 2,
  "trailingComma": "es5",
  "printWidth": 100
}
```

### `.prettierignore`

```
assets/dashboard/dist/
assets/dashboard/node_modules/
*.generated.ts
```

## Scripts

### `format.sh` (on-demand)

```bash
#!/bin/bash
set -e

# Ensure prettier is installed
if ! (cd assets/dashboard && npm list prettier >/dev/null 2>&1); then
    echo "Installing prettier..."
    (cd assets/dashboard && npm install --save-dev prettier)
fi

echo "Formatting Go files..."
gofmt -w $(find . -name '*.go' -not -path './vendor/*')

echo "Formatting TS/JS/CSS/MD/JSON files..."
cd assets/dashboard && npx prettier --write "../.." --ignore-path "../../.prettierignore"

echo "Done!"
```

### `.git/hooks/pre-commit`

```bash
#!/bin/bash
set -e

# Get staged files
GO_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)
PRETTIER_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep -E '\.(ts|tsx|js|jsx|css|md|json)$' || true)

# Ensure prettier is installed (only if we have prettier-eligible files)
if [ -n "$PRETTIER_FILES" ]; then
    if ! (cd assets/dashboard && npm list prettier >/dev/null 2>&1); then
        echo "Installing prettier..."
        (cd assets/dashboard && npm install --save-dev prettier)
    fi
fi

# Format Go files
if [ -n "$GO_FILES" ]; then
    echo "Formatting Go files..."
    echo "$GO_FILES" | xargs gofmt -w
    echo "$GO_FILES" | xargs git add
fi

# Format Prettier files
if [ -n "$PRETTIER_FILES" ]; then
    echo "Formatting TS/JS/CSS/MD/JSON files..."
    echo "$PRETTIER_FILES" | xargs npx --prefix assets/dashboard prettier --write --ignore-unknown
    echo "$PRETTIER_FILES" | xargs git add
fi
```

## Usage

### On-demand formatting

```bash
./format.sh
```

### Automatic on commit

```bash
git add . && git commit -m "message"  # Hook runs automatically
```

## CLAUDE.md Update

Update pre-commit requirements to mention formatting is handled automatically.
