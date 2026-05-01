#!/bin/bash
# One-shot setup script for a fresh Ubuntu 22.04 / 24.04 DigitalOcean Droplet.
# Run as root immediately after provisioning.
#
# Usage:
#   ssh root@<droplet-ip>
#   curl -fsSL https://raw.githubusercontent.com/JeffMboya/zenith-link/main/scripts/deploy-do.sh | bash
set -euo pipefail

REPO="https://github.com/JeffMboya/zenith-link.git"
APP_DIR="/opt/zenith-link"

# ------------------------------------------------------------------
# 1. Swap — CesiumJS/Vite build peaks at ~700 MB. On a 512 MB Droplet
#    the build OOMs without swap. 2 GB swap on a 10 GB disk is safe.
# ------------------------------------------------------------------
if [ ! -f /swapfile ]; then
  echo "==> Adding 2 GB swapfile (needed for Node build on 512 MB RAM)..."
  fallocate -l 2G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile
  swapon /swapfile
  echo '/swapfile none swap sw 0 0' >> /etc/fstab
  # Reduce swappiness so the OS prefers RAM when available at runtime
  sysctl vm.swappiness=10
  echo 'vm.swappiness=10' >> /etc/sysctl.conf
  echo "    Swap active: $(free -h | grep Swap)"
else
  echo "==> Swapfile already exists — skipping."
fi

# ------------------------------------------------------------------
# 2. Docker
# ------------------------------------------------------------------
if ! command -v docker &>/dev/null; then
  echo "==> Installing Docker..."
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg lsb-release
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
    https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin
  systemctl enable --now docker
else
  echo "==> Docker already installed — skipping."
fi

# ------------------------------------------------------------------
# 3. Repo
# ------------------------------------------------------------------
echo "==> Cloning zenith-link..."
if [ -d "$APP_DIR/.git" ]; then
  git -C "$APP_DIR" pull
else
  git clone "$REPO" "$APP_DIR"
fi
cd "$APP_DIR"

# ------------------------------------------------------------------
# 4. Secrets
# ------------------------------------------------------------------
if [ ! -f .env ]; then
  cp .env.example .env
  HMAC_KEY=$(openssl rand -hex 32)
  sed -i "s/^ZENITH_HMAC_KEY=$/ZENITH_HMAC_KEY=$HMAC_KEY/" .env
  echo "==> Generated ZENITH_HMAC_KEY — saved to $APP_DIR/.env"
  echo "    Add VITE_CESIUM_TOKEN= to .env for 3D globe terrain (optional)."
else
  echo "==> .env already exists — skipping key generation."
fi

# ------------------------------------------------------------------
# 5. Build + start
#    Build happens inside Docker — Node is memory-limited to 400 MB
#    so the heap stays under the 2 GB swap ceiling.
# ------------------------------------------------------------------
echo "==> Building images (this takes ~2-3 min on a 2-vCPU Droplet)..."
docker compose -f docker/docker-compose.prod.yaml --env-file "$APP_DIR/.env" up -d --build

DROPLET_IP=$(curl -s http://checkip.amazonaws.com 2>/dev/null || hostname -I | awk '{print $1}')
echo ""
echo "================================================================"
echo "  Zenith-Link is live at: http://${DROPLET_IP}"
echo "================================================================"
echo ""
echo "Useful commands (run from $APP_DIR):"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml --env-file $APP_DIR/.env logs -f"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml --env-file $APP_DIR/.env ps"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml --env-file $APP_DIR/.env restart"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml --env-file $APP_DIR/.env down"
echo ""
echo "To update after a git push:"
echo "  cd $APP_DIR && git pull && docker compose -f docker/docker-compose.prod.yaml --env-file "$APP_DIR/.env" up -d --build"
