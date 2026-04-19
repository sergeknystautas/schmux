#!/usr/bin/env bash
# check-vendor-binary.sh — verify a vendor-locked schmux binary contains
# only stub implementations of the modules excluded by build tags
# (dashboardsx, tunnel, github), not their real implementations.
#
# This is defense-in-depth alongside the COMPILE-TIME check in
# internal/buildflags/vendor_combo_check.go. The compile check guarantees
# the four required tags are set together (nogithub, notunnel,
# nodashboardsx, vendorlocked); this script catches subtler regressions
# like a real-impl symbol leaking through a stub via accidental import.
#
# Pattern targets symbols that ONLY exist in the real implementation files
# (NOT in disabled.go stubs). Stubs intentionally export some of the same
# names (EnsureInstanceKey, Manager.Start, Discovery.GetPRs, etc.) for
# compile-time interface satisfaction with the rest of the codebase, so
# package-path matching alone would false-positive on legitimate stub
# symbols.
#
# Maintenance: when refactoring real-impl files in internal/dashboardsx,
# internal/tunnel, or internal/github, update PATTERN below to reference
# new function/variable names that should NEVER appear in a vendor build.
# Run ./scripts/check-vendor-binary.sh against a vendor binary to verify.
#
# Usage: $0 BINARY_PATH

set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 BINARY_PATH" >&2
    exit 2
fi

BINARY="$1"

if [[ ! -f "$BINARY" ]]; then
    echo "Error: binary not found: $BINARY" >&2
    exit 2
fi

PATTERN='(/internal/dashboardsx\.(ProvisionCert|LoadOrCreateAccount|SaveCert|legoLogAdapter|DetectBindableIPs|ServiceDNSProvider|acmeUser|checkAndRenew|heartbeatInterval|InstanceKeyPath|ACMEAccountPath|\(\*Client\)\.(Heartbeat|CertProvisioningStart|DNSChallengeCreate|DNSChallengeDelete|CallbackExchange|post))|/internal/tunnel\.(cloudflaredDownloadURL|FindCloudflared|EnsureCloudflared|verifyCodesign|extractTgz|fileSHA256|parseCloudflaredURL|installSuggestion|\(\*Manager\)\.setStatus)|/internal/github\.(httpClient|apiBaseURL|httpsPattern|parseRetryAfter|parseUsername|usernamePatterns|sshPattern|ghPullRequest|FetchOpenPRs|CheckVisibility|ParseRepoURL|CheckAuth|BuildReviewPrompt|PRBranchName|\(\*Discovery\)\.poll|\(\*Discovery\)\.stop))'

if go tool nm "$BINARY" | grep -E "$PATTERN"; then
    echo ""
    echo "FAIL: vendor binary contains real-implementation symbols (above)."
    echo "      A change in the upstream repo is leaking real-impl through"
    echo "      one of the *_disabled.go stubs. See"
    echo "      scripts/check-vendor-binary.sh for the symbol allowlist."
    exit 1
fi
echo "OK: $(basename "$BINARY") clean — no real-impl symbols leaked."
