#!/bin/bash
# Checks if UI/API changes have corresponding scenario test updates.
# Used as a post-commit nudge — non-blocking (unless --strict is passed).

STRICT=false
if [[ "${1:-}" == "--strict" ]]; then
  STRICT=true
fi

CHANGED_FILES=$(git diff --cached --name-only 2>/dev/null || git diff HEAD~1 --name-only 2>/dev/null)

UI_CHANGED=false
API_CHANGED=false
SCENARIOS_CHANGED=false

for file in $CHANGED_FILES; do
  case "$file" in
    assets/dashboard/src/routes/*) UI_CHANGED=true ;;
    internal/dashboard/handlers*) API_CHANGED=true ;;
    test/scenarios/*) SCENARIOS_CHANGED=true ;;
  esac
done

if [ "$UI_CHANGED" = true ] || [ "$API_CHANGED" = true ]; then
  if [ "$SCENARIOS_CHANGED" = false ]; then
    echo "💡 You modified dashboard routes or API handlers but no scenario files were updated."
    echo "   Consider running /scenario to add test coverage."
  fi
fi

if [ "$STRICT" = true ] && { [ "$UI_CHANGED" = true ] || [ "$API_CHANGED" = true ]; } && [ "$SCENARIOS_CHANGED" = false ]; then
  exit 1
fi
