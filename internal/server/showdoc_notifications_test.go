package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShowDocEventIsEncryptedQueuedAndRedacted(t *testing.T) {
	s, store := newPlatformTestServer(t)
	passwordHash, err := hashOwnerPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateOwner("notify@example.com", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateOwnerNode(owner.ID, PlannedNode{NodeID: "notify-node", Private: true}, HostInfo{}, "agent-token", accountAudit(owner.ID, "node.create", "notify-node"), 20, 3); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := s.features.cipher.encrypt("showdoc-push-token-1234")
	if err != nil {
		t.Fatal(err)
	}
	s.features.mu.Lock()
	s.features.data.NotificationBindings[owner.ID] = NotificationBinding{
		OwnerID: owner.ID, Provider: "showdoc", TokenCipher: ciphertext,
		TokenSuffix: "1234", Status: "bound", BoundAt: time.Now().Unix(),
	}
	s.features.data.NotificationPreferences[owner.ID] = defaultNotificationPreference(owner.ID)
	s.features.mu.Unlock()

	s.enqueueShowDocEvent("alert.firing", AlertEvent{ID: "alert-1", NodeID: "notify-node", NodeName: "Test Node", Message: "CPU high"})

	s.features.mu.RLock()
	if len(s.features.data.NotificationDeliveries) != 1 {
		t.Fatalf("deliveries = %d", len(s.features.data.NotificationDeliveries))
	}
	delivery := s.features.data.NotificationDeliveries[0]
	storedBinding := s.features.data.NotificationBindings[owner.ID]
	s.features.mu.RUnlock()
	if delivery.OwnerID != owner.ID || delivery.Event != "alert.firing" || delivery.Status != "pending" {
		t.Fatalf("delivery = %#v", delivery)
	}
	if storedBinding.TokenCipher == "showdoc-push-token-1234" {
		t.Fatal("ShowDoc token was stored in plaintext")
	}
	snapshot := s.features.Snapshot()
	if snapshot.NotificationBindings[owner.ID].TokenCipher != "" {
		t.Fatal("snapshot exposed encrypted ShowDoc token")
	}
	if snapshot.NotificationDeliveries[0].IdempotencyKey == "" {
		t.Fatal("persisted delivery lost idempotency key")
	}

	s.enqueueShowDocEvent("alert.firing", AlertEvent{ID: "alert-1", NodeID: "notify-node", NodeName: "Test Node", Message: "CPU high"})
	s.features.mu.RLock()
	defer s.features.mu.RUnlock()
	if len(s.features.data.NotificationDeliveries) != 1 {
		t.Fatalf("duplicate event queued %d deliveries", len(s.features.data.NotificationDeliveries))
	}
}

func TestNotificationQuietHoursAcrossMidnight(t *testing.T) {
	preference := NotificationPreference{QuietStart: "23:00", QuietEnd: "07:00", TimeZone: "UTC"}
	inside := time.Date(2026, 8, 16, 1, 30, 0, 0, time.UTC)
	until := notificationQuietUntil(inside, preference)
	if until.IsZero() || !until.Equal(time.Date(2026, 8, 16, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("quiet until = %v", until)
	}
	outside := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if value := notificationQuietUntil(outside, preference); !value.IsZero() {
		t.Fatalf("outside quiet hours returned %v", value)
	}
}

func TestAccountNotificationAPIRequiresLogin(t *testing.T) {
	s, _ := newPlatformTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "https://monitor.example.com/api/account/notifications", nil)
	resp := httptest.NewRecorder()
	s.handleAccountNotifications(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
}
