#!/usr/bin/env bash
set -euo pipefail

base_ref="${GITHUB_BASE_REF:-main}"

git fetch --no-tags origin "${base_ref}" --depth=1 >/dev/null 2>&1 || true

base_commit="$(git merge-base HEAD "origin/${base_ref}" 2>/dev/null || true)"
if [[ -z "${base_commit}" ]]; then
  base_commit="$(git rev-parse HEAD^ 2>/dev/null || true)"
fi

if [[ -z "${base_commit}" ]]; then
  echo "Unable to determine base commit; skipping API doc gate."
  exit 0
fi

changed_files="$(git diff --name-only "${base_commit}"...HEAD)"
if [[ -z "${changed_files}" ]]; then
  exit 0
fi

api_regex='^(internal/dashboard/|internal/nudgenik/|internal/config/|internal/state/|internal/workspace/|internal/session/|internal/tmux/)'
api_changed="$(echo "${changed_files}" | grep -E "${api_regex}" || true)"
doc_changed="$(echo "${changed_files}" | grep -E '^docs/api\.md$' || true)"

if [[ -n "${api_changed}" && -z "${doc_changed}" ]]; then
  echo "API-related changes detected without docs/api.md update."
  echo "Update docs/api.md to match the API contract."
  echo
  echo "API-related changes:"
  echo "${api_changed}"
  exit 1
fi
