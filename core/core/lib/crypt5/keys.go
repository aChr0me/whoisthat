package crypt5

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"sync"
)

// vendoredKeys lives in keys_table.go (generated from the reference
// implementation's src/keys.rs, 36 markers — docs/crypt5-format.md). It is the
// one non-reconstructable constant of the scheme; "qzmtapkx" is decrypt-only.

var (
	parsedMu   sync.Mutex
	parsedKeys = map[string]*rsa.PrivateKey{}
)

// DefaultKeys resolves markers against vendoredKeys, parsing each key once.
func DefaultKeys() KeyLookup {
	return func(marker string) (*rsa.PrivateKey, bool) {
		parsedMu.Lock()
		defer parsedMu.Unlock()
		if k, ok := parsedKeys[marker]; ok {
			return k, true
		}
		enc, ok := vendoredKeys[marker]
		if !ok {
			return nil, false
		}
		der, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return nil, false
		}
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, false
		}
		k, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, false
		}
		parsedKeys[marker] = k
		return k, true
	}
}
