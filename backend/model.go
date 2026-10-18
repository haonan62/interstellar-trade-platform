package main

import "encoding/json"

type AppState struct {
	Meta     Meta                `json:"meta"`
	Colonies map[string]*Colony  `json:"colonies"`
	Users    map[string]*User    `json:"users"`
	Sessions map[string]*Session `json:"sessions"`
	Trades   map[string]*Trade   `json:"trades"`
	Bundles  map[string]*Bundle  `json:"bundles"`
	// PendingActions holds dual-control proposals awaiting a second admin.
	PendingActions map[string]*PendingAction `json:"pending_actions,omitempty"`
	// Counts is filled only by the PostgreSQL store for scoped reads (a scoped
	// state holds a subset of rows, so len(map) is not the real total).
	Counts *StateCounts `json:"-"`
}

// StateCounts are database-side totals used by dashboard-style reads.
type StateCounts struct {
	Users, Colonies, Accounts, VisibleTrades int
	AccountsByColony                         map[string]int
	TradesByColony                           map[string]int
}

type Meta struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Colony struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	PublicKey          string              `json:"public_key"`
	PrivateKey         string              `json:"private_key"`
	CreatedAt          string              `json:"created_at"`
	TrustedColonies    map[string]string   `json:"trusted_colonies"`
	Accounts           map[string]*Account `json:"accounts"`
	ProcessedEnvelopes map[string]bool     `json:"processed_envelopes"`
	Outbox             []*Envelope         `json:"outbox"`
	Ledger             []*LedgerEntry      `json:"ledger"`
	NextLedgerSeq      int64               `json:"next_ledger_seq"`
	// LastLedgerHash lets appendLedger chain entries without holding the whole
	// ledger in memory (the PostgreSQL store never loads it).
	LastLedgerHash string           `json:"last_ledger_hash,omitempty"`
	NetObligations map[string]int64 `json:"net_obligations"`
	NetClaims      map[string]int64 `json:"net_claims"`
	// Supply policy. MintCap==0 means DefaultMintCap.
	MintedTotal        int64 `json:"minted_total,omitempty"`
	MintCap            int64 `json:"mint_cap,omitempty"`
	RequireDualControl bool  `json:"require_dual_control,omitempty"`
}

type Account struct {
	UserID   string `json:"user_id"`
	ColonyID string `json:"colony_id"`
	Balance  int64  `json:"balance"`
}

type User struct {
	ID           string   `json:"id"`
	Username     string   `json:"username"`
	DisplayName  string   `json:"display_name"`
	ColonyID     string   `json:"colony_id"`
	Roles        []string `json:"roles"`
	PasswordSalt string   `json:"password_salt"`
	PasswordHash string   `json:"password_hash"`
	PublicKey    string   `json:"public_key"`
	PrivateKey   string   `json:"private_key"`
	CreatedAt    string   `json:"created_at"`
	Active       bool     `json:"active"`

	// Phase 4 (all optional: documents written before it load unchanged).
	FailedLogins      int    `json:"failed_logins,omitempty"`
	LockedUntil       string `json:"locked_until,omitempty"`
	MFAEnabled        bool   `json:"mfa_enabled,omitempty"`
	MFASecret         string `json:"mfa_secret,omitempty"`         // base32 TOTP secret
	MFAPendingSecret  string `json:"mfa_pending_secret,omitempty"` // enrolment not yet confirmed
	MFALastStep       int64  `json:"mfa_last_step,omitempty"`      // last accepted TOTP step (replay guard)
	ResetTokenHash    string `json:"reset_token_hash,omitempty"`   // sha256 of the one-time reset secret
	ResetExpiresAt    string `json:"reset_expires_at,omitempty"`
	PasswordChangedAt string `json:"password_changed_at,omitempty"`
}

type Session struct {
	Token      string `json:"token"`
	UserID     string `json:"user_id"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	LastSeenAt string `json:"last_seen_at"`
}

type Trade struct {
	ID                   string `json:"id"`
	Asset                string `json:"asset"`
	Price                int64  `json:"price"`
	SellerUserID         string `json:"seller_user_id"`
	BuyerUserID          string `json:"buyer_user_id"`
	SellerColonyID       string `json:"seller_colony_id"`
	BuyerColonyID        string `json:"buyer_colony_id"`
	Status               string `json:"status"`
	OfferEnvelopeID      string `json:"offer_envelope_id,omitempty"`
	AcceptanceEnvelopeID string `json:"acceptance_envelope_id,omitempty"`
	SettlementEnvelopeID string `json:"settlement_envelope_id,omitempty"`
	SellerSignature      string `json:"seller_signature,omitempty"`
	BuyerSignature       string `json:"buyer_signature,omitempty"`
	CreatedAt            string `json:"created_at"`
	AcceptedAt           string `json:"accepted_at,omitempty"`
	SettledAt            string `json:"settled_at,omitempty"`
	CompletedAt          string `json:"completed_at,omitempty"`
	ExpiresAt            string `json:"expires_at,omitempty"`
	RefundedAt           string `json:"refunded_at,omitempty"`
	CancelEnvelopeID     string `json:"cancel_envelope_id,omitempty"`
	RefundEnvelopeID     string `json:"refund_envelope_id,omitempty"`
}

type LedgerEntry struct {
	Seq         int64           `json:"seq"`
	ColonyID    string          `json:"colony_id"`
	Time        string          `json:"time"`
	Type        string          `json:"type"`
	ActorUserID string          `json:"actor_user_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	PrevHash    string          `json:"prev_hash"`
	Hash        string          `json:"hash"`
}

type Envelope struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	FromColonyID    string          `json:"from_colony_id"`
	ToColonyID      string          `json:"to_colony_id"`
	Payload         json.RawMessage `json:"payload"`
	ColonySignature string          `json:"colony_signature"`
	CreatedAt       string          `json:"created_at"`
}

type Bundle struct {
	ID           string      `json:"id"`
	FromColonyID string      `json:"from_colony_id"`
	ToColonyID   string      `json:"to_colony_id"`
	ExportedAt   string      `json:"exported_at"`
	Messages     []*Envelope `json:"messages"`
}

type OfferUserSignable struct {
	TradeID        string `json:"trade_id"`
	Asset          string `json:"asset"`
	Price          int64  `json:"price"`
	SellerUserID   string `json:"seller_user_id"`
	SellerColonyID string `json:"seller_colony_id"`
	BuyerUserID    string `json:"buyer_user_id"`
	BuyerColonyID  string `json:"buyer_colony_id"`
	CreatedAt      string `json:"created_at"`
	// omitempty keeps signatures of pre-expiry offers byte-identical.
	ExpiresAt string `json:"expires_at,omitempty"`
}

type OfferPayload struct {
	TradeID         string `json:"trade_id"`
	Asset           string `json:"asset"`
	Price           int64  `json:"price"`
	SellerUserID    string `json:"seller_user_id"`
	SellerName      string `json:"seller_name"`
	SellerColonyID  string `json:"seller_colony_id"`
	BuyerUserID     string `json:"buyer_user_id"`
	BuyerName       string `json:"buyer_name"`
	BuyerColonyID   string `json:"buyer_colony_id"`
	CreatedAt       string `json:"created_at"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	SellerSignature string `json:"seller_signature"`
}

type AcceptanceUserSignable struct {
	TradeID       string `json:"trade_id"`
	BuyerUserID   string `json:"buyer_user_id"`
	BuyerColonyID string `json:"buyer_colony_id"`
	DebitAmount   int64  `json:"debit_amount"`
	AcceptedAt    string `json:"accepted_at"`
}

type AcceptancePayload struct {
	TradeID        string `json:"trade_id"`
	BuyerUserID    string `json:"buyer_user_id"`
	BuyerColonyID  string `json:"buyer_colony_id"`
	DebitAmount    int64  `json:"debit_amount"`
	AcceptedAt     string `json:"accepted_at"`
	BuyerSignature string `json:"buyer_signature"`
}

type SettlementPayload struct {
	TradeID            string `json:"trade_id"`
	SellerUserID       string `json:"seller_user_id"`
	SellerColonyID     string `json:"seller_colony_id"`
	CreditAmount       int64  `json:"credit_amount"`
	AcceptedFromColony string `json:"accepted_from_colony"`
	SettledAt          string `json:"settled_at"`
}

// CancelUserSignable / CancelPayload: the buyer asks the seller colony to refund
// an acceptance it has not yet credited (envelope type "cancel_request").
type CancelUserSignable struct {
	TradeID       string `json:"trade_id"`
	BuyerUserID   string `json:"buyer_user_id"`
	BuyerColonyID string `json:"buyer_colony_id"`
	RequestedAt   string `json:"requested_at"`
}

type CancelPayload struct {
	TradeID        string `json:"trade_id"`
	BuyerUserID    string `json:"buyer_user_id"`
	BuyerColonyID  string `json:"buyer_colony_id"`
	RequestedAt    string `json:"requested_at"`
	BuyerSignature string `json:"buyer_signature"`
}

// RefundPayload is the seller colony's authoritative decision that it did NOT
// credit the seller and the buyer must be refunded (envelope type "refund").
type RefundPayload struct {
	TradeID        string `json:"trade_id"`
	BuyerUserID    string `json:"buyer_user_id"`
	BuyerColonyID  string `json:"buyer_colony_id"`
	SellerColonyID string `json:"seller_colony_id"`
	RefundAmount   int64  `json:"refund_amount"`
	Reason         string `json:"reason"`
	DecidedAt      string `json:"decided_at"`
}

// PendingAction is a dual-control proposal: a different admin must approve it.
type PendingAction struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // mint | force_refund | policy
	ColonyID    string `json:"colony_id"`
	UserID      string `json:"user_id,omitempty"`
	TradeID     string `json:"trade_id,omitempty"`
	Amount      int64  `json:"amount,omitempty"`
	MintCap     *int64 `json:"mint_cap,omitempty"`
	RequireDual *bool  `json:"require_dual_control,omitempty"`
	ProposedBy  string `json:"proposed_by"`
	ProposedAt  string `json:"proposed_at"`
	ExpiresAt   string `json:"expires_at"`
	Status      string `json:"status"` // pending | approved | rejected
	DecidedBy   string `json:"decided_by,omitempty"`
	DecidedAt   string `json:"decided_at,omitempty"`
}

type envelopeSignable struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	FromColonyID string          `json:"from_colony_id"`
	ToColonyID   string          `json:"to_colony_id"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    string          `json:"created_at"`
}

type Dashboard struct {
	User            UserView        `json:"user"`
	Counts          DashboardCounts `json:"counts"`
	ColonySummaries []ColonySummary `json:"colony_summaries"`
	RecentTrades    []TradeView     `json:"recent_trades"`
}

type DashboardCounts struct {
	Colonies int `json:"colonies"`
	Users    int `json:"users"`
	Trades   int `json:"trades"`
	Accounts int `json:"accounts"`
}

type ColonySummary struct {
	Colony         ColonyView `json:"colony"`
	Accounts       int        `json:"accounts"`
	TradesInvolved int        `json:"trades_involved"`
}

type UserView struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	ColonyID    string   `json:"colony_id"`
	Roles       []string `json:"roles"`
	CreatedAt   string   `json:"created_at"`
	Active      bool     `json:"active"`
	MFAEnabled  bool     `json:"mfa_enabled,omitempty"`
}

type ColonyView struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	PublicKey          string            `json:"public_key"`
	CreatedAt          string            `json:"created_at"`
	TrustedColonies    map[string]string `json:"trusted_colonies"`
	NetObligations     map[string]int64  `json:"net_obligations"`
	NetClaims          map[string]int64  `json:"net_claims"`
	MintCap            int64             `json:"mint_cap"`
	MintedTotal        int64             `json:"minted_total"`
	RequireDualControl bool              `json:"require_dual_control"`
}

type AccountView struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	ColonyID    string `json:"colony_id"`
	ColonyName  string `json:"colony_name"`
	Balance     int64  `json:"balance"`
}

type TradeView struct {
	ID                   string `json:"id"`
	Asset                string `json:"asset"`
	Price                int64  `json:"price"`
	SellerUserID         string `json:"seller_user_id"`
	SellerName           string `json:"seller_name"`
	SellerColonyID       string `json:"seller_colony_id"`
	SellerColonyName     string `json:"seller_colony_name"`
	BuyerUserID          string `json:"buyer_user_id"`
	BuyerName            string `json:"buyer_name"`
	BuyerColonyID        string `json:"buyer_colony_id"`
	BuyerColonyName      string `json:"buyer_colony_name"`
	Status               string `json:"status"`
	CreatedAt            string `json:"created_at"`
	AcceptedAt           string `json:"accepted_at,omitempty"`
	SettledAt            string `json:"settled_at,omitempty"`
	CompletedAt          string `json:"completed_at,omitempty"`
	OfferEnvelopeID      string `json:"offer_envelope_id,omitempty"`
	AcceptanceEnvelopeID string `json:"acceptance_envelope_id,omitempty"`
	SettlementEnvelopeID string `json:"settlement_envelope_id,omitempty"`
	ExpiresAt            string `json:"expires_at,omitempty"`
	RefundedAt           string `json:"refunded_at,omitempty"`
}

type AuthResponse struct {
	Token string   `json:"token"`
	User  UserView `json:"user"`
}
