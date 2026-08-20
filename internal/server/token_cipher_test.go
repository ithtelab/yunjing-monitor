package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenCipherRoundTripAndTamperResistance(t *testing.T) {
	cipher, err := newTokenCipher("a-test-auth-secret-that-is-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cipher.encrypt("agent-token-value")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, encryptedTokenPrefix) || strings.Contains(sealed, "agent-token-value") {
		t.Fatalf("token was not encrypted: %q", sealed)
	}
	plain, err := cipher.decrypt(sealed)
	if err != nil || plain != "agent-token-value" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	if _, err := cipher.decrypt(sealed[:len(sealed)-1] + "x"); err == nil {
		t.Fatal("tampered token decrypted successfully")
	}
}

func TestNodeExportDoesNotContainReversibleToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	store, err := NewStore(path, "a-test-auth-secret-that-is-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetNodeToken("CN-token-export", "agent-token-value", hashToken("agent-token-value"), 10); err != nil {
		t.Fatal(err)
	}
	backup, err := json.Marshal(store.ExportNodes())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(backup), "agent-token-value") {
		t.Fatalf("plaintext token leaked into export: %s", backup)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "agent-token-value") {
		t.Fatalf("plaintext token leaked into JSON store: %s", raw)
	}
}
