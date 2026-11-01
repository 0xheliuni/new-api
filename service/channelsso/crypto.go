package channelsso

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"

	"github.com/QuantumNous/new-api/common"
)

// hkdfInfo is the HKDF context label for keys derived for this feature. It
// scopes the derived key to Channel SSO so it cannot be reused (or confused
// with) keys other features derive from the same common.CryptoSecret.
const hkdfInfo = "channel-sso-v1"

// aesKeySize is the AES-256 key length derived via HKDF-SHA256.
const aesKeySize = 32

// errSealedCiphertextInvalid is returned by Open for any malformed,
// truncated, or tampered input, without distinguishing the cause — doing so
// would leak information useful to an attacker probing stored ciphertexts.
var errSealedCiphertextInvalid = errors.New("channel sso: sealed ciphertext is invalid")

// sealingKey derives the AES-256-GCM key used by Seal and Open from
// common.CryptoSecret via HKDF-SHA256, labeled with hkdfInfo. Deriving a
// feature-scoped key, rather than using common.CryptoSecret directly,
// keeps upstream credential ciphertexts independent of any other use of
// that secret.
func sealingKey() ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(common.CryptoSecret), nil, hkdfInfo, aesKeySize)
}

// Seal encrypts plaintext with AES-256-GCM under the key derived from
// common.CryptoSecret, using a fresh random nonce each call, and returns
// base64url(nonce||ciphertext). Both upstream channel passwords and cookie
// jars are secrets that MUST only ever be stored sealed; never log or
// return plaintext anywhere.
func Seal(plaintext []byte) (string, error) {
	key, err := sealingKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.URLEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal: it decodes base64url(nonce||ciphertext), decrypts
// with the key derived from common.CryptoSecret, and authenticates the
// result. Any decoding, length, or authentication failure (including
// tampering) returns errSealedCiphertextInvalid without further detail.
func Open(sealed string) ([]byte, error) {
	raw, err := base64.URLEncoding.DecodeString(sealed)
	if err != nil {
		return nil, errSealedCiphertextInvalid
	}
	key, err := sealingKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return nil, errSealedCiphertextInvalid
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errSealedCiphertextInvalid
	}
	return plaintext, nil
}
