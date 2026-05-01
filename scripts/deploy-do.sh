#!/bin/bash
# One-shot setup script for a fresh Ubuntu 22.04 / 24.04 DigitalOcean Droplet.
# Run as root (or with sudo) immediately after provisioning.
#
# Usage:
#   ssh root@<droplet-ip>
#   curl -fsSL https://raw.githubusercontent.com/JeffMboya/zenith-link/main/scripts/deploy-do.sh | bash
#
# Or clone and run locally:
#   bash scripts/deploy-do.sh
set -euo pipefail

REPO="https://github.com/JeffMboya/zenith-link.git"
APP_DIR="/opt/zenith-link"

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

echo "==> Cloning zenith-link..."
git clone "$REPO" "$APP_DIR" 2>/dev/null || (cd "$APP_DIR" && git pull)
cd "$APP_DIR"

echo "==> Creating .env..."
if [ ! -f .env ]; then
  cp .env.example .env
  HMAC_KEY=$(openssl rand -hex 32)
  sed -i "s/^ZENITH_HMAC_KEY=$/ZENITH_HMAC_KEY=$HMAC_KEY/" .env
  echo "    Generated ZENITH_HMAC_KEY automatically."
  echo "    Edit $APP_DIR/.env to add your VITE_CESIUM_TOKEN (optional)."
else
  echo "    .env already exists — skipping."
fi

echo "==> Building and starting services..."
docker compose -f docker/docker-compose.prod.yaml up -d --build

DROPLET_IP=$(curl -s http://checkip.amazonaws.com 2>/dev/null || hostname -I | awk '{print $1}')
echo ""
echo "Done. Zenith-Link is running at: http://${DROPLET_IP}"
echo ""
echo "Useful commands:"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml logs -f"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml restart"
echo "  docker compose -f $APP_DIR/docker/docker-compose.prod.yaml down"
