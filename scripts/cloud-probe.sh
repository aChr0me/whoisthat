#!/usr/bin/env bash
# cloud-probe.sh — decides how much of the whoisthat test suite a machine can run.
#
# Run this FIRST in any cloud session (or any unfamiliar VM/container) and hand
# the whole output back. It answers one question: can this environment build and
# test a TUN + nftables VPN client, or is it limited to the hermetic tier?
#
# Read-only on the host: creates at most a throwaway netns, a dummy link, one
# nft table and (if permitted) one unconfigured TUN device, and removes them.
#
# Exit codes: 0 = hermetic tier only, 10 = netns/nft suite only,
# 20 = TUN + nftables available (T1 E2E allowed).
# Capture the code directly — `./cloud-probe.sh; echo $?` — piping into `tail`
# loses it, the pipeline returns tail's status instead.

set -u -o pipefail
HOST="${PROBE_HOST:-open.marvelhub.art}"
PORT="${PROBE_PORT:-443}"
CAP=0
NERR=1; NETNS=0; TUNCAP=0
say() { printf '%s\n' "$*"; }
verdict() { say "VERDICT: $*"; exit "$1"; }
SUDO=""
step() { say ""; say "== $*"; }

step "identity"
say "uname:      $(uname -srm)"
say "user:       $(id -un) (uid $(id -u))"
say "container:  $( [ -f /run/.containerenv ] && echo podman || { [ -f /.dockerenv ] && echo docker || echo none-visible; } )"
say "systemd:    $( [ -d /run/systemd/system ] && systemctl --version | head -1 || echo absent )"
say "date:       $(date -u '+%Y-%m-%dT%H:%MZ')"

step "passwordless sudo"
if sudo -n true 2>/dev/null; then
  SUDO="sudo -n"; say "yes"
elif command -v sudo >/dev/null 2>&1; then
  if grep -qi 'fprint\|finger' /etc/pam.d/sudo 2>/dev/null; then
    say "sudo present but fingerprint-gated — unattended runs can time out, treat as no"
  else
    say "sudo present, needs interaction — treat as no for unattended runs"
  fi
else
  say "no sudo at all"
fi

step "toolchain"
for b in cargo rustc go git nft ip openssl unshare xray; do
  if command -v "$b" >/dev/null 2>&1; then say "$b: $(command -v "$b")"; else say "$b: MISSING"; fi
done
[ -n "$(command -v nft)" ] && [ -n "$(command -v ip)" ] && CAP=$((CAP + 0))

step "/dev/net/tun"
if [ -c /dev/net/tun ]; then
  say "/dev/net/tun present ($(stat -c '%A %U:%G' /dev/net/tun))"
else
  say "/dev/net/tun ABSENT"
fi

step "netns + dummy link (unprivileged user namespace)"
NSOPTS=""
[ -z "$SUDO" ] && NSOPTS="-r -n"
if ! command -v ip >/dev/null 2>&1; then
  say "SKIP — no iproute2 (netns and TUN both impossible)"
  NETNS=0
elif unshare $NSOPTS -- sh -c 'ip link add wtprobe0 type dummy && ip link show wtprobe0 >/dev/null' 2>/dev/null; then
  say "PASS unshare $NSOPTS + link add"
  NETNS=1
else
  say "FAIL unshare $NSOPTS + link add"
  NETNS=0
fi

step "nftables in a netns"
if unshare $NSOPTS -- sh -c 'nft add table inet wt_probe && nft add chain inet wt_probe o "{ type filter hook output priority 0; policy drop; }" && nft list table inet wt_probe >/dev/null' 2>/dev/null; then
  say "PASS nft table+drop chain"
  NERR=0
else
  say "FAIL nft (kernel modules missing or no CAP_NET_ADMIN)"
  NERR=1
fi

step "host TUN device, short-lived"
if [ -c /dev/net/tun ] && [ -n "$SUDO" ] && command -v ip >/dev/null 2>&1; then
  if $SUDO ip tuntap add dev wtprobe1 mode tun 2>/dev/null; then
    say "PASS ip tuntap add (host ns)"
    TUNCAP=1
    $SUDO ip link del wtprobe1 2>/dev/null
  else
    say "FAIL ip tuntap add"
    TUNCAP=0
  fi
else
  say "SKIP (needs /dev/net/tun, iproute2 and sudo)"
  TUNCAP=0
fi

step "DNS + egress through the network path"
if getent hosts "$HOST" >/dev/null 2>&1; then say "PASS DNS $HOST"; else say "FAIL DNS $HOST"; fi
say "egress IP: $(curl -fsS --max-time 10 https://ifconfig.me 2>/dev/null || echo unreachable)"

step "TLS termination on $HOST:$PORT (is the connection MITM'd?)"
say "public CA issuers look like: Let's Encrypt | R1x | GTS | DigiCert | Sectigo | ZeroSSL"
ISSUER=$(echo | timeout 12 openssl s_client -connect "$HOST:$PORT" -servername "$HOST" 2>/dev/null \
  | openssl x509 -noout -issuer -subject 2>/dev/null | tr '\n' ' ')
if [ -n "$ISSUER" ]; then
  say "chain: $ISSUER"
  case "$ISSUER" in
    *Anthropic*|*anthropic*|*Proxy*|*proxy*|*Zscaler*|*Netskope*)
      say "MITM-SUSPECT: issuer is not a public CA — REALITY may still work, verify with xray" ;;
    *) say "public-CA issuer, no obvious interception" ;;
  esac
else
  say "no TLS answer on $HOST:$PORT (raw TCP may be blocked)"
fi

step "xray REALITY sanity (only if xray + a config exist)"
if command -v xray >/dev/null 2>&1 && [ -n "${PROBE_XRAY_CONFIG:-}" ] && [ -r "${PROBE_XRAY_CONFIG:-}" ]; then
  if xray -test -c "$PROBE_XRAY_CONFIG" >/dev/null 2>&1; then
    say "config parses; start it and curl through socks to prove the handshake:"
    say "  xray -c \"$PROBE_XRAY_CONFIG\" & sleep 3; curl -x socks5h://127.0.0.1:10808 -m 15 https://ifconfig.me"
  else
    say "xray -test FAILED on $PROBE_XRAY_CONFIG"
  fi
else
  say "SKIP (no xray binary or PROBE_XRAY_CONFIG unset)"
fi

step "summary"
say "nft in netns:         $( [ "${NERR:-1}" -eq 0 ] && echo yes || echo no )"
say "netns usable:         $( [ "${NETNS:-0}" -eq 1 ] && echo yes || echo no )"
say "host TUN creatable:   $( [ "${TUNCAP:-0}" -eq 1 ] && echo yes || echo no )"

if [ "${TUNCAP:-0}" -eq 1 ] && [ "${NETNS:-0}" -eq 1 ] && [ "${NERR:-1}" -eq 0 ]; then
  verdict 20 "TUN + netns + nftables: T1 E2E may run here (still prove REALITY first, the egress may be proxied)."
elif [ "${NETNS:-0}" -eq 1 ] && [ "${NERR:-1}" -eq 0 ]; then
  verdict 10 "netns + nftables only: T0 queue and ruleset tests run; TUN bring-up needs /dev/net/tun, iproute2 and non-interactive sudo."
else
  verdict 0 "hermetic tier only: builds, unit tests and fixture parsing."
fi
