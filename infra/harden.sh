#!/usr/bin/env bash
#
# harden.sh — baseline hardening for the StudyHub droplet (Ubuntu/Debian).
#
# Run ON the droplet as root:   bash infra/harden.sh
#
# SAFETY: this disables SSH password login. It REFUSES to do so unless it can
# see an authorized SSH key for the current user, so you can't lock yourself
# out. If anything goes wrong, the DigitalOcean web Console (Access → Launch
# Droplet Console) still gets you in — it doesn't use SSH.
#
# What it does (all idempotent):
#   1. Firewall: default-deny inbound, allow only SSH (22), HTTP (80), HTTPS (443)
#   2. SSH: disable password auth + root password login (key-only)
#   3. Automatic security updates (unattended-upgrades)
#   4. fail2ban for SSH brute-force protection
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then echo "Run as root (sudo bash infra/harden.sh)"; exit 1; fi

echo "==> 1/4 Firewall (ufw): default-deny inbound; allow 22, 80, 443"
apt-get update -y -qq
apt-get install -y -qq ufw
ufw allow OpenSSH      >/dev/null || ufw allow 22/tcp >/dev/null
ufw allow 80/tcp       >/dev/null
ufw allow 443/tcp      >/dev/null
ufw default deny incoming  >/dev/null
ufw default allow outgoing >/dev/null
ufw --force enable
ufw status verbose

echo "==> 2/4 SSH: enforce key-only login"
# Lock-out guard: require at least one authorized key before disabling passwords.
keys_found=0
for f in /root/.ssh/authorized_keys /home/*/.ssh/authorized_keys; do
  if [ -s "$f" ]; then keys_found=1; fi
done
if [ "$keys_found" -ne 1 ]; then
  echo "!! No SSH authorized_keys found — SKIPPING password-auth disable to avoid lockout."
  echo "   Add your public key (ssh-copy-id) first, then re-run."
else
  conf=/etc/ssh/sshd_config.d/99-harden.conf
  cat > "$conf" <<'EOF'
PasswordAuthentication no
PermitRootLogin prohibit-password
ChallengeResponseAuthentication no
KbdInteractiveAuthentication no
EOF
  if sshd -t; then
    systemctl reload ssh 2>/dev/null || systemctl reload sshd
    echo "   Key-only SSH enforced (via $conf)."
  else
    echo "!! sshd config test failed — reverting"; rm -f "$conf"
  fi
fi

echo "==> 3/4 Automatic security updates"
apt-get install -y -qq unattended-upgrades
dpkg-reconfigure -f noninteractive unattended-upgrades || true
systemctl enable --now unattended-upgrades || true

echo "==> 4/4 fail2ban (SSH brute-force protection)"
apt-get install -y -qq fail2ban
systemctl enable --now fail2ban
fail2ban-client status sshd 2>/dev/null || true

echo "==> Done. Verify you can still open a NEW ssh session before closing this one."
