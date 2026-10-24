package domain

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	PasswordIterations = 120000
	SessionTTL         = 24 * time.Hour
)

// NowFn is the clock for trade/expiry logic; tests replace it. Session expiry
// deliberately keeps using the real clock.
var NowFn = time.Now

func NowUTC() string { return NowFn().UTC().Format(time.RFC3339) }
func NewID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}
func RandomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
func MustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func CloneJSON[T any](in T) (T, error) {
	var out T
	b, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}
func SaveJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func HashLedgerEntry(seq int64, colonyID, ts, typ, actor string, payload json.RawMessage, prev string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d|%s|%s|%s|%s|%s|", seq, colonyID, ts, typ, actor, prev)
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}
func HasRole(user *User, role string) bool {
	for _, r := range user.Roles {
		if r == role {
			return true
		}
	}
	return false
}
func NormalizeRoles(roles []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range roles {
		r = strings.TrimSpace(strings.ToLower(r))
		if r == "" || seen[r] {
			continue
		}
		switch r {
		case "super_admin", "colony_admin", "trader", "relay_operator":
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
func SortTrades(trades []TradeView) {
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].CreatedAt == trades[j].CreatedAt {
			return trades[i].ID > trades[j].ID
		}
		return trades[i].CreatedAt > trades[j].CreatedAt
	})
}
func SortUsers(users []UserView) {
	sort.Slice(users, func(i, j int) bool {
		if users[i].ColonyID == users[j].ColonyID {
			return users[i].Username < users[j].Username
		}
		return users[i].ColonyID < users[j].ColonyID
	})
}
func SortColonies(cols []ColonyView) {
	sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
}
func SortAccounts(accounts []AccountView) {
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].ColonyName == accounts[j].ColonyName {
			return accounts[i].Username < accounts[j].Username
		}
		return accounts[i].ColonyName < accounts[j].ColonyName
	})
}
func SignWithPrivateKey(privateKeyB64 string, signable any) (string, error) {
	privBytes, err := base64.StdEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(signable)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(privBytes), payload)
	return base64.StdEncoding.EncodeToString(sig), nil
}
func VerifyWithPublicKey(publicKeyB64 string, signable any, sigB64 string) error {
	pubBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return err
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(signable)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), payload, sigBytes) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
func DerivePBKDF2(password, salt []byte, iterations, keyLen int) []byte {
	hLen := 32
	numBlocks := (keyLen + hLen - 1) / hLen
	result := make([]byte, 0, numBlocks*hLen)
	for block := 1; block <= numBlocks; block++ {
		result = append(result, pbkdf2Block(password, salt, iterations, block)...)
	}
	return result[:keyLen]
}
func pbkdf2Block(password, salt []byte, iterations, blockNum int) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write([]byte{byte(blockNum >> 24), byte(blockNum >> 16), byte(blockNum >> 8), byte(blockNum)})
	u := mac.Sum(nil)
	out := make([]byte, len(u))
	copy(out, u)
	for i := 1; i < iterations; i++ {
		mac = hmac.New(sha256.New, password)
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}

// verifyLegacyPBKDF2 checks hashes created before the argon2id migration. It is
// only used to authenticate once and then upgrade the stored hash.
func verifyLegacyPBKDF2(password, saltB64, hashB64 string) bool {
	expected, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		return false
	}
	actual := DerivePBKDF2([]byte(password), []byte(saltB64), PasswordIterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
func BearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
