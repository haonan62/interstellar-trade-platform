package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/storage"
)

type Service struct{ store *storage.Store }

func NewService(store *storage.Store) *Service { return &Service{store: store} }

// Store is the backing store; tests use it to inspect and corrupt state.
func (s *Service) Store() *storage.Store { return s.store }

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
	ColonyID string        `json:"colony_id"`
	Bundle   domain.Bundle `json:"bundle"`
}
type ImportBundleResult struct {
	BundleID          string             `json:"bundle_id"`
	ImportedCount     int                `json:"imported_count"`
	SkippedDuplicates int                `json:"skipped_duplicates"`
	GeneratedOutbox   int                `json:"generated_outbox"`
	ProcessedTypes    []string           `json:"processed_types"`
	Trades            []domain.TradeView `json:"trades"`
}

func NormalizeUsername(username string) string { return strings.ToLower(strings.TrimSpace(username)) }
func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
func canManageColony(actor *domain.User, colonyID string) bool {
	return actor != nil && (domain.HasRole(actor, "super_admin") || (actor.ColonyID == colonyID && domain.HasRole(actor, "colony_admin")))
}
func canRelay(actor *domain.User, colonyID string) bool {
	return actor != nil && (domain.HasRole(actor, "super_admin") || (actor.ColonyID == colonyID && (domain.HasRole(actor, "colony_admin") || domain.HasRole(actor, "relay_operator"))))
}
func userExistsByUsername(state *domain.AppState, username string) bool {
	return findUserByUsername(state, username) != nil
}
func findUserByUsername(state *domain.AppState, username string) *domain.User {
	for _, u := range state.Users {
		if strings.EqualFold(u.Username, username) {
			return u
		}
	}
	return nil
}

func actorFromToken(state *domain.AppState, token string) (*domain.User, error) {
	user, err := actorFromTokenNoMFA(state, token)
	if err != nil {
		return nil, err
	}
	// When the deployment requires admin MFA, an admin without it is limited
	// to the enrolment endpoints (which use actorFromTokenNoMFA).
	if domain.AdminMFARequired.Load() && domain.IsAdminUser(user) && !user.MFAEnabled {
		return nil, &domain.AppError{Status: 403, Code: "mfa_enrollment_required", Message: "administrators must enable MFA before using the API"}
	}
	return user, nil
}

func actorFromTokenNoMFA(state *domain.AppState, token string) (*domain.User, error) {
	if token == "" {
		return nil, domain.Unauthorized("missing bearer token")
	}
	sess, ok := state.Sessions[token]
	if !ok {
		return nil, domain.Unauthorized("invalid session")
	}
	expiresAt, err := time.Parse(time.RFC3339, sess.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		return nil, domain.Unauthorized("session expired")
	}
	user := state.Users[sess.UserID]
	if user == nil || !user.Active {
		return nil, domain.Unauthorized("user not found")
	}
	return user, nil
}

// MaxSessionsPerUser bounds session growth; the oldest sessions are evicted.
const MaxSessionsPerUser = 10

// pruneSessions drops expired sessions and enforces the per-user cap. Sessions
// use the real clock (not nowFn): they are an operational concern, not trade
// logic.
func pruneSessions(state *domain.AppState, userID string, keep int) {
	now := time.Now().UTC()
	var mine []*domain.Session
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

func createSessionForUser(state *domain.AppState, user *domain.User) (domain.AuthResponse, error) {
	token, err := domain.RandomBase64URL(32)
	if err != nil {
		return domain.AuthResponse{}, domain.InternalErr(err.Error())
	}
	pruneSessions(state, user.ID, MaxSessionsPerUser-1) // room for the new one
	now := time.Now().UTC()
	state.Sessions[token] = &domain.Session{Token: token, UserID: user.ID, CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(domain.SessionTTL).Format(time.RFC3339), LastSeenAt: now.Format(time.RFC3339)}
	return domain.AuthResponse{Token: token, User: toUserView(user)}, nil
}

// newUser builds a user from an already-computed password hash: hashing must
// happen before the store lock is taken (see hashPasswordArgon2id).
func newUser(username, displayName, passwordHash, colonyID string, roles []string) (*domain.User, error) {
	id, err := domain.NewID("usr")
	if err != nil {
		return nil, domain.InternalErr(err.Error())
	}
	pub, priv, err := generateEd25519Keypair()
	if err != nil {
		return nil, domain.InternalErr(err.Error())
	}
	return &domain.User{ID: id, Username: username, DisplayName: displayName, ColonyID: colonyID, Roles: roles, PasswordHash: passwordHash, PublicKey: pub, PrivateKey: priv, CreatedAt: domain.NowUTC(), Active: true}, nil
}
func generateEd25519Keypair() (string, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}
func appendLedger(colony *domain.Colony, typ, actorUserID string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	colony.NextLedgerSeq++
	prevHash := colony.LastLedgerHash
	if n := len(colony.Ledger); prevHash == "" && n > 0 { // state written before LastLedgerHash existed
		prevHash = colony.Ledger[n-1].Hash
	}
	entry := &domain.LedgerEntry{Seq: colony.NextLedgerSeq, ColonyID: colony.ID, Time: domain.NowUTC(), Type: typ, ActorUserID: actorUserID, Payload: raw, PrevHash: prevHash}
	entry.Hash = domain.HashLedgerEntry(entry.Seq, entry.ColonyID, entry.Time, entry.Type, entry.ActorUserID, entry.Payload, entry.PrevHash)
	colony.Ledger = append(colony.Ledger, entry)
	colony.LastLedgerHash = entry.Hash
	return nil
}
func SignEnvelope(colony *domain.Colony, env *domain.Envelope) (string, error) {
	return domain.SignWithPrivateKey(colony.PrivateKey, domain.EnvelopeSignable{ID: env.ID, Type: env.Type, FromColonyID: env.FromColonyID, ToColonyID: env.ToColonyID, Payload: env.Payload, CreatedAt: env.CreatedAt})
}
func verifyEnvelope(colony *domain.Colony, env *domain.Envelope) error {
	return domain.VerifyWithPublicKey(colony.PublicKey, domain.EnvelopeSignable{ID: env.ID, Type: env.Type, FromColonyID: env.FromColonyID, ToColonyID: env.ToColonyID, Payload: env.Payload, CreatedAt: env.CreatedAt}, env.ColonySignature)
}
