package crypt5

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// The vectors below are produced by encryptForTest, a test-only encoder that
// follows docs/crypt5-format.md in reverse. They prove the decoder and the
// documented layout agree; they do NOT prove agreement with a real Happ link.
// A recorded payload from the reference implementation is still wanted.

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func lookupOf(m map[string]*rsa.PrivateKey) KeyLookup {
	return func(marker string) (*rsa.PrivateKey, bool) { k, ok := m[marker]; return k, ok }
}

type encOpts struct {
	marker string // 8 chars
	salted bool
	tag    string // 2 non-digit chars when salted (digits force the layout fallback)
	salt   string // 8 chars when salted
	nonce  string // 12 chars
	sep    string // 1 char
}

func defaultOpts(salted bool) encOpts {
	return encOpts{marker: "abcd1234", salted: salted, tag: "xy", salt: "SaLtSaLt", nonce: "0123456789ab", sep: "."}
}

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func encryptForTest(t *testing.T, pub *rsa.PublicKey, plaintext string, o encOpts) string {
	t.Helper()
	chachaKey := make([]byte, 32)
	if _, err := rand.Read(chachaKey); err != nil {
		t.Fatal(err)
	}
	rsaValue := append([]byte(nil), chachaKey...)
	if o.salted {
		for i := range rsaValue {
			rsaValue[i] ^= o.salt[i%8]
		}
	}
	aead, err := chacha20poly1305.New(chachaKey)
	if err != nil {
		t.Fatal(err)
	}
	intermediate := swapPairs([]byte(b64u([]byte(plaintext))))
	urlB64 := b64u(aead.Seal(nil, []byte(o.nonce), intermediate, nil))

	rsaPlain := swapPairs([]byte(b64u(rsaValue)))
	rsaCipher, err := rsa.EncryptPKCS1v15(rand.Reader, pub, rsaPlain)
	if err != nil {
		t.Fatal(err)
	}

	body := o.nonce
	if o.salted {
		body += o.tag + o.salt
	}
	body += strconv.Itoa(len(urlB64)) + o.sep + urlB64 + base64.StdEncoding.EncodeToString(rsaCipher)
	shuffled := o.marker[:4] + body + o.marker[4:]
	return string(blockPairSwap([]byte(shuffled)))
}

func TestBlockPairSwapKnownAnswer(t *testing.T) {
	for in, want := range map[string]string{
		"abcd":       "cdab",
		"abcdefgh":   "cdabghef",
		"abcdef":     "cdabef", // trailing partial block untouched
		"abc":        "abc",
		"":           "",
		"0123456789": "2301674589",
	} {
		if got := string(blockPairSwap([]byte(in))); got != want {
			t.Errorf("blockPairSwap(%q) = %q, want %q", in, got, want)
		}
		if back := string(blockPairSwap(blockPairSwap([]byte(in)))); back != in {
			t.Errorf("blockPairSwap is not an involution for %q: %q", in, back)
		}
	}
}

func TestSwapPairsKnownAnswer(t *testing.T) {
	for in, want := range map[string]string{
		"abcd": "badc", "abcde": "badce", "a": "a", "": "",
	} {
		if got := string(swapPairs([]byte(in))); got != want {
			t.Errorf("swapPairs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	keys := lookupOf(map[string]*rsa.PrivateKey{"abcd1234": key})
	// Varied lengths so payload length hits every residue mod 4 (partial block).
	urls := []string{
		"https://panel.example/happy-accy/abc123",
		"https://panel.example/happy-accy/abc1234",
		"https://panel.example/happy-accy/abc12345",
		"https://panel.example/happy-accy/abc123456",
		"https://панель.example/путь?x=1&y=2",
	}
	for _, salted := range []bool{true, false} {
		for _, u := range urls {
			p := encryptForTest(t, &key.PublicKey, u, defaultOpts(salted))
			got, err := Decrypt(p, keys)
			if err != nil || got != u {
				t.Errorf("salted=%v len=%d: Decrypt = %q, %v; want %q", salted, len(p), got, err, u)
			}
		}
	}
}

func TestDecryptLayoutFallback(t *testing.T) {
	key := testKey(t)
	keys := lookupOf(map[string]*rsa.PrivateKey{"abcd1234": key})
	// A salted link whose tag is digits looks legacy at body[12]; the decoder
	// must retry the other layout instead of failing.
	o := defaultOpts(true)
	o.tag = "00"
	p := encryptForTest(t, &key.PublicKey, "https://panel.example/x", o)
	got, err := Decrypt(p, keys)
	if err != nil || got != "https://panel.example/x" {
		t.Fatalf("Decrypt = %q, %v", got, err)
	}
}

func TestDecryptUnknownMarkerIsHardError(t *testing.T) {
	key := testKey(t)
	p := encryptForTest(t, &key.PublicKey, "https://panel.example/x", defaultOpts(true))
	_, err := Decrypt(p, lookupOf(map[string]*rsa.PrivateKey{"zzzzzzzz": key}))
	if !errors.Is(err, ErrUnknownMarker) {
		t.Fatalf("err = %v, want ErrUnknownMarker (no fallback to another key)", err)
	}
}

func TestDecryptWrongKeyRejected(t *testing.T) {
	right, wrong := testKey(t), testKey(t)
	p := encryptForTest(t, &right.PublicKey, "https://panel.example/x", defaultOpts(true))
	got, err := Decrypt(p, lookupOf(map[string]*rsa.PrivateKey{"abcd1234": wrong}))
	if !errors.Is(err, ErrDecrypt) || got != "" {
		t.Fatalf("Decrypt = %q, %v; want ErrDecrypt and no plaintext", got, err)
	}
}

func TestDecryptTamperedCiphertextRejected(t *testing.T) {
	key := testKey(t)
	keys := lookupOf(map[string]*rsa.PrivateKey{"abcd1234": key})
	p := encryptForTest(t, &key.PublicKey, "https://panel.example/x", defaultOpts(true))
	// Work in the unshuffled domain: marker(4)+nonce(12)+tag/salt(10)+len+sep,
	// so offset 40 is inside the base64 ciphertext segment.
	sh := blockPairSwap([]byte(p))
	if sh[40] == 'A' {
		sh[40] = 'B'
	} else {
		sh[40] = 'A'
	}
	p = string(blockPairSwap(sh))
	if _, err := Decrypt(p, keys); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("err = %v, want ErrDecrypt (Poly1305 must reject tampering)", err)
	}
}

func TestDecryptMalformed(t *testing.T) {
	key := testKey(t)
	keys := lookupOf(map[string]*rsa.PrivateKey{"abcd1234": key})
	o := defaultOpts(true)
	// Marker resolves (abcd + 1234) but the body is too short / truncated.
	mk := func(body string) string {
		return string(blockPairSwap([]byte(o.marker[:4] + body + o.marker[4:])))
	}
	for name, p := range map[string]string{
		"empty":           "",
		"under 8":         "abcdefg",
		"body under 13":   mk("0123456789ab"),
		"salted under 22": mk("0123456789abxySaLt"),
		"no digits":       mk("0123456789abxySaLtSaLt" + "..."),
		"length overruns": mk("0123456789abxySaLtSaLt" + "999.AAAA" + "QUJD"),
		"no rsa tail":     mk("0123456789abxySaLtSaLt" + "4.AAAA"),
	} {
		if _, err := Decrypt(p, keys); err == nil {
			t.Errorf("%s: expected an error", name)
		} else if errors.Is(err, ErrUnknownMarker) && name != "empty" && name != "under 8" {
			t.Errorf("%s: err = %v; marker is known so this must be a malformed/decrypt error", name, err)
		}
	}
}

func TestDecryptPayloadAsWrittenInLink(t *testing.T) {
	key := testKey(t)
	keys := lookupOf(map[string]*rsa.PrivateKey{"abcd1234": key})
	p := encryptForTest(t, &key.PublicKey, "https://panel.example/happy-accy/id", defaultOpts(true))
	payload, err := ParseMarker("happ://crypt5/" + p + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(payload, keys); err != nil || !strings.HasSuffix(got, "/happy-accy/id") {
		t.Fatalf("Decrypt via ParseMarker = %q, %v", got, err)
	}
}

func TestVendoredTableWellFormed(t *testing.T) {
	if len(vendoredKeys) == 0 {
		t.Skip("vendored marker table is empty: copy it verbatim from the reference implementation")
	}
	if len(vendoredKeys) != 36 {
		t.Errorf("vendored table has %d markers, want 36", len(vendoredKeys))
	}
	lookup := DefaultKeys()
	for m := range vendoredKeys {
		if len(m) != 8 {
			t.Errorf("marker %q is not 8 chars", m)
		}
		if k, ok := lookup(m); !ok || k == nil || k.N.BitLen() != 4096 {
			t.Errorf("marker %q does not parse to an RSA-4096 key", m)
		}
	}
	if _, ok := lookup("qzmtapkx"); !ok {
		t.Error("decrypt-only marker qzmtapkx must still be accepted for decryption")
	}
}
