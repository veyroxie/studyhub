#!/usr/bin/env bash
# Copy the droplet's database backups to this machine.
#
# Off-site storage on a budget: the nightly dumps are ~50KB each, so a year of
# them is around 20MB. Keeping a copy on your own laptop costs nothing and
# survives the one failure the backups exist for — losing the droplet, which
# today would take every backup with it.
#
# This is a manual stopgap, not a replacement for automated off-site upload
# (set S3_BUCKET/S3_ACCESS_KEY/S3_SECRET_KEY for that). Run it whenever you
# think of it; running it twice is harmless.
#
#   make backup-pull
set -euo pipefail

DROPLET="${DROPLET:-root@167.99.64.149}"
REMOTE_DIR="${REMOTE_DIR:-/root/studyhub}"
DEST="${BACKUP_DEST:-$HOME/studyhub-backups}"

mkdir -p "$DEST"

echo "==> Copying backups from the droplet ..."
# -u skips files already here and unchanged, so repeat runs transfer nothing.
rsync -au --info=stats2 \
  -e 'ssh -o ConnectTimeout=10' \
  "$DROPLET:$REMOTE_DIR/backups/studyhub_*.sql.gz" "$DEST/" 2>/dev/null ||
  scp -o ConnectTimeout=10 "$DROPLET:$REMOTE_DIR/backups/studyhub_*.sql.gz" "$DEST/"

count=$(find "$DEST" -name 'studyhub_*.sql.gz' | wc -l)
newest=$(find "$DEST" -name 'studyhub_*.sql.gz' | sort | tail -1)
size=$(du -sh "$DEST" | cut -f1)

echo
echo "$count backup(s) held locally in $DEST ($size)"
echo "newest: $(basename "${newest:-none}")"
echo
echo "These contain student PII — keep this directory off shared machines and"
echo "out of any synced folder you would not put the database itself in."
