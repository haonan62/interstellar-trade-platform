package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

// Ready reports whether the backing store answers (used by /api/v1/readyz).
func (s *Service) Ready(ctx context.Context) error { return s.store.Ping(ctx) }

type BootstrapRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"` // required once the account has MFA enabled
}
type CreateColonyRequest struct {
	Name string `json:"name"`
}
type TrustPeerRequest struct {
	PeerColonyID string `json:"peer_colony_id"`
}
type CreateUserRequest struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Password    string   `json:"password"`
	ColonyID    string   `json:"colony_id"`
	Roles       []string `json:"roles"`
}
type MintRequest struct {
	ColonyID string `json:"colony_id"`
	UserID   string `json:"user_id"`
	Amount   int64  `json:"amount"`
}
type CreateOfferRequest struct {
	SellerUserID string `json:"seller_user_id"`
	BuyerUserID  string `json:"buyer_user_id"`
	Asset        string `json:"asset"`
	Price        int64  `json:"price"`
	// TTLSeconds optionally overrides the default 7-day offer lifetime.
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`
}
type ExportBundleRequest struct {
	ColonyID   string `json:"colony_id"`
	ToColonyID string `json:"to_colony_id"`
}
type ImportBundleRequest struct {
	ColonyID string `json:"colony_id"`
	Bundle   Bundle `json:"bundle"`
}
type ImportBundleResult struct {
	BundleID          string      `json:"bundle_id"`
	ImportedCount     int         `json:"imported_count"`
	SkippedDuplicates int         `json:"skipped_duplicates"`
	GeneratedOutbox   int         `json:"generated_outbox"`
	ProcessedTypes    []string    `json:"processed_types"`
	Trades            []TradeView `json:"trades"`
}

func normalizeUsername(username string) string { return strings.ToLower(strings.TrimSpace(username)) }
func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
func canManageColony(actor *User, colonyID string) bool {
	return actor != nil && (hasRole(actor, "super_admin") || (actor.ColonyID == colonyID && hasRole(actor, "colony_admin")))
}
func canRelay(actor *User, colonyID string) bool {
	return actor != nil && (hasRole(actor, "super_admin") || (actor.ColonyID == colonyID && (hasRole(actor, "colony_admin") || hasRole(actor, "relay_operator"))))
}
func userExistsByUsername(state *AppState, username string) bool {
	return findUserByUsername(state, username) != nil
}
func findUserByUsername(state *AppState, username string) *User {
	for _, u := range state.Users {
		if strings.EqualFold(u.Username, username) {
			return u
		}
	}
	return nil
}

func actorFromToken(state *AppState, token string) (*User, error) {
	user, err := actorFromTokenNoMFA(state, token)
	if err != nil {
		return nil, err
	}
	// When the deployment requires admin MFA, an admin without it is limited
	// to the enrolment endpoints (which use actorFromTokenNoMFA).
	if adminMFARequired.Load() && isAdminUser(user) && !user.MFAEnabled {
		return nil, &AppError{Status: 403, Code: "mfa_enrollment_required", Message: "administrators must enable MFA before using the API"}
	}
	return user, nil
}

func actorFromTokenNoMFA(state *AppState, token string) (*User, error) {
	if token == "" {
		return nil, unauthorized("missing bearer token")
	}
	sess, ok := state.Sessions[token]
	if !ok {
		return nil, unauthorized("invalid session")
	}
	expiresAt, err := time.Parse(time.RFC3339, sess.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		return nil, unauthorized("session expired")
	}
	user := state.Users[sess.UserID]
	if user == nil || !user.Active {
		return nil, unauthorized("user not found")
	}
	return user, nil
}

// maxSessionsPerUser bounds session growth; the oldest sessions are evicted.
const maxSessionsPerUser = 10

// pruneSessions drops expired sessions and enforces the per-user cap. Sessions
// use the real clock (not nowFn): they are an operational concern, not trade
// logic.
func pruneSessions(state *AppState, userID string, keep int) {
	now := time.Now().UTC()
	var mine []*Session
	for tok, sess := range state.Sessions {
		exp, err := time.Parse(time.RFC3339, sess.ExpiresAt)
		if err != nil || now.After(exp) {
			delete(state.Sessions, tok)
			continue
		}
		if sess.UserID == userID {
			mine = append(mine, sess)
		}
	}
	if len(mine) <= keep {
		return
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt < mine[j].CreatedAt })
	for _, sess := range mine[:len(mine)-keep] {
		delete(state.Sessions, sess.Token)
	}
}

func createSessionForUser(state *AppState, user *User) (AuthResponse, error) {
	token, err := randomBase64URL(32)
	if err != nil {
		return AuthResponse{}, internalErr(err.Error())
	}
	pruneSessions(state, user.ID, maxSessionsPerUser-1) // room for the new one
	now := time.Now().UTC()
	state.Sessions[token] = &Session{Token: token, UserID: user.ID, CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(sessionTTL).Format(time.RFC3339), LastSeenAt: now.Format(time.RFC3339)}
	return AuthResponse{Token: token, User: toUserView(user)}, nil
}

// newUser builds a user from an already-computed password hash: hashing must
// happen before the store lock is taken (see hashPasswordArgon2id).
func newUser(username, displayName, passwordHash, colonyID string, roles []string) (*User, error) {
	id, err := newID("usr")
	if err != nil {
		return nil, internalErr(err.Error())
	}
	pub, priv, err := generateEd25519Keypair()
	if err != nil {
		return nil, internalErr(err.Error())
	}
	return &User{ID: id, Username: username, DisplayName: displayName, ColonyID: colonyID, Roles: roles, PasswordHash: passwordHash, PublicKey: pub, PrivateKey: priv, CreatedAt: nowUTC(), Active: true}, nil
}
func generateEd25519Keypair() (string, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}
func appendLedger(colony *Colony, typ, actorUserID string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	colony.NextLedgerSeq++
	prevHash := colony.LastLedgerHash
	if n := len(colony.Ledger); prevHash == "" && n > 0 { // state written before LastLedgerHash existed
		prevHash = colony.Ledger[n-1].Hash
	}
	entry := &LedgerEntry{Seq: colony.NextLedgerSeq, ColonyID: colony.ID, Time: nowUTC(), Type: typ, ActorUserID: actorUserID, Payload: raw, PrevHash: prevHash}
	entry.Hash = hashLedgerEntry(entry.Seq, entry.ColonyID, entry.Time, entry.Type, entry.ActorUserID, entry.Payload, entry.PrevHash)
	colony.Ledger = append(colony.Ledger, entry)
	colony.LastLedgerHash = entry.Hash
	return nil
}
func signEnvelope(colony *Colony, env *Envelope) (string, error) {
	return signWithPrivateKey(colony.PrivateKey, envelopeSignable{ID: env.ID, Type: env.Type, FromColonyID: env.FromColonyID, ToColonyID: env.ToColonyID, Payload: env.Payload, CreatedAt: env.CreatedAt})
}
func verifyEnvelope(colony *Colony, env *Envelope) error {
	return verifyWithPublicKey(colony.PublicKey, envelopeSignable{ID: env.ID, Type: env.Type, FromColonyID: env.FromColonyID, ToColonyID: env.ToColonyID, Payload: env.Payload, CreatedAt: env.CreatedAt}, env.ColonySignature)
}
