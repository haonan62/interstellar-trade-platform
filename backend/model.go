package main

import "encoding/json"

type AppState struct {
	Meta     Meta                `json:"meta"`
	Colonies map[string]*Colony  `json:"colonies"`
	Users    map[string]*User    `json:"users"`
	Sessions map[string]*Session `json:"sessions"`
	Trades   map[string]*Trade   `json:"trades"`
	Bundles  map[string]*Bundle  `json:"bundles"`
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
	NetObligations     map[string]int64    `json:"net_obligations"`
	NetClaims          map[string]int64    `json:"net_claims"`
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
}

type ColonyView struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	PublicKey       string            `json:"public_key"`
	CreatedAt       string            `json:"created_at"`
	TrustedColonies map[string]string `json:"trusted_colonies"`
	NetObligations  map[string]int64  `json:"net_obligations"`
	NetClaims       map[string]int64  `json:"net_claims"`
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
}

type AuthResponse struct {
	Token string   `json:"token"`
	User  UserView `json:"user"`
}
