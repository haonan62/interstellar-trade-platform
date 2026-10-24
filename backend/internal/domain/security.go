package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP is defined over HMAC-SHA1; it is not used for anything else.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Login throttling. Per ACCOUNT (not per IP): the counter lives in the user
// document, so every replica sees it. nginx adds the per-IP limit.
const (
	MaxFailedLogins  = 5
	LockoutDuration  = 15 * time.Minute
	ResetTokenTTL    = time.Hour
	minPasswordLen   = 8
	MaxPasswordLen   = 256 // argon2id cost grows with input; bound it
	TotpPeriod       = 30
	totpDigits       = 6
	totpSkewSteps    = 1
	totpIssuer       = "Interstellar Trade"
	resetTokenSecret = 32 // bytes of entropy
)

// AdminMFARequired is set once at start-up (--require-admin-mfa).
var AdminMFARequired atomic.Bool

func IsAdminUser(u *User) bool {
	return u != nil && (HasRole(u, "super_admin") || HasRole(u, "colony_admin"))
}

// LockClock is the clock for lockout/reset expiry; tests replace it. Like
// sessions, it is operational rather than trade logic.
var LockClock = time.Now

func IsLocked(u *User) bool {
	if u.LockedUntil == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, u.LockedUntil)
	return err != nil || LockClock().Before(until)
}

func ValidNewPassword(pw string) error {
	if len(pw) < minPasswordLen {
		return BadRequest(fmt.Sprintf("password must be at least %d characters", minPasswordLen))
	}
	if len(pw) > MaxPasswordLen {
		return BadRequest(fmt.Sprintf("password must be at most %d bytes", MaxPasswordLen))
	}
	return nil
}

// ---- TOTP (RFC 6238, SHA-1, 6 digits, 30 s) ----

func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func TotpAt(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off]&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])) % 1000000
	return fmt.Sprintf("%0*d", totpDigits, code), nil
}

// VerifyTOTP returns the matching time step (so the caller can refuse a replay
// of the same step) or 0 when no step within the skew window matches.
func VerifyTOTP(secret, code string, lastStep int64) int64 {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0
	}
	cur := LockClock().Unix() / TotpPeriod
	var matched int64
	for d := int64(-totpSkewSteps); d <= totpSkewSteps; d++ {
		want, err := TotpAt(secret, cur+d)
		if err != nil {
			return 0
		}
		// Compare every candidate (no early exit) to keep timing flat.
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && cur+d > lastStep && matched == 0 {
			matched = cur + d
		}
	}
	return matched
}

func OtpauthURL(username, secret string) string {
	label := url.PathEscape(totpIssuer + ":" + username)
	q := url.Values{"secret": {secret}, "issuer": {totpIssuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ---- one-time reset tokens: "<user id>.<secret>"; only sha256(secret) is stored ----

func NewResetToken(userID string) (token, hash string, err error) {
	b := make([]byte, resetTokenSecret)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	secret := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(secret))
	return userID + "." + secret, hex.EncodeToString(sum[:]), nil
}

func SplitResetToken(token string) (userID, secretHash string, ok bool) {
	i := strings.LastIndexByte(token, '.')
	if i <= 0 || i == len(token)-1 {
		return "", "", false
	}
	sum := sha256.Sum256([]byte(token[i+1:]))
	return token[:i], hex.EncodeToString(sum[:]), true
}
