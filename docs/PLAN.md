# Plan — whoisthat as the Happ replacement (Claude Code implementation)

Goal: a headless, fail-closed, Omarchy-integratable Xray client for the existing
`open.marvelhub.art` crypt5 subscription, built on `kvunoff/whoisthat` (MIT), with
the audit blockers fixed. Widget comes after the client is proven; not in scope here.

Implementer: Claude Code. Auditor: Hermes (reads diffs, runs gates, verifies symbols
per finding — a delegate's "fixed" is a claim). Nothing is released or dispatched to
CI without the user's explicit go-ahead.

## Hard invariants

1. **The live Happ tunnel is never touched.** `happd.service` stays up, `happ-xray`
   keeps its address and route for the whole effort. whoisthat runs its own xray on
   its own TUN name; no `Happ.conf` edits, no happd socket calls.
2. **Never arm the kill switch blind.** Development happens unarmed. Every arming
   test pre-schedules its own disarm (`(sleep 90; whoisthat kill-switch off) &`) so a
   crash can't strand the box offline. `sudo -v` before any test that needs nft.
3. **Sub credentials never reach a repo or a log.** URL from env
   (`WHOISTHAT_TEST_SUB`), fixed test HWID, bodies cached to disk and replayed.
4. **One fixed HWID per environment.** The panel serves all 47 entries for any
   16-hex `X-HWID`, so tests can't burn device slots — but a fixed value keeps the
   provider's view stable and diffable.
5. **Fixture-first.** No test may require the network; live fetch is a separate,
   manually triggered tier.

## Billing / account hygiene (Claude)

Suspension on a Pro/Max plan is not about tokens — it is about *concurrency* and
*shared credentials*. Rules: one Claude session per work item, print mode for
one-shots, `--effort low` unless reasoning is the task, `/compact` the moment
`/context` passes 60%, no more than two live sessions, `--max-turns` always set,
and **Claude is never logged in inside CI** (Actions uses `cargo test`, not a model).

## Architecture

- Fork `kvunoff/whoisthat` → `/home/a03/Projects/whoisthat` (private; MIT keeps the fork clean).
- `whoisthat::crypt5` — resolve `happy-link/<id>` → follow to `happ://crypt5/…` →
  ChaCha20-Poly1305 unwrap (key table from hpwnr, vendored as a small Rust module)
  → `happy-accy/<id>` → fetch with `X-HWID` + `UA: Happ/4.7.2/Desktop/341`. Feed the
  result to the existing `xrayjson.IsConfigBody` path unchanged.
- Respect `profile-update-interval` for refresh; crypt5 is re-minted per refresh.
- `whoisthat-killswitch-boot.service` — system unit, `Before=network-pre.target`,
  re-installs the armed ruleset at boot; user unit stays for the session.
- Local patches carried against upstream; each fix gets an upstream PR if the
  author is receptive (repo was pushed the same day, so ask early).

## Tickets

Every ticket lists the audit finding it closes. F-* = security audit, K-* =
kill-switch audit. Acceptance is a *checkable* artifact (test, symbol, rule), never
prose.

**T1 — baseline.** Fork, `cargo build --release`, `go build ./...` in `core/core`,
run the suites. Record what is green before touching anything. Acceptance: build log
+ test counts in `docs/baseline.md`.

**T2 — DHCP allow (K-1, blocker).** Add `255.255.255.255` (and `dport 67` bound to
it) to both generator variants, nft and iptables. Acceptance: netns test asserting
a packet to `255.255.255.255:67` traverses with the switch armed; the same test
fails on the unpatched tree.

**T3 — DoT-safe DNS (K-2, blocker).** `dnsovertls <tun> no` on arm, restore prior
value on disarm (read it first, don't assume off). Detect a DoT-only resolver
(`resolvectl` + link DNS override present) and refuse to arm with a clear message if
it can't be neutralised. Acceptance: netns test with a stub DoT resolver; resolution
succeeds armed, and the prior `dnsovertls` value is restored after `off`.

**T4 — real armed state (K-3, blocker).** `kill_switch_armed` must verify the live
ruleset: chain exists **and** `policy drop` **and** the v4/v6 rule counts are
non-zero (parse `nft -j list table inet whoisthat_ks`). Backfill
`status -s` with a kill-switch field (K-11) and make `doctor` able to *fail*
(K-9). Acceptance: unit test over captured `nft -j` fixtures for empty chain, v4-only
half-install, and a correct ruleset — the first two must report not-armed.

**T5 — 53 without the sink (K-4, blocker).** Early-return the DNAT when the
destination is in `dns-servers`. Acceptance: netns test showing an allowlisted
resolver answers on 53 while armed, and that a `kill -9` of the core leaves 53
reachable for the allowlisted resolver.

**T6 — arm/clean transaction (K-6).** One `nft -f` batch with the drop policy and
all rules; on failure, delete the half-built table and report. Acceptance: injected
`nft` failure leaves no table behind (test asserts `nft list table` is empty).

**T7 — rotation re-arm (K-5).** Re-arm when `proxyIPs` changes (hook at
`mainproxy.go:420`), no full reconnect. Acceptance: test with a swapped proxy IP
shows the new address allowlisted and the old one gone.

**T8 — conntrack cleanup (K-7).** EXIT/`SIGTERM`/`SIGINT` handler removing
`whoisthat_mangle`; also clean iptables by table name, not by current iface.
Acceptance: `kill -9` + restart leaves no stale mangle table (test on the tidy path,
plus a documented manual recovery line for the hard-kill case).

**T9 — caps dropped (F-01, blocker).** Keep `net_admin`/`net_raw`; drop `setpcap`
right after the ambient raise; `PR_SET_KEEPCAPS` off for the xray child so the
provider config can't reach `net_admin`. Acceptance: `getpcaps` on the running core
and on the xray child show the intended sets; documented in `security.md`.

**T10 — fetch TLS + PATH pinning (F-17, F-03).** https-only for the sub fetch (and
for the redirect hop), fixed absolute paths for `nft`/`ip`/`xray` resolved once at
startup, `PATH` pinned in the spawned unit. Acceptance: unit test that an `http://`
sub is rejected; grep shows no bare `Command::new("nft")` left.

**T11 — boot unit (K-14).** System unit installing the armed ruleset before
`network-pre.target`, using config from disk; no dependency on a logged-in session.
Acceptance: `systemd-analyze verify`, then a reboot test with the switch armed and
the tunnel down — traffic must be dead until the core connects.

**T12 — crypt5 fetcher + wiring.** New module, CLI `whoisthat sub add <happy-link>`,
refresh honouring `profile-update-interval`, unit tests over cached fixtures
(marker parser, wrong-key rejection, redirect/302 chain, non-JSON body). Acceptance:
`cargo test` green with zero network, plus one manual live fetch.

**T13 — docs.** `docs/happ-migration.md`: what changes for the user, how to go back
to Happ, the recovery lines for every failure mode in this plan.

## Test tiers

- **T0 hermetic (default, in CI):** `cargo test`, `go test`, plus the netns suites
  run as root on the box and on an Actions runner (`sudo` is available on
  `ubuntu-latest`). Fixtures for every nft/ruleset assertion, captured once and
  committed.
- **T1 loopback E2E:** an xray server in a netns with self-generated REALITY keys;
  the client under test points at it. Proves TUN + kill switch end to end with no
  subscription and no account exposure.
- **T2 live sub (manual):** replay the cached body, then at most one live fetch per
  day, `WHOISTHAT_TEST_SUB` exported in the shell only.
- **T3 cloud:** GitHub Actions `ubuntu-latest` for build + T0 only. Optional single
  `workflow_dispatch` for the live smoke, sub in a repo secret — a dispatch per
  change, never a schedule, so the provider sees a handful of runner IPs at most.
- **T3a cloud session, first turn:** `bash scripts/cloud-probe.sh; echo $?` and paste
  the whole output. `20` = E2E may run in the session, `10` = netns/nft suites only,
  `0` = hermetic tier only. The probe also prints the TLS issuer for the panel host —
  an interception CA in that chain means the REALITY check in the same script has to
  pass before any connect test is trusted.
- **Do not run anything on the dev box unprompted.** Its network rides the live Happ
  tunnel: a broken run there costs the user their session, not just time. Writes,
  diffs and cloud sessions are free; host runs happen only when asked, and only after
  `bash scripts/cloud-probe.sh` confirms the machine still answers.

## Verification gates (Hermes, per ticket)

`git diff` greps for the expected symbol; suites rerun; netns test shown failing
before and passing after; `getpcaps`/`nft list` read directly, not from the report.
Anything the implementer claims without a diff hunk gets sent back.

## Rollback

Per patch: `git revert`. Whole effort: remove the fork; Happ was never modified, so
`happd` and `happ-xray` are still the working path. Live machine: `whoisthat
kill-switch off` (switch), `nft delete table inet whoisthat_ks` (last resort),
`systemctl --user disable --now whoisthat-core`.

## Definition of done

All blockers (K-1..K-4, F-01) fixed with tests that fail without the patch; reboot
survives armed; 47 entries from the live sub parse and connect; `status -s` reports
truthfully; nothing on the box references Happ at runtime while `happd` stays
installable for rollback.
