#!/usr/bin/env bash
# One-off: promote Chiying and Nadine to admin, and issue all three staff
# accounts a temporary password they must replace at first sign-in.
#
# The seeded password "Teacher123!" shipped in source (fixed in 0068 and the
# seed), so every teacher login on the live site is currently guessable by
# anyone with the repository.
#
# Needs an ADMIN session. The API uses double-submit CSRF: the sh_csrf cookie
# must equal the X-CSRF-Token header on every mutating request, which is why
# a bare curl with -b cookies.txt returns "missing CSRF token".
#
#   ./scripts/rotate-staff-credentials.sh admin@studyhub.com
set -euo pipefail

BASE="${BASE:-https://studyhub.fit}"
ADMIN_EMAIL="${1:?usage: $0 <admin-email>}"
JAR="$(mktemp -t shjar-XXXXXX)"
trap 'rm -f "$JAR"' EXIT

read -rsp "Password for ${ADMIN_EMAIL}: " ADMIN_PW; echo

echo "==> Signing in ..."
code=$(curl -s -o /tmp/login.out -w '%{http_code}' -c "$JAR" -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"email":%s,"password":%s}' "$(printf '%s' "$ADMIN_EMAIL" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')" "$(printf '%s' "$ADMIN_PW" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')")")
if [ "$code" != "200" ]; then
  echo "login failed ($code): $(cat /tmp/login.out)" >&2; exit 1
fi

# The token is the sh_csrf cookie value; the header must carry the same string.
CSRF=$(awk '$6=="sh_csrf"{print $7}' "$JAR")
[ -n "$CSRF" ] || { echo "no sh_csrf cookie was issued" >&2; exit 1; }
echo "    signed in, CSRF token captured"

set_creds() {
  local id="$1" label="$2" payload="$3"
  local out code
  out=$(mktemp)
  code=$(curl -s -o "$out" -w '%{http_code}' -X PUT "$BASE/api/users/$id/credentials" \
    -b "$JAR" -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d "$payload")
  if [ "$code" = "200" ]; then
    echo "    ok   $label"
  else
    echo "    FAIL $label ($code): $(cat "$out")" >&2
  fi
  rm -f "$out"
}

echo "==> Rotating ..."
set_creds 2 "chiying -> admin" '{"role":"admin","password":"sh-AH4iERlclvWCMxxQ"}'
set_creds 3 "nadine  -> admin" '{"role":"admin","password":"sh-wtE6KCx4JqCwAI8s"}'
set_creds 4 "rose    (teacher)" '{"password":"sh-ZBB6DngO7gZZ5BaL"}'

echo
echo "Each of them signs in once with the password above and is then forced to"
echo "choose their own email and password before anything else will work."
