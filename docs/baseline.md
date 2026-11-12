# Baseline (T1)

Recorded before any patch, on a cloud session (tree at `c590482`).

## Environment (from `scripts/cloud-probe.sh`)

Probe printed exit `20`, but its own checks contradict that: `ip` missing,
unprivileged `unshare` netns FAIL, `ip tuntap add` FAIL, DNS/egress to the panel
unreachable, no `xray` binary. Only `nft` in a netns passed. Effective tier:
**hermetic only**. The probe's summary lines ("netns usable: yes",
"host TUN creatable: yes") print regardless of the FAILs and should be fixed.

## Results

| Step | Result |
|------|--------|
| `cargo build --release` | ok (whoisthat v0.19.0) |
| `cargo test` | 290 passed + 4 passed, 0 failed |
| `go build ./...` (core/core) | ok |
| `go test ./...` (core/core) | 22 packages ok; 442 passed, 0 failed, 9 skipped |

Go skips (need xray binary, cgroups or network): TestVerifyGeoIP,
TestJSONCatchAllConfigPassesXrayValidation, TestValidateXrayConfigIntegration{,HappDNS,FakeDNS},
TestSplitScriptFailsWithoutCgroupDirs, TestStartStop, TestStartTwice,
TestUnexpectedExitDeliversError.

## Not runnable here

netns/TUN/E2E suites (T2, T3, T5, T7, T8, T11 acceptance) need `ip` and a
working netns; run them on a machine where the probe's netns check actually passes.
