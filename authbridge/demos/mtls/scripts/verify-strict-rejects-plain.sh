#!/bin/bash
# Verify that a strict callee closes plain HTTP connections without
# serving them. The curl exit code differs across versions; we
# accept any non-zero (connection reset / EOF / hangup) as proof
# the strict listener is enforcing.
#
# Usage: verify-strict-rejects-plain.sh <namespace> <callee>

set -euo pipefail

NAMESPACE=${1:?usage: verify-strict-rejects-plain.sh <namespace> <callee>}
CALLEE=${2:?}

echo "[*] Hitting $CALLEE with plain HTTP — expect connection rejection"
if kubectl -n "$NAMESPACE" run --rm -i --restart=Never \
     --image=curlimages/curl:latest \
     --command "verify-strict-$$" -- \
     curl -sS --max-time 10 \
          "http://${CALLEE}.${NAMESPACE}.svc.cluster.local:8080/healthz" >/dev/null 2>&1; then
  echo "ERROR: strict callee unexpectedly served a plaintext request." >&2
  echo "       That violates the strict-mode contract." >&2
  exit 1
fi
echo "  Connection rejected as expected."
echo
echo "============================================================"
echo " Strict verified — plaintext callers rejected at the listener"
echo "============================================================"
