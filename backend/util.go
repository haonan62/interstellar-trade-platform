package main

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
	passwordIterations = 120000
	sessionTTL         = 24 * time.Hour
)

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }
func newID(prefix string) (string, error) { buf := make([]byte, 8); if _, err := rand.Read(buf); err != nil { return "", err }; return prefix + "_" + hex.EncodeToString(buf), nil }
func randomBase64URL(n int) (string, error) { buf := make([]byte, n); if _, err := rand.Read(buf); err != nil { return "", err }; return base64.RawURLEncoding.EncodeToString(buf), nil }
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func cloneJSON[T any](in T) (T, error) { var out T; b, err := json.Marshal(in); if err != nil { return out, err }; err = json.Unmarshal(b, &out); return out, err }
func saveJSONAtomic(path string, v any) error { if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { return err }; tmp := path + ".tmp"; b, err := json.MarshalIndent(v, "", "  "); if err != nil { return err }; if err := os.WriteFile(tmp, b, 0o600); err != nil { return err }; return os.Rename(tmp, path) }
func hashLedgerEntry(seq int64, colonyID, ts, typ, actor string, payload json.RawMessage, prev string) string { h := sha256.New(); h.Write([]byte(fmt.Sprintf("%d|%s|%s|%s|%s|%s|", seq, colonyID, ts, typ, actor, prev))); h.Write(payload); return hex.EncodeToString(h.Sum(nil)) }
func hasRole(user *User, role string) bool { for _, r := range user.Roles { if r == role { return true } }; return false }
func normalizeRoles(roles []string) []string { seen := map[string]bool{}; out := []string{}; for _, r := range roles { r = strings.TrimSpace(strings.ToLower(r)); if r == "" || seen[r] { continue }; switch r { case "super_admin", "colony_admin", "trader", "relay_operator": seen[r] = true; out = append(out, r) } }; sort.Strings(out); return out }
func sortTrades(trades []TradeView) { sort.Slice(trades, func(i, j int) bool { if trades[i].CreatedAt == trades[j].CreatedAt { return trades[i].ID > trades[j].ID }; return trades[i].CreatedAt > trades[j].CreatedAt }) }
func sortUsers(users []UserView) { sort.Slice(users, func(i, j int) bool { if users[i].ColonyID == users[j].ColonyID { return users[i].Username < users[j].Username }; return users[i].ColonyID < users[j].ColonyID }) }
func sortColonies(cols []ColonyView) { sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name }) }
func sortAccounts(accounts []AccountView) { sort.Slice(accounts, func(i, j int) bool { if accounts[i].ColonyName == accounts[j].ColonyName { return accounts[i].Username < accounts[j].Username }; return accounts[i].ColonyName < accounts[j].ColonyName }) }
func signWithPrivateKey(privateKeyB64 string, signable any) (string, error) { privBytes, err := base64.StdEncoding.DecodeString(privateKeyB64); if err != nil { return "", err }; payload, err := json.Marshal(signable); if err != nil { return "", err }; sig := ed25519.Sign(ed25519.PrivateKey(privBytes), payload); return base64.StdEncoding.EncodeToString(sig), nil }
func verifyWithPublicKey(publicKeyB64 string, signable any, sigB64 string) error { pubBytes, err := base64.StdEncoding.DecodeString(publicKeyB64); if err != nil { return err }; sigBytes, err := base64.StdEncoding.DecodeString(sigB64); if err != nil { return err }; payload, err := json.Marshal(signable); if err != nil { return err }; if !ed25519.Verify(ed25519.PublicKey(pubBytes), payload, sigBytes) { return fmt.Errorf("invalid signature") }; return nil }
func derivePBKDF2(password, salt []byte, iterations, keyLen int) []byte { hLen := 32; numBlocks := (keyLen + hLen - 1) / hLen; result := make([]byte, 0, numBlocks*hLen); for block := 1; block <= numBlocks; block++ { result = append(result, pbkdf2Block(password, salt, iterations, block)...)}; return result[:keyLen] }
func pbkdf2Block(password, salt []byte, iterations, blockNum int) []byte { mac := hmac.New(sha256.New, password); mac.Write(salt); mac.Write([]byte{byte(blockNum >> 24), byte(blockNum >> 16), byte(blockNum >> 8), byte(blockNum)}); u := mac.Sum(nil); out := make([]byte, len(u)); copy(out, u); for i := 1; i < iterations; i++ { mac = hmac.New(sha256.New, password); mac.Write(u); u = mac.Sum(nil); for j := range out { out[j] ^= u[j] } }; return out }
func hashPassword(password string) (string, string, error) { salt, err := randomBase64URL(16); if err != nil { return "", "", err }; hash := derivePBKDF2([]byte(password), []byte(salt), passwordIterations, 32); return salt, base64.StdEncoding.EncodeToString(hash), nil }
func verifyPassword(password, saltB64, hashB64 string) bool { expected, err := base64.StdEncoding.DecodeString(hashB64); if err != nil { return false }; actual := derivePBKDF2([]byte(password), []byte(saltB64), passwordIterations, len(expected)); return subtle.ConstantTimeCompare(actual, expected) == 1 }
func bearerToken(header string) string { parts := strings.SplitN(header, " ", 2); if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") { return "" }; return strings.TrimSpace(parts[1]) }
