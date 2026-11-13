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
   `( sleep 90; whoisthat kill-switch off ) &` — a crashed run cannot strand the box
   offline.
5. Known recovery lines, rehearsed before the first arm:
   `whoisthat kill-switch off` → `nft delete table inet whoisthat_ks` →
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
Rollback: `whoisthat kill-switch off`.

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
- A test that passes only after a manual `kill-switch off` is a fail: it means the
  failure path leaks.
- Happ stays installed the whole time. Removing it is a separate, later decision.
