package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestRSAKeyPair(t *testing.T) (privPath, pubPath string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	dir := t.TempDir()
	privPath = filepath.Join(dir, "private.pem")
	pubPath = filepath.Join(dir, "public.pem")

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	return privPath, pubPath
}

func TestIssueAndVerifyAccessToken_RS256(t *testing.T) {
	privPath, pubPath := writeTestRSAKeyPair(t)
	issuer, err := NewRS256Issuer(privPath, pubPath)
	if err != nil {
		t.Fatalf("NewRS256Issuer: %v", err)
	}

	token, err := issuer.IssueAccessToken("user-1", "admin", "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.UserID != "user-1" || claims.Role != "admin" || claims.JTI != "jti-1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestIssueAndVerifyAccessTokenForSession_IncludesIndependentSessionID(t *testing.T) {
	privPath, pubPath := writeTestRSAKeyPair(t)
	issuer, err := NewRS256Issuer(privPath, pubPath)
	if err != nil {
		t.Fatalf("NewRS256Issuer: %v", err)
	}

	token, err := issuer.IssueAccessTokenForSession("user-1", "admin", "jti-1", "session-1")
	if err != nil {
		t.Fatalf("IssueAccessTokenForSession: %v", err)
	}

	claims, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if claims.JTI != "jti-1" || claims.SessionID != "session-1" {
		t.Fatalf("token/session identifiers = %q/%q, want jti-1/session-1", claims.JTI, claims.SessionID)
	}
}

func TestVerifyAccessToken_Expired(t *testing.T) {
	privPath, pubPath := writeTestRSAKeyPair(t)
	issuer, err := NewRS256Issuer(privPath, pubPath)
	if err != nil {
		t.Fatalf("NewRS256Issuer: %v", err)
	}

	start := time.Now()
	issuer.nowFunc = func() time.Time { return start }

	token, err := issuer.IssueAccessToken("user-1", "admin", "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	issuer.nowFunc = func() time.Time { return start.Add(AccessTokenTTL + time.Minute) }

	if _, err := issuer.VerifyAccessToken(token); err == nil {
		t.Fatal("VerifyAccessToken succeeded for expired token")
	}
}

func TestVerifyAccessToken_WrongSignature(t *testing.T) {
	privPathA, pubPathA := writeTestRSAKeyPair(t)
	issuerA, err := NewRS256Issuer(privPathA, pubPathA)
	if err != nil {
		t.Fatalf("NewRS256Issuer A: %v", err)
	}

	token, err := issuerA.IssueAccessToken("user-1", "admin", "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	privPathB, pubPathB := writeTestRSAKeyPair(t)
	issuerB, err := NewRS256Issuer(privPathB, pubPathB)
	if err != nil {
		t.Fatalf("NewRS256Issuer B: %v", err)
	}

	if _, err := issuerB.VerifyAccessToken(token); err == nil {
		t.Fatal("VerifyAccessToken succeeded for token signed by a different key")
	}
}

func TestKeyRotation_OldKidValidWithinGracePeriod(t *testing.T) {
	privPath, pubPath := writeTestRSAKeyPair(t)
	issuer, err := NewRS256Issuer(privPath, pubPath)
	if err != nil {
		t.Fatalf("NewRS256Issuer: %v", err)
	}

	start := time.Now()
	issuer.nowFunc = func() time.Time { return start }

	oldToken, err := issuer.IssueAccessToken("user-1", "admin", "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	newPrivPath, newPubPath := writeTestRSAKeyPair(t)
	if err := issuer.RotateRS256Key(newPrivPath, newPubPath); err != nil {
		t.Fatalf("RotateRS256Key: %v", err)
	}

	newToken, err := issuer.IssueAccessToken("user-2", "analyst", "jti-2")
	if err != nil {
		t.Fatalf("IssueAccessToken (post-rotation): %v", err)
	}

	// Still within the grace period: old kid must still verify.
	issuer.nowFunc = func() time.Time { return start.Add(5 * time.Minute) }
	if _, err := issuer.VerifyAccessToken(oldToken); err != nil {
		t.Fatalf("VerifyAccessToken(oldToken) within grace period failed: %v", err)
	}
	if _, err := issuer.VerifyAccessToken(newToken); err != nil {
		t.Fatalf("VerifyAccessToken(newToken) failed: %v", err)
	}

	// Past the grace period: old kid must be rejected.
	issuer.nowFunc = func() time.Time { return start.Add(KeyRotationGracePeriod + time.Minute) }
	if _, err := issuer.VerifyAccessToken(oldToken); err == nil {
		t.Fatal("VerifyAccessToken(oldToken) succeeded after grace period elapsed")
	}
}

func TestNewHS256Issuer_RequiresSecret(t *testing.T) {
	if _, err := NewHS256Issuer(""); err == nil {
		t.Fatal("NewHS256Issuer(\"\") succeeded, want error")
	}

	issuer, err := NewHS256Issuer("dev-only-secret")
	if err != nil {
		t.Fatalf("NewHS256Issuer: %v", err)
	}

	token, err := issuer.IssueAccessToken("user-1", "viewer", "jti-1")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if _, err := issuer.VerifyAccessToken(token); err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
}
