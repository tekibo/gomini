#!/usr/bin/env bash
set -euo pipefail

BINARY=gomini
CONFIG=config.example.json
PORT=8081
BASE="http://127.0.0.1:$PORT"
PASS=0
FAIL=0
SERVER_PID=

trap 'kill "$SERVER_PID" 2>/dev/null || true' EXIT

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

check() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then
    pass "$label"
  else
    fail "$label"
  fi
}

check_body() {
  local label=$1 pattern=$2
  shift 2
  local out
  out=$("$@" 2>&1) || { fail "$label (exit $?)"; return; }
  if grep -qE "$pattern" <<<"$out"; then
    pass "$label"
  else
    fail "$label (no match: $pattern)"
  fi
}

check_status() {
  local label=$1 expect=$2 url=$3
  shift 3
  local code
  code=$(curl -s -o /dev/null -w "%{http_code}" "$@" "$url") || true
  if [ "$code" = "$expect" ]; then
    pass "$label"
  else
    fail "$label (expected $expect, got $code)"
  fi
}

echo "=== Build ==="
go build -o "$BINARY" ./cmd/gomini
pass "go build"

echo "=== Start server ==="
"./$BINARY" --config "$CONFIG" &
SERVER_PID=$!
sleep 2
check "server started" kill -0 "$SERVER_PID"

echo "=== Endpoints ==="

check_body "GET  /"                  '"status":"ok"'          curl -s "$BASE/"
check_body "GET  /health"            '"status":"ok"'          curl -s "$BASE/health"
check_body "GET  /v1/models"         'gemini-3.7-flash'       curl -s "$BASE/v1/models"
check_body "GET  /v1beta/models"     'gemini-3.7-flash'       curl -s "$BASE/v1beta/models"
check_status "OPTIONS /"            204                       "$BASE/" -X OPTIONS

check_body "POST /v1/chat/completions"  '"error"|"choices"|"object"' \
  curl -s -X POST "$BASE/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d '{"model":"gemini-3.7-flash","messages":[{"role":"user","content":"hi"}],"stream":false}'

check_body "POST /v1/responses" '"error"|"output"|"status"' \
  curl -s -X POST "$BASE/v1/responses" \
    -H "Content-Type: application/json" \
    -d '{"model":"gemini-3.7-flash","input":"hi"}'

check_body "POST /v1beta/models/gemini-3.7-flash:generateContent" '"error"|"candidates"' \
  curl -s -X POST "$BASE/v1beta/models/gemini-3.7-flash:generateContent" \
    -H "Content-Type: application/json" \
    -d '{"contents":[{"parts":[{"text":"hi"}]}]}'

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
exit $FAIL
