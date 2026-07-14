package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// Create temp key file
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	km, err := NewKeyManager(keyPath)
	if err != nil {
		t.Fatalf("Failed to create KeyManager: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"simple", "hello world"},
		{"with spaces", "sk-12345-abcde-XYZ"},
		{"empty", ""},
		{"long key", "sk-proj-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"},
		{"special chars", "sk-!@#$%^&*()_+-=[]{}|;':\",./<>?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := km.Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			if tt.plaintext == "" {
				// Empty strings are now also encrypted to distinguish from unencrypted values
				if !IsEncrypted(encrypted) {
					t.Errorf("Empty plaintext should be encrypted")
				}
				decrypted, err := km.Decrypt(encrypted)
				if err != nil {
					t.Fatalf("Decrypt empty failed: %v", err)
				}
				if decrypted != "" {
					t.Errorf("Decrypted empty got %q, want empty", decrypted)
				}
				return
			}

			if !IsEncrypted(encrypted) {
				t.Errorf("Encrypted value should have 'enc:' prefix")
			}

			decrypted, err := km.Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if decrypted != tt.plaintext {
				t.Errorf("Decrypted = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestDecryptPlaintext(t *testing.T) {
	// Create temp key file
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	km, err := NewKeyManager(keyPath)
	if err != nil {
		t.Fatalf("Failed to create KeyManager: %v", err)
	}

	// Test that plain text values are returned as-is (backward compatibility)
	plaintext := "sk-plain-api-key"
	decrypted, err := km.Decrypt(plaintext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Plain text should be returned as-is: got %q, want %q", decrypted, plaintext)
	}
}

func TestKeyPersistence(t *testing.T) {
	// Create temp key file
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "test.key")

	// Create first KeyManager
	km1, err := NewKeyManager(keyPath)
	if err != nil {
		t.Fatalf("Failed to create first KeyManager: %v", err)
	}

	// Encrypt a value
	plaintext := "secret-api-key"
	encrypted1, err := km1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Create second KeyManager (should load existing key)
	km2, err := NewKeyManager(keyPath)
	if err != nil {
		t.Fatalf("Failed to create second KeyManager: %v", err)
	}

	// Decrypt with second KeyManager
	decrypted, err := km2.Decrypt(encrypted1)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestGlobalKeyManager(t *testing.T) {
	// Create temp key file
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "global.key")

	// Override default path for testing
	originalPath := DefaultKeyPath()
	defer func() {
		globalKeyManager = nil
		// Restore original path concept by resetting global
	}()

	_ = originalPath // Avoid unused variable warning

	// Set up test environment
	os.Setenv("API_SWITCH_KEY_PATH", keyPath)
	defer os.Unsetenv("API_SWITCH_KEY_PATH")

	// Test global encrypt/decrypt
	plaintext := "global-test-key"
	encrypted, err := EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	decrypted, err := DecryptString(encrypted)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted = %q, want %q", decrypted, plaintext)
	}
}
