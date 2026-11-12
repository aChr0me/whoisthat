# Baseline (T1)

Recorded on untouched upstream tree at `c590482` (branch `hermes/plan`), cloud session,
2026-11-12. Nothing modified before these runs.

## Environment probe

`bash scripts/cloud-probe.sh` exit code: **20** (script verdict), but not trustworthy:

- `ip` and `xray` missing; `ip link add` / `ip tuntap add` checks FAILED, yet the
  summary printed "host TUN creatable: yes" (it only checks `/dev/net/tun` + sudo).
- `open.marvelhub.art` does not resolve and there is no egress from the session.
- Only `nft` table + drop chain inside a netns passed.

Effective tier used: **hermetic + nft ruleset only**. No TUN / E2E / live fetch.

## Builds

| Command | Result |
|---|---|
| `cargo build --release` (whoisthat v0.19.0) | ok, 1m34s |
| `go build ./...` in `core/core` | ok |

## Tests

| Suite | Result |
|---|---|
| `cargo test --release` | 290 passed + 4 passed, 0 failed |
| `go test ./...` in `core/core` | 22 packages ok, 442 passing tests, 0 failures |

Go packages ok: core, commands, db, lib, AppConfig, PortPool, TCPServer, crypto, geo,
happ, happdns, logger, netmark, parser, mainproxy (10.8s), tun, xray, version,
xrayjson, xraymgr, structs, utils.

Not run: netns suites, loopback E2E, live sub (none exist yet / tier not available).
