// Package identity provides Ed25519 key management and message signing for AgentExchange.
// Every outbound message can be signed; receivers verify using the sender's
// public key retrieved from the platform registry or Agent Card.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Identity holds an agent's Ed25519 key pair.
type Identity struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// New generates a fresh Ed25519 identity.
func New() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return &Identity{pub: pub, priv: priv}, nil
}

// FromPrivateKey loads an identity from a base64url-encoded private key seed.
func FromPrivateKey(b64 string) (*Identity, error) {
	seed, err := base64.URLEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return &Identity{pub: priv.Public().(ed25519.PublicKey), priv: priv}, nil
}

// PublicKeyBase64 returns the base64url-encoded public key (used in Agent Cards).
func (id *Identity) PublicKeyBase64() string {
	return base64.URLEncoding.EncodeToString(id.pub)
}

// PrivateKeySeedBase64 returns the base64url-encoded 32-byte private key seed.
// Store this securely; it allows full key reconstruction.
func (id *Identity) PrivateKeySeedBase64() string {
	return base64.URLEncoding.EncodeToString(id.priv.Seed())
}

// ─── Message Signing ─────────────────────────────────────────────────────────

// SigningInput is the canonical representation of a message that is signed.
// It is serialized as sorted-key JSON before signing.
type SigningInput struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Method  string `json:"method"`
	Nonce   string `json:"nonce"`
	TS      int64  `json:"ts"`
	Payload string `json:"payload"` // SHA-256 hex of canonical JSON params
}

// SignMessage produces a base64url Ed25519 signature over the signing input.
func (id *Identity) SignMessage(from, to, method string, paramsJSON []byte) (sig, nonce string, ts int64, err error) {
	nonce = uuid.New().String()
	ts = time.Now().UnixMilli()

	payloadHash := sha256.Sum256(paramsJSON)
	payloadHex := fmt.Sprintf("%x", payloadHash)

	input := SigningInput{
		From:    from,
		To:      to,
		Method:  method,
		Nonce:   nonce,
		TS:      ts,
		Payload: payloadHex,
	}

	canonical, err := canonicalJSON(input)
	if err != nil {
		return
	}

	sigBytes := ed25519.Sign(id.priv, canonical)
	sig = base64.URLEncoding.EncodeToString(sigBytes)
	return
}

// VerifyMessage verifies a signature produced by SignMessage.
func VerifyMessage(pubKeyB64, from, to, method, nonce string, ts int64, paramsJSON []byte, sigB64 string) error {
	pubBytes, err := base64.URLEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return fmt.Errorf("decode public key: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length %d", len(pubBytes))
	}

	payloadHash := sha256.Sum256(paramsJSON)
	payloadHex := fmt.Sprintf("%x", payloadHash)

	input := SigningInput{
		From:    from,
		To:      to,
		Method:  method,
		Nonce:   nonce,
		TS:      ts,
		Payload: payloadHex,
	}

	canonical, err := canonicalJSON(input)
	if err != nil {
		return err
	}

	sigBytes, err := base64.URLEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(pubBytes, canonical, sigBytes) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// ─── Quote Commitment Signing ─────────────────────────────────────────────────

// SignQuote produces a commitment signature over (quoteID, priceUSD, slaMS, expiresAt, pubkey).
func (id *Identity) SignQuote(quoteID string, priceUSD float64, slaMS, expiresAt int64) (string, error) {
	input := map[string]any{
		"quote_id":   quoteID,
		"price_usd":  priceUSD,
		"sla_ms":     slaMS,
		"expires_at": expiresAt,
		"pubkey":     id.PublicKeyBase64(),
	}
	canonical, err := canonicalJSON(input)
	if err != nil {
		return "", err
	}
	sigBytes := ed25519.Sign(id.priv, canonical)
	return base64.URLEncoding.EncodeToString(sigBytes), nil
}

// ─── Key File I/O ─────────────────────────────────────────────────────────────

// SaveToFile writes the private key seed to a file (permissions 0600).
func (id *Identity) SaveToFile(path string) error {
	return os.WriteFile(path, []byte(id.PrivateKeySeedBase64()), 0600)
}

// LoadFromFile reads a private key seed from a file written by SaveToFile.
func LoadFromFile(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return FromPrivateKey(string(data))
}

// LoadOrCreate loads a key from path if it exists, otherwise generates and saves a new one.
func LoadOrCreate(path string) (*Identity, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		id, err := New()
		if err != nil {
			return nil, err
		}
		if err := id.SaveToFile(path); err != nil {
			return nil, err
		}
		return id, nil
	}
	return LoadFromFile(path)
}

// ─── Canonical JSON ───────────────────────────────────────────────────────────

// canonicalJSON serializes v as JSON with keys sorted at every level.
// This ensures deterministic output for signing.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// round-trip through sorted map to ensure key order
	var m any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return marshalSorted(m)
}

func marshalSorted(v any) ([]byte, error) {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		buf := []byte{'{'}
		for i, k := range keys {
			kJSON, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			vJSON, err := marshalSorted(val[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, kJSON...)
			buf = append(buf, ':')
			buf = append(buf, vJSON...)
			if i < len(keys)-1 {
				buf = append(buf, ',')
			}
		}
		buf = append(buf, '}')
		return buf, nil

	case []any:
		buf := []byte{'['}
		for i, elem := range val {
			elemJSON, err := marshalSorted(elem)
			if err != nil {
				return nil, err
			}
			buf = append(buf, elemJSON...)
			if i < len(val)-1 {
				buf = append(buf, ',')
			}
		}
		buf = append(buf, ']')
		return buf, nil

	default:
		return json.Marshal(v)
	}
}
