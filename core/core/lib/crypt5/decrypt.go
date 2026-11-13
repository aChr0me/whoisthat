package crypt5

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

var (
	ErrUnknownMarker = errors.New("crypt5: unknown key marker")
	ErrMalformed     = errors.New("crypt5: malformed payload")
	ErrDecrypt       = errors.New("crypt5: decryption failed")
)

// KeyLookup resolves an 8-character marker to its RSA private key.
type KeyLookup func(marker string) (*rsa.PrivateKey, bool)

// blockPairSwap swaps i<->i+2 then i+1<->i+3 in every complete 4-byte block;
// a trailing partial block is left alone. It is its own inverse.
func blockPairSwap(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := 0; i+4 <= len(out); i += 4 {
		out[i], out[i+2] = out[i+2], out[i]
		out[i+1], out[i+3] = out[i+3], out[i+1]
	}
	return out
}

// swapPairs swaps every adjacent byte pair; an odd trailing byte stays. It is
// its own inverse.
func swapPairs(b []byte) []byte {
	out := append([]byte(nil), b...)
	for i := 0; i+2 <= len(out); i += 2 {
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

// b64URL decodes URL-safe base64 without padding, tolerating stray '='.
func b64URL(b []byte) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(string(b), "="))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// Decrypt unwraps a crypt5 payload (the token from happ://crypt5/<payload>)
// into the subscription or redirect URL it carries, following
// docs/crypt5-format.md. An unknown marker is a hard error: no other key is
// tried. Of the salted and legacy body layouts the one indicated by body[12]
// is tried first and the other is the fallback.
func Decrypt(payload string, keys KeyLookup) (string, error) {
	shuffled := blockPairSwap([]byte(payload))
	n := len(shuffled)
	if n < 8 {
		return "", fmt.Errorf("%w: payload shorter than 8 bytes", ErrMalformed)
	}
	marker := string(shuffled[:4]) + string(shuffled[n-4:])
	key, ok := keys(marker)
	if !ok || key == nil {
		return "", fmt.Errorf("%w %q", ErrUnknownMarker, marker)
	}
	body := shuffled[4 : n-4]
	if len(body) < 13 {
		return "", fmt.Errorf("%w: body shorter than 13 bytes", ErrMalformed)
	}

	preferSalted := !isDigit(body[12])
	var firstErr error
	for _, salted := range []bool{preferSalted, !preferSalted} {
		out, err := decryptBody(body, key, salted)
		if err == nil {
			return out, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return "", firstErr
}

func decryptBody(body []byte, key *rsa.PrivateKey, salted bool) (string, error) {
	nonce := body[:12]
	lenStart := 12
	var salt []byte
	if salted {
		if len(body) < 22 {
			return "", fmt.Errorf("%w: salted body shorter than 22 bytes", ErrMalformed)
		}
		salt = body[14:22] // body[12:14] is a tag, ignored
		lenStart = 22
	}

	i := lenStart
	for i < len(body) && isDigit(body[i]) {
		i++
	}
	if i == lenStart {
		return "", fmt.Errorf("%w: no segment length digits", ErrMalformed)
	}
	segLen, err := strconv.Atoi(string(body[lenStart:i]))
	if err != nil || segLen < 1 {
		return "", fmt.Errorf("%w: bad segment length", ErrMalformed)
	}
	i++ // separator byte
	if i > len(body) || segLen > len(body)-i {
		return "", fmt.Errorf("%w: segment overruns payload", ErrMalformed)
	}
	urlB64 := body[i : i+segLen]
	rsaCipher := body[i+segLen:]
	if len(rsaCipher) == 0 {
		return "", fmt.Errorf("%w: empty RSA segment", ErrMalformed)
	}

	rsaRaw, err := base64.StdEncoding.DecodeString(string(rsaCipher))
	if err != nil {
		return "", fmt.Errorf("%w: RSA segment is not base64", ErrMalformed)
	}
	rsaPlain, err := rsa.DecryptPKCS1v15(nil, key, rsaRaw)
	if err != nil {
		return "", fmt.Errorf("%w: RSA unwrap", ErrDecrypt)
	}
	chachaKey, err := b64URL(swapPairs(rsaPlain))
	if err != nil || len(chachaKey) != chacha20poly1305.KeySize {
		return "", fmt.Errorf("%w: RSA plaintext is not a 32-byte key", ErrDecrypt)
	}
	if salted {
		for j := range chachaKey {
			chachaKey[j] ^= salt[j%8]
		}
	}

	aead, err := chacha20poly1305.New(chachaKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDecrypt, err)
	}
	ct, err := b64URL(urlB64)
	if err != nil {
		return "", fmt.Errorf("%w: ciphertext is not base64", ErrMalformed)
	}
	intermediate, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("%w: AEAD authentication", ErrDecrypt)
	}
	plain, err := b64URL(swapPairs(intermediate))
	if err != nil {
		return "", fmt.Errorf("%w: inner plaintext is not base64", ErrDecrypt)
	}
	return string(plain), nil
}
