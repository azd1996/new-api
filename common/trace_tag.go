package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Trace-tag decoding. The downstream gateway tags each outbound request with the
// header TraceTagHeader, whose value encodes the plaintext
// `downstream_request_id + "|" + customer_api_key_id`. The construction is SIV
// (synthetic IV): iv = HMAC-SHA256(macKey, plaintext)[:16], then
// AES-256-CTR(encKey, iv, plaintext); the iv doubles as the authentication tag.
// We only ever decode (we are the receiver), so Encode is intentionally absent.
const (
	// TraceTagHeader is the inbound header the downstream gateway sets.
	TraceTagHeader = "x-bce-aigw-trace-tag"
	// TraceTagKeyEnv holds the 64-hex (32-byte) master key used to decode the
	// header. HKDF-SHA256 derives separate MAC and encryption subkeys from it.
	TraceTagKeyEnv = "AIGW_TRACE_TAG_KEY"

	traceTagSIVSize = 16
	traceTagKeySize = 32
)

// traceTagEncoding is RFC 4648 standard base32 without padding: single-case, so
// it survives any case normalization the downstream link may apply.
var traceTagEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// TraceTagKeys holds the two subkeys derived from the master key. The SIV
// construction requires distinct keys for MAC and encryption.
type TraceTagKeys struct {
	mac []byte
	enc cipher.Block
}

var (
	traceTagOnce sync.Once
	traceTagKeys *TraceTagKeys
)

// GetTraceTagKeys returns the process-wide trace-tag keys derived from
// AIGW_TRACE_TAG_KEY, or nil when the env var is unset or invalid (decoding is
// then skipped). The master key is read and derived exactly once.
func GetTraceTagKeys() *TraceTagKeys {
	traceTagOnce.Do(func() {
		hexKey := strings.TrimSpace(os.Getenv(TraceTagKeyEnv))
		if hexKey == "" {
			return
		}
		keys, err := newTraceTagKeys(hexKey)
		if err != nil {
			SysError("invalid " + TraceTagKeyEnv + ": " + err.Error())
			return
		}
		traceTagKeys = keys
	})
	return traceTagKeys
}

func newTraceTagKeys(hexKey string) (*TraceTagKeys, error) {
	master, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("key is not valid hex: %w", err)
	}
	if len(master) != traceTagKeySize {
		return nil, fmt.Errorf("key must be %d bytes, got %d", traceTagKeySize, len(master))
	}
	mac, err := hkdf.Key(sha256.New, master, nil, "aigw trace-tag mac", 32)
	if err != nil {
		return nil, fmt.Errorf("derive mac key: %w", err)
	}
	encKey, err := hkdf.Key(sha256.New, master, nil, "aigw trace-tag enc", 32)
	if err != nil {
		return nil, fmt.Errorf("derive enc key: %w", err)
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	return &TraceTagKeys{mac: mac, enc: block}, nil
}

func (k *TraceTagKeys) siv(plaintext []byte) []byte {
	m := hmac.New(sha256.New, k.mac)
	m.Write(plaintext)
	return m.Sum(nil)[:traceTagSIVSize]
}

// Decode reverses the downstream gateway's encoding and returns the downstream
// request id and customer (api key) id. The input is upper-cased and trimmed to
// tolerate any case normalization upstream. Authentication (SIV/HMAC) must pass
// or an error is returned; the customer id may legitimately be empty (the
// downstream gateway's public mode).
func (k *TraceTagKeys) Decode(tag string) (requestID, customerID string, err error) {
	blob, err := traceTagEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(tag)))
	if err != nil {
		return "", "", fmt.Errorf("base32 decode: %w", err)
	}
	if len(blob) < traceTagSIVSize {
		return "", "", errors.New("trace tag too short")
	}
	iv, ciphertext := blob[:traceTagSIVSize], blob[traceTagSIVSize:]
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCTR(k.enc, iv).XORKeyStream(plaintext, ciphertext)
	// Re-derive the SIV from the decrypted plaintext and compare in constant
	// time: this step is the authentication.
	if !hmac.Equal(iv, k.siv(plaintext)) {
		return "", "", errors.New("trace tag authentication failed")
	}
	parts := strings.SplitN(string(plaintext), "|", 3)
	if len(parts) < 2 {
		return "", "", errors.New("malformed trace tag plaintext")
	}
	return parts[0], parts[1], nil
}
