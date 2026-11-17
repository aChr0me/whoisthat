#!/usr/bin/env bash
# T11: the boot-arm path, without rebooting.
#
# --arm-boot is what a boot unit calls before the network is up: it installs the
# block from config and exits. Two preconditions the first version of this test
# missed: it only acts when kill-switch=true is persisted, and --check-clean only
# reports orphans while no core is running.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/core/core/whoisthat-core"
WT="$ROOT/target/release/whoisthat"
CFG="${HOME}/.config/whoisthat"
OUT="${WT_OUT:-/home/a03/.hermes/cache/scratch/wt-bootarm.json}"
started=$(date -u +%FT%TZ)
fail=0
note=""

verdict() {
  printf '{"result":"%s","scenario":"bootarm","note":"%s","started":"%s","finished":"%s"}\n' \
    "$1" "$note" "$started" "$(date -u +%FT%TZ)" > "$OUT.tmp"
  mv "$OUT.tmp" "$OUT"
}
bad() { fail=1; note="${note:+$note }$1"; }
nftok() {
  sudo -n nft list table inet "$1" >/dev/null 2>&1 && return 0
  timeout 20 pkexec nft list table inet "$1" >/dev/null 2>&1
}
restore() {
  nftok whoisthat_ks && "$BIN" --cleanup "$CFG" >/dev/null 2>&1
  systemctl --user is-active --quiet whoisthat-core || systemctl --user start whoisthat-core
  [ "${prev:-false}" = "true" ] || "$WT" settings set kill-switch=false -j >/dev/null 2>&1
}
trap restore EXIT

mkdir -p "$(dirname "$OUT")"
verdict running
prev=$("$WT" settings get kill-switch -j 2>/dev/null | grep -o 'true\|false' | head -1)
"$WT" settings set kill-switch=true -j >/dev/null 2>&1

# Stop the daemon: a boot unit runs when nothing else does.
systemctl --user stop whoisthat-core
for _ in $(seq 1 15); do systemctl --user is-active --quiet whoisthat-core || break; sleep 1; done

"$BIN" --arm-boot "$CFG" >/dev/null 2>&1 || bad "arm-boot-failed"
sleep 1
nftok whoisthat_ks || bad "no-ruleset-after-arm-boot"
"$BIN" --check-clean "$CFG" >/dev/null 2>&1 && bad "check-clean-missed-orphan-state"
"$BIN" --cleanup "$CFG" >/dev/null 2>&1 || bad "cleanup-failed"
sleep 1
nftok whoisthat_ks && bad "ruleset-survived-cleanup"
"$BIN" --check-clean "$CFG" >/dev/null 2>&1 || bad "check-clean-still-dirty"

if [ "$fail" -eq 0 ]; then verdict pass; else verdict fail; fi
[ "$fail" -eq 0 ]
