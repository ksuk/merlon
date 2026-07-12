// Package logging provides structured (slog-based) logging with automatic
// PII masking for application log output (security.md §4.1, OPS-004).
//
// Masking is applied only at log emission time; it has no effect on data
// stored in the database. Audit logs (api/internal/server/audit.go) are
// exempt from this masking, since full recording is required there.
package logging

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MaskCustomerName masks a customer name to its first rune plus "***"
// (security.md §4.1: 顧客名 -> 先頭1文字 + "***").
func MaskCustomerName(name string) string {
	if name == "" {
		return "***"
	}
	runes := []rune(name)
	return string(runes[0]) + "***"
}

// MaskEmail masks an email address to the first two characters of the
// local part plus "***@domain" (security.md §4.1).
func MaskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at < 0 {
		return "***"
	}
	local, domain := email[:at], email[at+1:]
	if len(local) > 2 {
		local = local[:2]
	}
	return local + "***@" + domain
}

// MaskPhone masks a phone number so only the last group (or last 4 digits
// for a plain digit string) remains visible (security.md §4.1: 電話番号 ->
// 末尾4桁のみ表示).
func MaskPhone(phone string) string {
	if strings.Contains(phone, "-") {
		parts := strings.Split(phone, "-")
		for i := 0; i < len(parts)-1; i++ {
			parts[i] = "***"
		}
		return strings.Join(parts, "-")
	}
	if len(phone) <= 4 {
		return "***" + phone
	}
	return "***" + phone[len(phone)-4:]
}

// MaskExternalID returns the first 8 hex characters of the SHA-256 hash of
// externalID (security.md §4.1). The transformation is deterministic but
// one-way.
func MaskExternalID(externalID string) string {
	sum := sha256.Sum256([]byte(externalID))
	return hex.EncodeToString(sum[:])[:8]
}

// MaskIP masks the last octet of an IPv4 address (security.md §4.1: IP
// アドレス -> アプリログでは末尾オクテットをマスク). Audit logs record the
// full IP and must not use this function.
func MaskIP(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ip
	}
	parts[3] = "***"
	return strings.Join(parts, ".")
}
