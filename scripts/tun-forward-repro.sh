#!/usr/bin/env bash
# Offline TUN forwarding check, no root: runs in a user+net namespace with a
# veth "internet", mirrors the core's tuntap/fwmark/rp_filter setup and asserts
# that a TCP request through the xray tun inbound completes.
#   unshare -Urnm bash scripts/tun-forward-repro.sh /path/to/xray
set -u
X=${1:?usage: tun-forward-repro.sh /path/to/xray}
cd "$(mktemp -d)"
ip link set lo up
# peer netns holding the "internet"
unshare -n sleep 60 & PEER=$!
sleep 0.3
ip link add wan0 type veth peer name wan1 netns $PEER
ip addr add 10.9.0.2/24 dev wan0; ip link set wan0 up
nsenter -t $PEER -n sh -c 'ip link set lo up; ip addr add 10.9.0.1/24 dev wan1; ip link set wan1 up; ip addr add 203.0.113.7/32 dev lo; python3 -m http.server 80 --bind 203.0.113.7 >/dev/null 2>&1 &
sleep 60' &
sleep 1
ip route add 203.0.113.7/32 via 10.9.0.1   # reachable "internet" target via wan
# tun as core creates it
ip tuntap add mode tun dev wtun
ip link set wtun mtu 1400; ip addr add 198.18.0.1/15 dev wtun; ip link set wtun up
ip rule add fwmark 1 table 100
ip route replace 10.9.0.1/32 dev wan0 table 100
ip route replace default via 10.9.0.1 table 100
[ "$(curl -s -m 3 -o /dev/null -w "%{http_code}" http://203.0.113.7/ --interface wan0)" = 200 ] || { echo "FAIL: testbed baseline"; exit 2; }
nsenter -t $PEER -n ss -ltn
sysctl -qw net.ipv4.conf.wan0.rp_filter=2 net.ipv4.conf.wtun.rp_filter=2
ip route add default dev wtun metric 1
ip route del 203.0.113.7/32 || true
cat > cfg.json <<J
{"log":{"loglevel":"debug"},
"inbounds":[{"tag":"tun-in","protocol":"tun","settings":{"name":"wtun","MTU":1400},"sniffing":{"enabled":true,"routeOnly":false,"destOverride":["http","tls"]}}],
"outbounds":[{"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"mark":1}}}]}
J
$X run -c cfg.json >xray.log 2>&1 &
sleep 2
CODE=$(curl -s -m 5 -o /dev/null -w "%{http_code}" http://203.0.113.7/)
kill %1 2>/dev/null; kill $PEER
[ "$CODE" = 200 ] && echo "PASS: forwarded through tun" || { echo "FAIL: http=$CODE"; tail -5 xray.log; exit 1; }
