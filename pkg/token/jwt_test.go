package token

import (
	"testing"
	"time"
)

func TestGenerateAndVerifyToken(t *testing.T) {
	m := NewJWTManager("test-secret", 1, 7)
	tokenStr, err := m.GenerateToken(42, "alice", "ADMIN")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}
	claims, err := m.VerifyToken(tokenStr)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID = %d, want 42", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Errorf("Username = %q, want alice", claims.Username)
	}
	if claims.Role != "ADMIN" {
		t.Errorf("Role = %q, want ADMIN", claims.Role)
	}
	if !claims.ExpiresAt.After(time.Now()) {
		t.Error("token should not be expired")
	}
}

func TestVerifyTokenWrongSecret(t *testing.T) {
	m1 := NewJWTManager("secret-a", 1, 7)
	m2 := NewJWTManager("secret-b", 1, 7)
	tokenStr, err := m1.GenerateToken(1, "bob", "USER")
	if err != nil {
		t.Fatalf("GenerateToken error: %v", err)
	}
	if _, err := m2.VerifyToken(tokenStr); err == nil {
		t.Error("expected verification failure with wrong secret")
	}
}

func TestVerifyTokenGarbage(t *testing.T) {
	m := NewJWTManager("test-secret", 1, 7)
	if _, err := m.VerifyToken("not-a-jwt"); err == nil {
		t.Error("expected verification failure for garbage token")
	}
}
