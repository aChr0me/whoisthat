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

Be clear about what is and is not proven before relying on it.

| Area | State |
|---|---|
| Kill-switch fixes K-1…K-7, K-14 | Code and hermetic tests done. The behaviour that needs a real kernel, TUN device or reboot (DHCP traversal, resolver answering while armed, live proxy-IP rotation, boot ordering) is **host-acceptance pending**: see [host acceptance](/host-acceptance). |
| crypt5 links (`happ://crypt5/…`) | Marker parser, redirect chain and AEAD decrypt exist. `whoisthat sub add <happy-link>` does **not** resolve crypt5 yet: the 36-marker key table is vendored (`core/core/lib/crypt5/keys_table.go`), but nothing calls the decryptor from the subscription path, so no real link decrypts today. Until that is wired, add a subscription whose URL is a plain config body. |
| Happ itself | Never modified. `happd.service` and `happ-xray` keep their address and route. |

## What changes for you

- **Two clients, two TUN devices.** whoisthat runs its own Xray on its own TUN
  name (`tun-name`, default `whoisthattun`). It does not edit `Happ.conf` or talk
  to the `happd` socket. Run only one of them as your active tunnel.
- **Fail-closed by default when you enable the kill-switch.** With
  `kill-switch=true` an unexpected core exit leaves traffic blocked, not leaking.
  The rules survive `whoisthat core stop` on purpose.
- **Allowlisted resolvers keep working.** Port-53 traffic to the configured
  `dns-servers` is excluded from the DNS redirect, so those resolvers still
  answer if the core dies (K-4; hermetically tested, not yet proven on a host).
- **The kill-switch follows server IP changes.** If the server's address
  rotates while connected, the allowlist is re-armed in one atomic step, with no
  reconnect (K-5; hermetically tested, live rotation not yet proven on a host).
- **Reboots can be covered.** The optional boot unit arms the switch before the
  network comes up (K-14, below). Without it, the switch re-arms when the core
  starts in your session.
- **Subscription fetches are https-only**, and `nft`/`ip`/`xray` are resolved
  from fixed system paths, not your `PATH`.
- **Device id.** The subscription provider sees whoisthat's HWID, not Happ's.
  Check it with `whoisthat hwid status`.

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
- crypt5 `happy-link` URLs are not resolved by `sub add` yet (see status above).
- An empty or placeholder card list usually means a wrong or missing HWID:
  `whoisthat hwid status`.

### Happ stopped working

whoisthat does not touch it. Check `systemctl status happd`, confirm the
whoisthat kill-switch is off, and that only one of the two tunnels is active.
