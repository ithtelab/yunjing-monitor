package server

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionStoreCreateValidAndDelete(t *testing.T) {
	store := NewSessionStore()

	token, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("created session token is empty")
	}
	if !store.Valid(token) {
		t.Fatal("created session should be valid")
	}

	store.Delete(token)
	if store.Valid(token) {
		t.Fatal("deleted session should be invalid")
	}
}

func TestPersistentSessionStoreSurvivesReloadAndHashesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth-sessions.json")
	store, err := NewPersistentSessionStore(path, "stable-test-auth-secret")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateForDuration(SessionKindOwner, "owner-123", rememberedSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("persistent session file contains raw token")
	}

	reloaded, err := NewPersistentSessionStore(path, "stable-test-auth-secret")
	if err != nil {
		t.Fatal(err)
	}
	session, ok := reloaded.Get(token)
	if !ok || session.Kind != SessionKindOwner || session.Subject != "owner-123" {
		t.Fatalf("reloaded session = %+v ok=%v", session, ok)
	}
	reloaded.Delete(token)

	afterDelete, err := NewPersistentSessionStore(path, "stable-test-auth-secret")
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.Valid(token) {
		t.Fatal("deleted persistent session survived reload")
	}
}

func TestPersistentSessionStoreRejectsDifferentSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth-sessions.json")
	store, err := NewPersistentSessionStore(path, "first-auth-secret")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateFor(SessionKindAdmin, "admin")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewPersistentSessionStore(path, "different-auth-secret")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Valid(token) {
		t.Fatal("session signed with a different AUTH_SECRET should be invalid")
	}
}

func TestSessionStoreValidExpiresAndRemovesSession(t *testing.T) {
	store := NewSessionStore()
	store.sessions["expired"] = Session{
		Subject: "admin",
		Kind:    SessionKindAdmin,
		Expires: time.Now().Add(-time.Second),
	}

	if store.Valid("expired") {
		t.Fatal("expired session should be invalid")
	}
	if _, ok := store.sessions["expired"]; ok {
		t.Fatal("expired session should be removed")
	}
}

func TestSessionStoreCreateForOwner(t *testing.T) {
	store := NewSessionStore()
	token, err := store.CreateFor(SessionKindOwner, "owner-123")
	if err != nil {
		t.Fatal(err)
	}
	sess, ok := store.Get(token)
	if !ok {
		t.Fatal("owner session missing")
	}
	if sess.Kind != SessionKindOwner || sess.Subject != "owner-123" {
		t.Fatalf("session = %+v", sess)
	}
}

func TestSessionStoreDeleteForRevokesOnlyMatchingOwner(t *testing.T) {
	store := NewSessionStore()
	ownerToken, _ := store.CreateFor(SessionKindOwner, "owner-123")
	otherOwnerToken, _ := store.CreateFor(SessionKindOwner, "owner-456")
	adminToken, _ := store.CreateFor(SessionKindAdmin, "owner-123")
	if err := store.DeleteFor(SessionKindOwner, "owner-123"); err != nil {
		t.Fatal(err)
	}
	if store.Valid(ownerToken) {
		t.Fatal("matching owner session still valid")
	}
	if !store.Valid(otherOwnerToken) || !store.Valid(adminToken) {
		t.Fatal("unrelated session was revoked")
	}
}

func TestSessionStoreConcurrentCreateAndValidate(t *testing.T) {
	store := NewSessionStore()
	var wg sync.WaitGroup
	errs := make(chan error, 32)

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := store.Create()
			if err != nil {
				errs <- err
				return
			}
			if !store.Valid(token) {
				errs <- errSessionInvalid{}
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type errSessionInvalid struct{}

func (errSessionInvalid) Error() string { return "created session was invalid" }
