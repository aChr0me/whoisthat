# Host acceptance — the part only the real machine can prove

Everything in `T0` is hermetic and runs in the cloud. The tickets below need a real
kernel, a real TUN device and the real tunnel, so they run **last**, on the dev box,
only on the user's go-ahead. `docs/PLAN.md` marks them host-acceptance-pending.

## Preconditions, in order

1. `happd.service` active and `happ-xray` still up — Happ is the rollback, never touched.
2. `bash scripts/cloud-probe.sh` on the box reports `20`. Anything less and the run
   stops here rather than guessing.
3. `sudo -v` first (this box gates sudo on a fingerprint; an unattended `sudo` inside a
   test will otherwise time out mid-way).
4. Arm-time safety net on every arming test:
   `( sleep 90; whoisthat settings set kill-switch=false --force ) &` — a crashed run cannot strand the box
   offline.
5. Known recovery lines, rehearsed before the first arm:
   `whoisthat settings set kill-switch=false --force` → `nft delete table inet whoisthat_ks` →
   `systemctl --user restart NetworkManager` → router/`happd` untouched.

## Suites first, one shell, in this order

```
cd ~/Projects/whoisthat/core/core && go build ./... && go test ./...
cd ~/Projects/whoisthat && cargo build --release && cargo test
```

Both must be green *before* anything arms. A red suite means the ticket is not accepted
regardless of what the cloud reports.

## T5 — resolvers answer with the switch armed

Arm with the default resolvers, then `dig @<allowlisted resolver> example.org +short`
and `getent hosts example.org`. Then `kill -9` the core and repeat both. DNS must answer
in all three states; the failure this closes was a hard blackhole after `-9`.
Rollback: `whoisthat settings set kill-switch=false --force`.

## T7 — rotation re-arm

Force a proxy-IP change (touch the config or wait for `profile-update-interval`), then
check the live ruleset: the new address allowlisted, the old one gone, no full reconnect.
`nft list table inet whoisthat_ks` before and after.

## T8 — no stale mangle table

`systemctl --user stop whoisthat-core`, confirm `nft list table inet whoisthat_mangle`
fails cleanly, restart, connect again. Then the hard case: `kill -9` the core, confirm
the documented manual cleanup actually removes it, and that a restart is clean afterwards.

## T11 — boot with the switch armed

Arm, reboot, log in, and with the core *not* yet connected confirm traffic is dead and
DNS is not silently answered from a stale lease. Then confirm the core's own connect
works. `systemd-analyze verify` the boot unit first; this is the only test that costs a
reboot.

## Acceptance notes

- Record each result in the ticket's section here, with the command output pasted, not a
  verdict word.
- A test that passes only after a manual `kill-switch=false` is a fail: it means the
  failure path leaks.
- Happ stays installed the whole time. Removing it is a separate, later decision.

## Ticket 0 result — verified xray install

- Version: `Xray 26.11.9 (Xray, Penetrates Everything.) 676eaf6 (go1.27.3 linux/amd64)`
- Source: https://github.com/XTLS/Xray-core/releases/download/v26.11.9/Xray-linux-64.zip
- SHA2-256 (matched the release `.dgst` sidecar): `b651f7e1d9966ee2c1b1ee1bf8b52c72771824d3065dce96bbc8bcc19ed95f82`
- Installed binary sha256: `e87bce586acdb9136aa5b1f1d9c8db9f1ea1d145b6d86368c7f6f662f9f6a66d`
  (differs from the zip hash as expected — one is the archive, one the extracted
  binary; both independently confirmed: `unzip`'s extracted file matches the
  installed file byte for byte via `sha256sum`).
- Installed path: `/usr/local/bin/xray`, mode 0755, owner root, 2026-11-15.
- No system package exists for xray on this box (`pacman -Ss ^xray$` empty); manual
  install is the only option until one is packaged.
- Happ's own copy at `/opt/happ/bin/core/xray` (26.10.29) left untouched.

## Bugs found and fixed during the first live run (commit 86417b9)

- crypt5 decrypt assumed URL-safe base64; the real happy-link payload uses
  standard base64 (`+`/`/`). Fixed to try standard first, URL-safe as fallback.
- xray preflight called `run -test` with no `-format`/`-c` flags; v26.x needs
  `-format=json -c /dev/stdin` or every profile fails latency with exit 23.
- IPv6 default-route install omitted `dev <iface>`; RA gateways are almost
  always link-local (`fe80::...`), and the kernel refuses a link-local nexthop
  with no explicit device ("Egress device not specified").
- Live TUN enable is `whoisthat mode tun` after `connect`, not
  `settings set autoconnect-mode=tun` (that key only controls boot
  autoconnect, confirmed by reading the CLI's own `mode --help`).

## T5 — first attempt, inconclusive (needs a clean re-run)

Connect (`3:3`, group 3, real vless/hysteria2 profile) → `mode tun` →
`settings set kill-switch=true` succeeded: `status -j` showed
`connected:true, mode:tun, kill_switch_armed:true`, and `nft list table inet
whoisthat_ks` showed the real drop-all-except-whitelist chain with the live
server IPs. DNS (`getent hosts`) and HTTPS (`curl`) both worked while armed.

The actual `kill -9 core` + re-check step was lost to a client disconnect
mid-command; no verified pass/fail recorded. Retest needed, plus confirm the
new `whoisthat-core --cleanup` one-shot clears the state afterward without a
full daemon restart.

**Real finding from this attempt, independent of the daemon's own
correctness**: after the daemon died (whether from this test or a plain
`kill -9`), the `whoisthat_mangle`/`whoisthat` nft tables and the
`whoisthattun` interface survived indefinitely — the daemon's own startup
reconcile (`ReconcileOrphanedRules`) only runs on the *next* `whoisthat-core`
start, so a user who doesn't relaunch it stays stuck with a half-torn-down
firewall state that broke DHCP-sourced DNS until manually cleared. Added
`whoisthat-core --cleanup` as a direct, no-daemon-needed hook for this —
see commit 86417b9. Still needs a documented/automated trigger (recovery
docs below, and ideally a systemd `ExecStopPost` once the daemon runs as a
unit) so a crash doesn't require remembering a manual command.
