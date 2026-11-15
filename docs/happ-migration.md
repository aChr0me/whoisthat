---
title: "Migrating from Happ"
description: "What changes when whoisthat replaces Happ, how to go back, and the recovery line for every failure mode."
audience: "user"
type: "how-to"
---

# Migrating from Happ

whoisthat is a headless, fail-closed Xray client for the same subscription Happ
uses. This page covers what changes for you, how to return to Happ, and what to
run when something goes wrong. Commands are copy-pasteable and were checked
against the CLI in this tree.

## Status of this migration

Be clear about what is and is not proven before relying on it. "Implemented"
means code plus a hermetic test that fails without it. "Host-acceptance pending"
means the part that needs a real kernel, TUN device, reboot or live panel has not
been run; see [host acceptance](/host-acceptance).

| Area | Implemented (hermetic test) | Host-acceptance pending |
|---|---|---|
| K-4: allowlisted resolvers answer with the switch armed | `dns-servers` are excluded from the port-53 redirect in all three plans (`TestDnsHijackForExcludesAllowlistedResolvers`). | A resolver answering in a netns while the core is killed (`kill -9`). |
| K-5: allowlist follows proxy-IP rotation | The allowlist is re-armed atomically when the resolved set changes, is a no-op when unchanged, and keeps the old IPs if the re-arm fails (`TestRotateProxyIPsRearmsWithNewAddress`, `TestRotateProxyIPsFailedRearmKeepsOldIPs`, `TestRotationWatcherRearmsOnResolvedChange`). | A live rotation on the box. |
| K-7: conntrack/mangle cleanup | Removed by table or chain name on every shutdown path, including a hung `Stop()`, and in the startup reconcile. | `kill -9` plus restart on a real host. |
| K-14: boot-time arming | `whoisthat autostart boot-unit` emits the unit; it passes `systemd-analyze verify`. | An armed reboot with the tunnel down: traffic must stay dead until the core connects. |
| Other kill-switch fixes (K-1…K-3, K-6) | Code and hermetic tests done. | DHCP traversal on a real link; DoT resolver behaviour. |
| crypt5 links (`happ://crypt5/…`) | `whoisthat sub add` accepts a `…/happy-link/<id>` URL or a `happ://crypt5/…` link. The core follows the 302 to the marker, decrypts it against the 36-marker key table vendored in `core/core/lib/crypt5/keys_table.go`, and fetches the result with `X-HWID` and `User-Agent: Happ/4.7.2/Desktop/341`. This runs on every refresh, since crypt5 is re-minted each time. Errors (no HWID, unknown marker, malformed payload, AEAD failure, non-https) are distinct and never fall back to a plain fetch (`TestHappResolveHappyPath`, `TestHappRefreshRedecryptsChangedPayload`, `TestHappErrorsAreDistinctAndNeverFallThrough`, `TestHappRejectsHTTPLink`). | One live fetch against a real panel. The test vectors come from a test-only encoder using the table's own keys, not a recorded Happ link. |
| Happ itself | Never modified. `happd.service` and `happ-xray` keep their address and route. | None. |

## What changes for you

- **Two clients, two TUN devices.** whoisthat runs its own Xray on its own TUN
  name (`tun-name`, default `whoisthattun`). It does not edit `Happ.conf` or talk
  to the `happd` socket. Run only one of them as your active tunnel.
- **Fail-closed by default when you enable the kill-switch.** With
  `kill-switch=true` an unexpected core exit leaves traffic blocked, not leaking.
  The rules survive `whoisthat core stop` on purpose.
- **Allowlisted resolvers keep working.** Port-53 traffic to the configured
  `dns-servers` is excluded from the DNS redirect, so those resolvers still
  answer if the core dies (K-4; the live proof is host-acceptance pending).
- **The kill-switch follows server IP changes.** If the server's address
  rotates while connected, the allowlist is re-armed in one atomic step, with no
  reconnect (K-5; the live rotation is host-acceptance pending).
- **Reboots can be covered.** The optional boot unit arms the switch before the
  network comes up (K-14, below). Without it, the switch re-arms when the core
  starts in your session.
- **Subscription fetches are https-only**, and `nft`/`ip`/`xray` are resolved
  from fixed system paths, not your `PATH`.
- **Device id.** The subscription provider sees whoisthat's HWID, not Happ's.
  It is generated once and persisted, so it is stable and does not use up a
  device slot on each refresh. Check it with `whoisthat hwid status`. Happ links
  need it enabled: with HWID off or unset the core refuses to fetch rather than
  receive placeholder cards.

## Switching over

1. Leave Happ running. Do not stop `happd`.
2. Start whoisthat unarmed first: `whoisthat core start`, add the subscription,
   connect, and confirm `whoisthat status` and `whoisthat ip` look right.
3. Only then arm: `whoisthat settings set kill-switch=true`, and check
   `whoisthat settings get kill-switch-armed` reads `true`.

When testing an arm for the first time, pre-schedule the disarm so a mistake
cannot strand the machine offline:

```bash
( sleep 90; whoisthat settings set kill-switch=false --force ) &
```

## Boot-time arming (optional)

The user service starts with your session, so there is a window at boot before
it runs. The system unit closes it by arming from the config on disk before
`network-pre.target`, with an empty server allowlist: only loopback, DHCP and
your allowlisted resolvers pass, and everything else stays blocked until the
core connects and installs the real server IPs.

```bash
whoisthat autostart boot-unit | sudo tee /etc/systemd/system/whoisthat-killswitch-boot.service
systemd-analyze verify /etc/systemd/system/whoisthat-killswitch-boot.service
sudo systemctl enable whoisthat-killswitch-boot.service
```

The unit reads `config.json` read-only and arms only if `kill-switch-enabled` is
true. It has no `ExecStop`, so stopping it or shutting down never drops the
ruleset. To remove it:

```bash
sudo systemctl disable whoisthat-killswitch-boot.service
sudo rm /etc/systemd/system/whoisthat-killswitch-boot.service
```

Turning the kill-switch off in settings does not remove the unit, but the unit
then arms nothing on the next boot.

## Going back to Happ

Happ was never touched, so going back is removing whoisthat's effect:

```bash
whoisthat settings set kill-switch=false --force   # drop the block first
whoisthat disconnect
systemctl --user disable --now whoisthat-core
sudo systemctl disable --now whoisthat-killswitch-boot.service 2>/dev/null
```

Then confirm `happd.service` is active and `happ-xray` is still up. Nothing else
needs restoring.

## Recovery by failure mode

Work down each list; stop at the first line that restores connectivity.

### Offline with the kill-switch armed

```bash
whoisthat settings set kill-switch=false --force
sudo nft delete table inet whoisthat_ks          # last resort, always safe to repeat
systemctl --user restart NetworkManager
```

On the iptables backend the chain is `WHOISTHAT_KS`; `whoisthat core restart`
runs the reconcile that removes it (see [Kill-Switch](/usage/kill-switch)).

### Offline right after a reboot

Expected if the boot unit is enabled and the core has not connected yet. Log in
and let the core connect. If it never connects, run the lines above. To stop the
boot unit arming next time: `whoisthat settings set kill-switch=false --force`
and, if desired, disable the unit as shown above.

### DNS fails with the kill-switch armed

Resolvers outside `dns-servers` are blocked on purpose. Check the
`dns-servers` list in `config.json` ([Configuration](/reference/configuration)) and add your resolver, or disarm. If the
system resolver is DNS-over-TLS only, arming refuses with a message instead of
breaking DNS; the previous `dnsovertls` value is restored on disarm.

### Core was killed hard (`kill -9`, crash, power loss)

The next `whoisthat core start` runs the startup reconcile, which removes stale
tables and chains and re-arms if the switch is still enabled. Manual cleanup of
the conntrack-bypass state, by name:

```bash
sudo nft delete table ip whoisthat_mangle 2>/dev/null
sudo nft delete table ip6 whoisthat_mangle 2>/dev/null
sudo nft delete table inet whoisthat_mangle 2>/dev/null
for ipt in iptables ip6tables; do
  sudo $ipt -t mangle -D PREROUTING -j WHOISTHAT_MANGLE_PRE 2>/dev/null
  sudo $ipt -t mangle -D OUTPUT -j WHOISTHAT_MANGLE_OUT 2>/dev/null
  sudo $ipt -t mangle -F WHOISTHAT_MANGLE_PRE 2>/dev/null; sudo $ipt -t mangle -X WHOISTHAT_MANGLE_PRE 2>/dev/null
  sudo $ipt -t mangle -F WHOISTHAT_MANGLE_OUT 2>/dev/null; sudo $ipt -t mangle -X WHOISTHAT_MANGLE_OUT 2>/dev/null
done
```

A graceful stop (SIGINT, SIGTERM, `whoisthat core stop`) removes this state
itself, by table name, even if the TUN teardown times out.

### Server IP changed and traffic stalls

The core re-resolves the server periodically and re-arms on change. To force it
immediately: `whoisthat disconnect && whoisthat connect <profile>`.

### TUN device or routes left behind

```bash
whoisthat core restart                      # reconcile removes the TUN, rules and tables 100/200
sudo ip link del dev whoisthattun 2>/dev/null
```

### Subscription will not add or refresh

- `http://` subscription URLs are rejected; use https.
- A Happ link (`…/happy-link/<id>` or `happ://crypt5/…`) fails with a specific
  error and is never retried as a plain URL: *no hardware id* (enable HWID, see
  `whoisthat hwid status`), *unknown key marker* or *malformed payload* (the
  panel minted a format this build does not know), *decryption failed* (corrupt
  or truncated link; re-copy it), or *not an https URL*.
- An empty or placeholder card list usually means a wrong or missing HWID:
  `whoisthat hwid status`.

### Happ stopped working

whoisthat does not touch it. Check `systemctl status happd`, confirm the
whoisthat kill-switch is off, and that only one of the two tunnels is active.
