package auth

import "testing"

func TestHashPassword_VerifyRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	ok, err := VerifyPassword(hash, "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword returned false for correct password")
	}
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	ok, err := VerifyPassword(hash, "wrong-password")
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword returned true for wrong password")
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	tests := []struct {
		name    string
		plain   string
		wantErr bool
	}{
		{"eleven characters", "12345678901", true},
		{"twelve characters", "123456789012", false},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordPolicy(tt.plain)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePasswordPolicy(%q) = nil, want error", tt.plain)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePasswordPolicy(%q) = %v, want nil", tt.plain, err)
			}
		})
	}
}

func TestHashPassword_NeverReturnsPlaintext(t *testing.T) {
	plain := "correct-horse-battery-staple"
	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == plain {
		t.Fatal("HashPassword returned the plaintext password unmodified")
	}
	for i := 0; i+len(plain) <= len(hash); i++ {
		if hash[i:i+len(plain)] == plain {
			t.Fatal("HashPassword output contains the plaintext password")
		}
	}
}
