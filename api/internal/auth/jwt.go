package auth

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const AccessTokenTTL = 15 * time.Minute
const RefreshTokenTTL = 7 * 24 * time.Hour

// KeyRotationGracePeriod is how long a retired signing key's kid remains
// acceptable for verification after rotation. It matches AccessTokenTTL: a
// token issued the instant before rotation must still be verifiable for the
// remainder of its own lifetime (auth.md §2).
const KeyRotationGracePeriod = 15 * time.Minute

// Claims is the JWT access token payload (auth.md §2).
type Claims struct {
	UserID string `json:"sub"`
	Role   string `json:"role"`
	JTI    string `json:"jti"`
	jwt.RegisteredClaims
}

// verifyKeyEntry is a verification key for a given kid. expiresAt is nil for
// the currently active signing key, and set to the rotation grace deadline
// for retired keys.
type verifyKeyEntry struct {
	key       any
	expiresAt *time.Time
}

// TokenIssuer issues and verifies access tokens, either with RS256 (production,
// key pair from files) or HS256 (development only, MERLON_JWT_SECRET).
type TokenIssuer struct {
	mu         sync.RWMutex
	method     jwt.SigningMethod
	currentKid string
	signingKey any // *rsa.PrivateKey (RS256) or []byte (HS256)
	verifyKeys map[string]verifyKeyEntry
	nowFunc    func() time.Time
}

// NewRS256Issuer builds an RS256 issuer from a private/public key file pair.
// kid is derived from the SHA-256 fingerprint of the public key.
func NewRS256Issuer(privateKeyPath, publicKeyPath string) (*TokenIssuer, error) {
	privData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	pubData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}

	priv, err := parseRSAPrivateKeyPEM(privData)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	pub, err := parseRSAPublicKeyPEM(pubData)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	kid, err := fingerprintRSAPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("fingerprint public key: %w", err)
	}

	return &TokenIssuer{
		method:     jwt.SigningMethodRS256,
		currentKid: kid,
		signingKey: priv,
		verifyKeys: map[string]verifyKeyEntry{kid: {key: pub}},
	}, nil
}

// RotateRS256Key introduces a new RS256 key pair as the active signing key.
// The previous key remains valid for verification for KeyRotationGracePeriod.
func (t *TokenIssuer) RotateRS256Key(privateKeyPath, publicKeyPath string) error {
	if t.method != jwt.SigningMethodRS256 {
		return errors.New("key rotation only supported for RS256 issuers")
	}

	privData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}
	pubData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}

	priv, err := parseRSAPrivateKeyPEM(privData)
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}
	pub, err := parseRSAPublicKeyPEM(pubData)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	kid, err := fingerprintRSAPublicKey(pub)
	if err != nil {
		return fmt.Errorf("fingerprint public key: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if entry, ok := t.verifyKeys[t.currentKid]; ok && entry.expiresAt == nil {
		expiresAt := t.now().Add(KeyRotationGracePeriod)
		t.verifyKeys[t.currentKid] = verifyKeyEntry{key: entry.key, expiresAt: &expiresAt}
	}

	t.currentKid = kid
	t.signingKey = priv
	t.verifyKeys[kid] = verifyKeyEntry{key: pub}

	return nil
}

// NewHS256Issuer builds a development-only HS256 issuer. It requires a
// non-empty secret (MERLON_JWT_SECRET); RS256 should be preferred in
// production (auth.md §2.5 "現行実装からの移行").
func NewHS256Issuer(secret string) (*TokenIssuer, error) {
	if secret == "" {
		return nil, errors.New("MERLON_JWT_SECRET must not be empty")
	}

	sum := sha256.Sum256([]byte(secret))
	kid := hex.EncodeToString(sum[:])[:16]
	key := []byte(secret)

	return &TokenIssuer{
		method:     jwt.SigningMethodHS256,
		currentKid: kid,
		signingKey: key,
		verifyKeys: map[string]verifyKeyEntry{kid: {key: key}},
	}, nil
}

func (t *TokenIssuer) now() time.Time {
	if t.nowFunc != nil {
		return t.nowFunc()
	}
	return time.Now()
}

// IssueAccessToken signs a new access token for userID/role/jti, valid for
// AccessTokenTTL.
func (t *TokenIssuer) IssueAccessToken(userID, role, jti string) (string, error) {
	t.mu.RLock()
	method := t.method
	kid := t.currentKid
	signingKey := t.signingKey
	t.mu.RUnlock()

	now := t.now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		JTI:    jti,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid

	return token.SignedString(signingKey)
}

// VerifyAccessToken validates the signature, expiry, and kid rotation grace
// window, returning the parsed Claims on success.
func (t *TokenIssuer) VerifyAccessToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	keyFunc := func(tok *jwt.Token) (any, error) {
		if tok.Method.Alg() != t.method.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", tok.Method.Alg())
		}

		kid, ok := tok.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, errors.New("token missing kid header")
		}

		t.mu.RLock()
		entry, ok := t.verifyKeys[kid]
		t.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("unknown kid: %s", kid)
		}
		if entry.expiresAt != nil && t.now().After(*entry.expiresAt) {
			return nil, fmt.Errorf("kid %s past rotation grace period", kid)
		}

		return entry.key, nil
	}

	token, err := jwt.ParseWithClaims(tokenString, claims, keyFunc,
		jwt.WithValidMethods([]string{t.method.Alg()}),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return nil, fmt.Errorf("verify access token: %w", err)
	}
	if !token.Valid {
		return nil, errors.New("invalid access token")
	}

	return claims, nil
}

func parseRSAPrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid PEM data")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return key, nil
}

func parseRSAPublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid PEM data")
	}

	keyAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := keyAny.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}
	return key, nil
}

func fingerprintRSAPublicKey(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])[:16], nil
}
