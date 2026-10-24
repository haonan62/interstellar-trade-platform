package domain

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Password hashing.
//
// New hashes are argon2id in PHC string format, stored in User.PasswordHash
// (PasswordSalt is empty). Pre-existing users have a PBKDF2-HMAC-SHA256 hash
// (PasswordSalt + base64 PasswordHash); they are verified with the legacy
// routine and transparently upgraded on their next successful login.
//
// Hashing is CPU/memory hard BY DESIGN, so it must never run while holding the
// store lock, and concurrency is bounded so a login flood cannot exhaust RAM.
const (
	argonTime    uint32 = 2
	argonMemKiB  uint32 = 19 * 1024 // OWASP minimum profile: m=19 MiB, t=2, p=1
	argonThreads uint8  = 1
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
	argonPrefix         = "$argon2id$"
)

// HashSem bounds concurrent password hashing (memory = permits * argonMemKiB).
var HashSem = make(chan struct{}, maxInt(2, runtime.NumCPU()))

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func acquireHash() func() {
	HashSem <- struct{}{}
	return func() { <-HashSem }
}

func HashPasswordArgon2id(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	release := acquireHash()
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemKiB, argonThreads, argonKeyLen)
	release()
	return fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s", argonPrefix, argon2.Version, argonMemKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func IsArgon2idHash(hash string) bool { return strings.HasPrefix(hash, argonPrefix) }

func VerifyArgon2id(password, phc string) bool {
	parts := strings.Split(phc, "$") // "", argon2id, v=19, m=..,t=..,p=.., salt, key
	if len(parts) != 6 {
		return false
	}
	var version int
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	// Refuse absurd parameters from a tampered record (memory/CPU bomb).
	if mem == 0 || mem > 256*1024 || t == 0 || t > 10 || p == 0 || p > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	release := acquireHash()
	got := argon2.IDKey([]byte(password), salt, t, mem, p, uint32(len(want)))
	release()
	return subtle.ConstantTimeCompare(got, want) == 1
}

// VerifyPassword reports whether the password matches and whether the stored
// hash is a legacy one that should be upgraded.
func VerifyPassword(password, salt, hash string) (ok, legacy bool) {
	if IsArgon2idHash(hash) {
		return VerifyArgon2id(password, hash), false
	}
	return verifyLegacyPBKDF2(password, salt, hash), true
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// BurnPasswordCheck spends the same work as a real verification so unknown or
// inactive usernames are not distinguishable by response time.
func BurnPasswordCheck(password string) {
	dummyOnce.Do(func() { dummyHash, _ = HashPasswordArgon2id("dummy-password-for-timing") })
	VerifyArgon2id(password, dummyHash)
}
