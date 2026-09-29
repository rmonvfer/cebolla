#!/bin/bash
# Host side, step 3 (and every later update): copy the project into the VM
# and (re)start the stack. Refuses to run unless the VM's root filesystem is
# on LUKS. Generates secrets inside the VM on first run.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
AGEKEY=$HOME/.config/onion-report/key.txt   # private key: decrypts filter-hit reports

ssh -o ConnectTimeout=10 onion true || { echo "cannot ssh to the VM (is the disk unlocked?)"; exit 1; }
if ! ssh onion 'lsblk -s -n -o TYPE "$(findmnt -n -o SOURCE /)" | grep -qx crypt'; then
  echo "REFUSING: the VM root filesystem is not on an encrypted (LUKS) device."; exit 1
fi

if [ ! -f "$AGEKEY" ]; then
  mkdir -p "$(dirname "$AGEKEY")"; chmod 700 "$(dirname "$AGEKEY")"
  age-keygen -o "$AGEKEY" 2>/dev/null; chmod 600 "$AGEKEY"
fi
RECIPIENT=$(age-keygen -y "$AGEKEY")

rsync -a --delete --exclude .git --exclude .env --exclude crawler/go.sh --exclude 'node_modules' --exclude 'webui/.next' "$ROOT/" onion:onion-crawler/

ssh onion RECIPIENT="$RECIPIENT" bash -s <<'EOF'
set -euo pipefail
cd ~/onion-crawler
if [ ! -f .env ]; then
  umask 077
  cp .env.example .env
  sed -i "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$(openssl rand -hex 24)|" .env
  sed -i "s|^OPENSEARCH_PASSWORD=.*|OPENSEARCH_PASSWORD=Os1-$(openssl rand -hex 20)|" .env
  sed -i "s|^GRAFANA_PASSWORD=.*|GRAFANA_PASSWORD=$(openssl rand -hex 16)|" .env
  echo "generated .env with random secrets"
fi
sed -i "s|^REPORT_AGE_RECIPIENT=.*|REPORT_AGE_RECIPIENT=$RECIPIENT|" .env
docker compose build --pull
docker compose up -d
docker compose ps --format 'table {{.Service}}\t{{.Status}}'
EOF
echo
echo "Explorer + Grafana, from aleph:  ssh -N -L 8088:127.0.0.1:8088 -L 3000:127.0.0.1:3000 onion"
echo "  from a laptop, through aleph:   ssh -t -L 8088:127.0.0.1:8088 -L 3000:127.0.0.1:3000 aleph ssh -N -L 8088:127.0.0.1:8088 -L 3000:127.0.0.1:3000 onion"
echo "  then http://localhost:8088 (explorer) and http://localhost:3000 (Grafana: admin / GRAFANA_PASSWORD in ~/onion-crawler/.env on the VM)"
echo "Search:     ssh onion 'cd onion-crawler && docker compose run --rm crawler search \"your query\"'"
