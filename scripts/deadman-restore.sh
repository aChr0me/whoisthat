#!/usr/bin/env bash
# Deadman rollback: put the machine back on plain DHCP internet, whatever state
# whoisthat left it in. Idempotent, safe to run when nothing is armed, and
# deliberately blunt — the point is a reachable network within seconds, not
# tidiness.
#
# Arm:    systemd-run --user --on-active=5min --unit=whoisthat-deadman scripts/deadman-restore.sh
# Cancel: systemctl --user stop whoisthat-deadman.timer
# Run it by hand any time: sudo scripts/deadman-restore.sh
set -uo pipefail

# systemd-run units start with no HOME and a minimal PATH; both must be set here
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export HOME="${HOME:-/home/a03}"

BIN="${WHOISTHAT_BIN:-$(cd "$(dirname "$0")/.." && pwd)/target/release/whoisthat}"
LOG="${WHOISTHAT_DEADMAN_LOG:-$HOME/.hermes/cache/scratch/whoisthat-deadman.log}"
mkdir -p "$(dirname "$LOG")"
say() { printf '%s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "$LOG" >&2; }

say "deadman restore start"

# 1. kill switch off first — it is the thing that blackholes traffic
[ -x "$BIN" ] && "$BIN" settings set kill-switch=false --force -j >>"$LOG" 2>&1 && say "kill-switch disarmed"

# 2. drop the rules table outright, in case the CLI route failed
nft list table inet whoisthat_ks >/dev/null 2>&1 && { nft delete table inet whoisthat_ks && say "nft table deleted"; }

# 3. tear down the tunnel
[ -x "$BIN" ] && "$BIN" disconnect -j >>"$LOG" 2>&1 && say "core disconnect issued"
for ifc in tun0 tun9 bootun0 mytun0; do
    ip link show "$ifc" >/dev/null 2>&1 && { ip link set "$ifc" down 2>/dev/null; ip link del "$ifc" 2>/dev/null; say "removed $ifc"; }
done

# 4. let the DHCP client re-assert a default route (blunt but reliable)
if command -v nmcli >/dev/null; then
    con=$(nmcli -t -f NAME,DEVICE connection show --active 2>/dev/null | awk -F: '$2!="lo"{print $1; exit}')
    [ -n "${con:-}" ] && { nmcli connection up "$con" >>"$LOG" 2>&1 && say "re-upped connection: $con"; }
fi
ip route show default | grep -q . || { ip route add default via 10.97.255.254 dev wlp1s0 2>/dev/null && say "static default route added"; }

say "deadman restore done: default=$(ip route show default | head -1)"
say "verify: curl -sS -m 5 https://example.org >/dev/null && echo reachable"
