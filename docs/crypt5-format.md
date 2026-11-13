# crypt5 payload format (reference implementation: hpwnr)

Supplied so the Go port does not guess. Derived from a working decryptor
(`Omegaplexx/hpwnr`, Rust, MIT — `src/lib.rs`, `src/keys.rs`) which decrypts real
Happ links; our panel emits the **crypt5** variant (`happ://crypt5/<payload>`).

Modes `crypt`..`crypt4` also exist (RSA PKCS#1 key chosen by mode ordinal). Only
crypt5 matters here.

## Keys

`src/keys.rs` holds the `CRYPT5_PKCS8` table: **36 8-character markers**, each mapping to
an RSA-4096 **PKCS#8** private key `("marker", "<base64 DER>")`. One marker (`qzmtapkx`)
is decrypt-only — no valid public half, so encryption must skip it, but decryption must
still accept it. The same file also carries `V2_KEYS` and `AES_GCM_KEYS` for other crypt
modes; crypt5 needs only the PKCS#8 set. Vendoring: copy those 36 verbatim; they are the
only non-reconstructable constant in the scheme. `core/core/lib/crypt5/keys_table.go` is
already generated from them.

## Decrypt, in order

Operate on the payload bytes **as written** in the marker (base64-decoded first if the
transport kept it encoded — see "transport" below).

1. `shuffled = block_pair_swap(payload)`: for every complete 4-byte block, swap
   `i↔i+2` then `i+1↔i+3`; a trailing partial block is left alone.
   (`swap_pairs`, used later, instead swaps every adjacent pair — self-inverse.)
2. Reject if `len < 8`. `marker = shuffled[0..4] ++ shuffled[n-4..n]` — first four
   plus last four characters, concatenated. Look the marker up; unknown marker is a
   hard error, not a fallback.
3. `body = shuffled[4..n-4]`; reject if `len(body) < 13`.
4. `nonce = body[0..12]`.
5. Layout select: `body[12]` **not** an ASCII digit ⇒ *salted* layout.
   - salted: `tag = body[12..14]` (2 chars, ignored on decrypt), `salt = body[14..22]`
     (8 chars), `len_start = 22`; reject if `len(body) < 22`.
   - legacy: no salt, `len_start = 12`.
   The reference tries the preferred layout, then the other if it fails — do the same
   so interrupted/older links still work.
6. From `body[len_start..]` read ASCII digits = decimal length `seg_len` of the
   ciphertext segment; one digit minimum. Then require one separator byte, `seg_len`
   bytes of `url_b64` ciphertext, and a non-empty remainder `rsa_cipher`.
7. `rsa_plain = RSA_PKCS1v15_decrypt(priv_key, b64decode(rsa_cipher))`;
   `rsa_value = url_safe_b64_decode(swap_pairs(rsa_plain))`. It must be exactly 32 bytes.
8. `chacha_key = rsa_value`, or, when salted, `chacha_key[i] = rsa_value[i] ^ salt[i % 8]`
   for `i` in 0..32.
9. `intermediate = ChaCha20Poly1305_decrypt(chacha_key, nonce, url_safe_b64_decode(url_b64))`
   (the AEAD tag is part of that ciphertext).
10. `plaintext = url_safe_b64_decode(swap_pairs(intermediate))` — the subscription or
    redirect URL, UTF-8.

Base64 in the payload is **URL-safe without padding**; the reference strips stray `=`
before decoding. RSA chunks are standard base64.

## Transport notes

- The payload arrives inside a `happ://crypt5/<payload>` Location, which is itself the
  target of a 302 from `…/happy-link/<id>`. `ParseMarker`/`ResolveMarker` already
  handle that; the AEAD step belongs behind them.
- The decrypted URL for this panel is `https://<host>/happy-accy/<same-id>`; fetching
  it needs `X-HWID: <16 hex>` and `UA: Happ/4.7.2/Desktop/341`, and returns the JSON
  config array that `xrayjson` already parses. No `X-HWID` yields placeholder cards.
- Crypt5 is re-minted on every refresh, so the decrypt runs per `profile-update-interval`
  tick, not once.

## Verifying a port

The reference repo has encrypt+decrypt round-trip tests; port a couple of its
vectors rather than trusting one live link, and keep one recorded payload plus its
expected URL as a fixture (a recorded payload is disposable, not a secret).
