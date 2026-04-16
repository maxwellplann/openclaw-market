package market

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SessionManager struct {
	secret []byte
}

func NewSessionManager() (*SessionManager, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("session secret: %w", err)
	}
	return &SessionManager{secret: secret}, nil
}

func (m *SessionManager) Sign(userID int64) string {
	ts := time.Now().Unix()
	payload := strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := payload + ":" + signature
	return base64.RawURLEncoding.EncodeToString([]byte(token))
}

func (m *SessionManager) Verify(token string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, errors.New("decode session")
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 {
		return 0, errors.New("invalid session")
	}

	payload := parts[0] + ":" + parts[1]
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	expected := mac.Sum(nil)

	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, errors.New("invalid signature")
	}
	if !hmac.Equal(expected, got) {
		return 0, errors.New("signature mismatch")
	}

	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, errors.New("invalid user id")
	}
	return userID, nil
}
