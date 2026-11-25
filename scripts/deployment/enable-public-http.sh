#!/usr/bin/env bash
set -euo pipefail
if [[ $(id -u) != 0 ]]; then echo 'Run this script with sudo.' >&2; exit 1; fi
binary=/home/yijiangli/.local/state/seller-ops-native/bin/seller-public-gateway
[[ -x "$binary" ]] || { echo 'Gateway binary is not prepared.' >&2; exit 1; }
if ss -lnt '( sport = :80 )' | tail -n +2 | grep -q .; then
 systemctl is-active --quiet seller-ops-public-gateway || { echo 'Port 80 is already occupied.' >&2;exit 1; }
fi
cat > /etc/systemd/system/seller-ops-public-gateway.service <<'UNIT'
[Unit]
Description=Seller Ops public HTTP gateway
After=network-online.target
Wants=network-online.target
[Service]
User=yijiangli
Group=yijiangli
Environment=PUBLIC_APP_ORIGIN=http://43.139.227.179
ExecStart=/home/yijiangli/.local/state/seller-ops-native/bin/seller-public-gateway
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
Restart=on-failure
RestartSec=3
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now seller-ops-public-gateway
if command -v ufw >/dev/null && ufw status | grep -q '^Status: active'; then ufw allow 80/tcp; fi
systemctl --no-pager --full status seller-ops-public-gateway
runuser -u yijiangli -- /usr/bin/node /data/workspace/yijiangli/seller-ops/scripts/deployment/activate-public-http.mjs
printf '\nPublic entry: http://43.139.227.179/\n'
