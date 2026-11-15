# HANDOFF — host acceptance for whoisthat (for the Claude session on this box)

Repo: `/home/a03/Projects/whoisthat`, branch `hermes/plan`. Read `docs/PLAN.md`,
`docs/host-acceptance.md`, `docs/happ-migration.md`, `docs/crypt5-format.md`.

All hermetic work is finished and green: `go test ./...` (all packages) and
`cargo test` (300 + 4 passed). What remains is what only this machine can prove.

Current machine state: no VPN is active (neither Happ nor IVPN), `wlp1s0` is the
uplink, `sudo` wants a password (timestamp_timeout=10, per-tty), and there is **no
system `xray`** — the only copy is Happ's `/opt/happ/bin/core/xray`
(Xray 26.10.29). Do not modify or remove anything under `/opt/happ` or
`~/.config/Happ`; Happ stays installed as the rollback.

## Ticket 0 — install a verified xray core

Do this first; every other ticket needs it.

1. Latest upstream is **v26.11.9**. Asset
   `https://github.com/XTLS/Xray-core/releases/download/v26.11.9/Xray-linux-64.zip`
   with a `.dgst` sidecar (`Xray-linux-64.zip.dgst`) containing the SHA2-256.
   It is not packaged in this box's pacman repos, so install the release binary:
   download zip and `.dgst` to a scratch dir, verify the zip's sha256 against the
   dgst, unzip, `install -m 0755 xray /usr/local/bin/xray`, then
   `/usr/local/bin/xray -version` and record the version string.
2. Sanity: `xray run -test -c <a config>` on a config produced by `whoisthat`
   (or the fixture in the tests) must pass.
3. Record version, sha256 and date in `docs/host-acceptance.md`.
4. If a system xray ever appears (`pacman -S xray`), prefer that and delete the
   manual copy — one binary, auto-updated, no stray `/usr/local` version drift.

## Tickets — host acceptance, in this order

Follow `docs/host-acceptance.md` exactly. Non-negotiables:

- `sudo -v` before anything privileged (it will prompt; that is expected).
- Before every arming test, pre-schedule the disarm:
  `( sleep 90; whoisthat settings set kill-switch=false --force ) &`
- Recovery ladder if something goes wrong:
  `whoisthat settings set kill-switch=false --force` → `nft delete table inet whoisthat_ks`
  → `systemctl --user restart NetworkManager`. Never touch `happd`.
- Record results **with the command output pasted** into `docs/host-acceptance.md`,
  not a verdict word. A test that only passes after a manual disarm is a fail.

1. **Live crypt5 fetch** — `whoisthat sub add <happy-link>`, confirm the resolved
   accy URL returns the 47-entry JSON array, and that `X-HWID` + the Happ UA are what
   the panel saw. The test vectors so far come from the test encoder, so this is the
   first real-link proof.
2. **T5** — resolvers answer while armed, including after `kill -9` of the core.
3. **T8** — no stale `whoisthat_mangle` table after a hard kill; document the one
   manual line a `-9` still needs.
4. **T7** — re-arm on a live proxy-IP rotation (new allowlisted, old gone, no reconnect).
5. **T11** — `systemd-analyze verify` the boot unit, then the armed-reboot test: arm,
   reboot, confirm traffic is dead before the core connects, then that connect works.

Acceptance commit: one commit per ticket on `hermes/plan`, `git push origin
hermes/plan` each time, and a final short summary naming the output that proves each
ticket. If a ticket reveals a code defect, fix it in the same commit and say which
test would have caught it.
