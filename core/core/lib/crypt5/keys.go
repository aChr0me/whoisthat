package crypt5

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"sync"
)

// vendoredKeys maps an 8-character marker to a base64 PKCS#8 DER RSA-4096
// private key. The scheme has 128 of them (docs/crypt5-format.md); marker
// "qzmtapkx" is decrypt-only.
//
// THIS TABLE IS EMPTY. It is the one non-reconstructable constant of the
// scheme, it is not in this repository, and it has to be copied verbatim from
// the reference implementation (src/keys.rs) by someone with access to it.
// Until then DefaultKeys() resolves nothing and every real link fails with
// ErrUnknownMarker. TestVendoredTableWellFormed starts enforcing the shape
// (128 entries, RSA-4096, qzmtapkx present) as soon as entries are added.
var vendoredKeys = map[string]string{
	// "marker": "<base64 DER>",
}

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
