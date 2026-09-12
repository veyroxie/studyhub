#!/usr/bin/env bash
# Hand an account to a person: set a one-time password they must replace, and
# optionally change the role at the same time.
#
# The password is generated here, printed once, and spent the moment they use
# it: must_change_credentials means that session can reach the setup endpoint
# and nothing else until they choose their own email and password.
#
#   ./scripts/issue-temp-password.sh <sign-in-as> <target-user-id> [role]
#   ./scripts/issue-temp-password.sh etee3001@gmail.com 1 admin
#
# Find the id with: make psql  ->  SELECT id, email, role FROM users;
set -euo pipefail

BASE="${BASE:-https://studyhub.fit}"
ADMIN_EMAIL="${1:?usage: $0 <sign-in-as> <target-user-id> [role]}"
TARGET_ID="${2:?usage: $0 <sign-in-as> <target-user-id> [role]}"
ROLE="${3:-}"

JAR="$(mktemp -t shjar-XXXXXX)"
OUT="$(mktemp)"
trap 'rm -f "$JAR" "$OUT"' EXIT

json_str() { python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))'; }

read -rsp "Password for ${ADMIN_EMAIL}: " ADMIN_PW; echo

code=$(curl -s -o "$OUT" -w '%{http_code}' -c "$JAR" -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"email":%s,"password":%s}' \
        "$(printf '%s' "$ADMIN_EMAIL" | json_str)" \
        "$(printf '%s' "$ADMIN_PW" | json_str)")")
[ "$code" = "200" ] || { echo "login failed ($code): $(cat "$OUT")" >&2; exit 1; }

# Double-submit CSRF: the sh_csrf cookie value must be echoed in the header.
CSRF=$(awk '$6=="sh_csrf"{print $7}' "$JAR")
[ -n "$CSRF" ] || { echo "no sh_csrf cookie was issued" >&2; exit 1; }

TEMP="sh-$(python3 -c "
import secrets,string
a=string.ascii_letters+string.digits
print(''.join(secrets.choice(a) for _ in range(16)))")"

if [ -n "$ROLE" ]; then
  payload=$(printf '{"role":"%s","password":"%s"}' "$ROLE" "$TEMP")
else
  payload=$(printf '{"password":"%s"}' "$TEMP")
fi

code=$(curl -s -o "$OUT" -w '%{http_code}' -X PUT "$BASE/api/users/$TARGET_ID/credentials" \
  -b "$JAR" -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d "$payload")
[ "$code" = "200" ] || { echo "failed ($code): $(cat "$OUT")" >&2; exit 1; }

echo
echo "User $TARGET_ID${ROLE:+ (role: $ROLE)} — one-time password:"
echo
echo "    $TEMP"
echo
echo "They sign in with it once, then must choose their own email and password"
echo "before anything else in the app will respond."
