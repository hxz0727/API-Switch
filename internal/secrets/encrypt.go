package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// KeyManager manages encryption keys for API key storage.
type KeyManager struct {
	keyPath string
	key     []byte
}

// NewKeyManager creates a new KeyManager with the given key file path.
func NewKeyManager(keyPath string) (*KeyManager, error) {
	km := &KeyManager{keyPath: keyPath}
	if err := km.loadOrGenerateKey(); err != nil {
		return nil, err
	}
	return km, nil
}

// DefaultKeyPath returns the default path for the encryption key file.
func DefaultKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".api-switch-key"
	}
	return filepath.Join(home, ".api-switch", ".key")
}

// loadOrGenerateKey loads the encryption key from file or generates a new one.
func (km *KeyManager) loadOrGenerateKey() error {
	// Ensure directory exists
	dir := filepath.Dir(km.keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create key directory: %w", err)
	}

	// Try to load existing key
	data, err := os.ReadFile(km.keyPath)
	if err == nil {
		if len(data) == 32 {
			km.key = data
			return nil
		}
		// Key file exists but is corrupted (wrong size) — do NOT silently overwrite
		return fmt.Errorf("encryption key file %s is corrupted: expected 32 bytes, got %d. "+
			"Remove it to regenerate: rm %s", km.keyPath, len(data), km.keyPath)
	}

	// Key file does not exist — generate new 256-bit key
	km.key = make([]byte, 32)
	if _, err := rand.Read(km.key); err != nil {
		return fmt.Errorf("cannot generate key: %w", err)
	}

	// Save with restrictive permissions
	if err := os.WriteFile(km.keyPath, km.key, 0600); err != nil {
		return fmt.Errorf("cannot save key: %w", err)
	}

	return nil
}

// Encrypt encrypts a plaintext string and returns a hex-encoded ciphertext.
func (km *KeyManager) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(km.key)
	if err != nil {
		return "", fmt.Errorf("cannot create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cannot create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("cannot generate nonce: %w", err)
	}

	// Encrypt and seal
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a hex-encoded ciphertext string.
// Returns the original plaintext, or the original string if not encrypted.
func (km *KeyManager) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Check if this is an encrypted value (prefix "enc:")
	if len(ciphertext) < 4 || ciphertext[:4] != "enc:" {
		// Not encrypted, return as-is (plain text for backward compatibility)
		return ciphertext, nil
	}

	// Decode hex
	data, err := hex.DecodeString(ciphertext[4:])
	if err != nil {
		return "", fmt.Errorf("cannot decode hex: %w", err)
	}

	block, err := aes.NewCipher(km.key)
	if err != nil {
		return "", fmt.Errorf("cannot create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cannot create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", fmt.Errorf("cannot decrypt: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a string is encrypted (has the "enc:" prefix).
func IsEncrypted(s string) bool {
	return len(s) > 4 && s[:4] == "enc:"
}

// Global key manager instance
var (
	globalKeyManager *KeyManager
	initOnce         sync.Once
	initErr          error
)

// InitGlobalKeyManager initializes the global key manager.
func InitGlobalKeyManager() error {
	initOnce.Do(func() {
		globalKeyManager, initErr = NewKeyManager(DefaultKeyPath())
	})
	return initErr
}

// EncryptString encrypts a string using the global key manager.
func EncryptString(plaintext string) (string, error) {
	if err := InitGlobalKeyManager(); err != nil {
		return "", err
	}
	return globalKeyManager.Encrypt(plaintext)
}

// DecryptString decrypts a string using the global key manager.
func DecryptString(ciphertext string) (string, error) {
	if err := InitGlobalKeyManager(); err != nil {
		return "", err
	}
	return globalKeyManager.Decrypt(ciphertext)
}
