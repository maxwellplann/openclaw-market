package market

import "testing"

func TestSessionManager(t *testing.T) {
	m, err := NewSessionManager()
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	token := m.Sign(42)
	userID, err := m.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if userID != 42 {
		t.Fatalf("userID = %d, want 42", userID)
	}
}
