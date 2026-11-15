// Package crypt5test is the encrypt half of the crypt5 scheme, for tests only.
// It lives in its own package so the production binary never links an encoder;
// import it from _test.go files exclusively. It follows docs/crypt5-format.md
// in reverse and is deliberately independent of the decoder's helpers, so a
// shared bug cannot make a round trip pass.
package crypt5test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"strconv"

	"golang.org/x/crypto/chacha20poly1305"

	"whoisthat-core/lib/crypt5"
)

// OrphanMarker is the decrypt-only entry of the vendored table: it has no valid
// public half, so it must never be chosen as an encryption key.
const OrphanMarker = "qzmtapkx"

// VendoredMarker is a marker of the vendored table that is safe to encrypt with.
const VendoredMarker = "asajzqxt"

const (
	nonce = "0123456789ab"
	tag   = "xy"
	salt  = "SaLtSaLt"
)

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func swapPairs(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := 0; i+2 <= len(out); i += 2 {
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

func blockPairSwap(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := 0; i+4 <= len(out); i += 4 {
		out[i], out[i+2] = out[i+2], out[i]
		out[i+1], out[i+3] = out[i+3], out[i+1]
	}
	return out
}

// Encrypt produces a salted-layout crypt5 payload for plaintext under marker
// (8 characters) and pub.
func Encrypt(pub *rsa.PublicKey, marker, plaintext string) (string, error) {
	return encrypt(pub, marker, plaintext, false)
}

// EncryptCorruptAEAD is Encrypt with one ciphertext bit flipped after sealing,
// so the RSA unwrap succeeds and only the AEAD authentication fails.
func EncryptCorruptAEAD(pub *rsa.PublicKey, marker, plaintext string) (string, error) {
	return encrypt(pub, marker, plaintext, true)
}

func encrypt(pub *rsa.PublicKey, marker, plaintext string, corrupt bool) (string, error) {
	if len(marker) != 8 {
		return "", fmt.Errorf("crypt5test: marker must be 8 characters")
	}
	if marker == OrphanMarker {
		return "", fmt.Errorf("crypt5test: %s is decrypt-only", OrphanMarker)
	}
	chachaKey := make([]byte, 32)
	if _, err := rand.Read(chachaKey); err != nil {
		return "", err
	}
	rsaValue := append([]byte(nil), chachaKey...)
	for i := range rsaValue {
		rsaValue[i] ^= salt[i%8]
	}
	aead, err := chacha20poly1305.New(chachaKey)
	if err != nil {
		return "", err
	}
	intermediate := swapPairs([]byte(b64u([]byte(plaintext))))
	sealed := aead.Seal(nil, []byte(nonce), intermediate, nil)
	if corrupt {
		sealed[0] ^= 0x01
	}
	urlB64 := b64u(sealed)
	rsaCipher, err := rsa.EncryptPKCS1v15(rand.Reader, pub, swapPairs([]byte(b64u(rsaValue))))
	if err != nil {
		return "", err
	}
	body := nonce + tag + salt + strconv.Itoa(len(urlB64)) + "." + urlB64 + base64.StdEncoding.EncodeToString(rsaCipher)
	shuffled := marker[:4] + body + marker[4:]
	return string(blockPairSwap([]byte(shuffled))), nil
}

// EncryptVendored encrypts under VendoredMarker using the public half of the
// vendored table's private key, so crypt5.Decrypt(…, crypt5.DefaultKeys())
// opens it offline.
func EncryptVendored(plaintext string) (string, error) {
	key, ok := crypt5.DefaultKeys()(VendoredMarker)
	if !ok {
		return "", fmt.Errorf("crypt5test: %s missing from the vendored table", VendoredMarker)
	}
	return Encrypt(&key.PublicKey, VendoredMarker, plaintext)
}

// EncryptVendoredCorruptAEAD is EncryptVendored with a corrupted AEAD segment.
func EncryptVendoredCorruptAEAD(plaintext string) (string, error) {
	key, ok := crypt5.DefaultKeys()(VendoredMarker)
	if !ok {
		return "", fmt.Errorf("crypt5test: %s missing from the vendored table", VendoredMarker)
	}
	return EncryptCorruptAEAD(&key.PublicKey, VendoredMarker, plaintext)
}
