package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// Store has two interchangeable backends behind the same API:
//
//   - file: the whole state in memory, one JSON file, one global RWMutex.
//     Kept for development, tests and as the migration source.
//   - pg:   PostgreSQL. Each call declares a Scope (the rows it touches); the
//     transaction locks only the colonies in scope, so unrelated colonies run
//     in parallel, and replicas share no process state.
type Store struct {
	mu    sync.RWMutex
	path  string
	state *AppState
	pg    *pgStore
}

// Scope tells the PostgreSQL store which rows a handler needs. The file store
// ignores it. A zero Scope means "everything" (slow path: used by tests and
// tooling, never by request handlers).
type Scope struct {
	All bool

	Tokens       []string // sessions (and their users) by token
	UserSessions []string // every session of these users
	Users        []string
	Usernames    []string // users by normalized username
	Trades       []string // trades; their users and both colonies are added
	Actions      []string // pending actions; their colony/trade/user are added
	Colonies     []string // locked (FOR UPDATE) when writing
	EnvelopeIDs  []string // processed-envelope lookups for the loaded colonies

	UserColonies  bool // also load the colonies of the loaded users
	ColonyHeaders bool // read-only headers of every colony (no accounts/outbox)
	AllUsers      bool // load users (up to UserLimit) regardless of ID
	UserLimit     int
	AllActions    bool // load pending actions (up to 500)

	TradesForActor int // up to N trades visible to the token's user, newest first
	AccountsLimit  int // up to N accounts visible to the token's user
	Counts         bool
	Lock           string // transaction-scoped advisory lock name
	PruneSessions  bool   // delete expired sessions in the same transaction
	Ledger         *LedgerQuery
}

// LedgerQuery loads one page of a colony's ledger (newest first, returned
// ascending) into Colony.Ledger.
type LedgerQuery struct {
	ColonyID  string
	Limit     int
	BeforeSeq int64
}

func (sc Scope) isAll() bool {
	return sc.All || (len(sc.Tokens) == 0 && len(sc.UserSessions) == 0 && len(sc.Users) == 0 && len(sc.Usernames) == 0 &&
		len(sc.Trades) == 0 && len(sc.Actions) == 0 && len(sc.Colonies) == 0 && len(sc.EnvelopeIDs) == 0 &&
		!sc.ColonyHeaders && !sc.AllUsers && !sc.AllActions && sc.TradesForActor == 0 && sc.AccountsLimit == 0 &&
		!sc.Counts && sc.Lock == "" && !sc.PruneSessions && sc.Ledger == nil)
}

func newEmptyState() *AppState {
	now := nowUTC()
	return &AppState{Meta: Meta{CreatedAt: now, UpdatedAt: now}, Colonies: map[string]*Colony{}, Users: map[string]*User{}, Sessions: map[string]*Session{}, Trades: map[string]*Trade{}, Bundles: map[string]*Bundle{}, PendingActions: map[string]*PendingAction{}}
}

func NewStore(path string) (*Store, error) {
	st := &Store{path: path}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		st.state = newEmptyState()
		if err := saveJSONAtomic(path, st.state); err != nil {
			return nil, err
		}
		return st, nil
	} else if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state AppState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	ensureStateMaps(&state)
	normalizeRawPayloads(&state)
	backfillMintedTotals(&state)
	backfillLedgerHeads(&state)
	st.state = &state
	return st, nil
}

// loadStateFile reads a state file without creating one (migration source).
func loadStateFile(path string) (*AppState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state AppState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	ensureStateMaps(&state)
	normalizeRawPayloads(&state)
	backfillMintedTotals(&state)
	backfillLedgerHeads(&state)
	return &state, nil
}

func ensureStateMaps(s *AppState) {
	if s.Colonies == nil {
		s.Colonies = map[string]*Colony{}
	}
	if s.Users == nil {
		s.Users = map[string]*User{}
	}
	if s.Sessions == nil {
		s.Sessions = map[string]*Session{}
	}
	if s.Trades == nil {
		s.Trades = map[string]*Trade{}
	}
	if s.Bundles == nil {
		s.Bundles = map[string]*Bundle{}
	}
	if s.PendingActions == nil {
		s.PendingActions = map[string]*PendingAction{}
	}
	for _, c := range s.Colonies {
		ensureColonyMaps(c)
	}
}

func ensureColonyMaps(c *Colony) {
	if c.TrustedColonies == nil {
		c.TrustedColonies = map[string]string{}
	}
	if c.Accounts == nil {
		c.Accounts = map[string]*Account{}
	}
	if c.ProcessedEnvelopes == nil {
		c.ProcessedEnvelopes = map[string]bool{}
	}
	if c.Outbox == nil {
		c.Outbox = []*Envelope{}
	}
	if c.Ledger == nil {
		c.Ledger = []*LedgerEntry{}
	}
	if c.NetObligations == nil {
		c.NetObligations = map[string]int64{}
	}
	if c.NetClaims == nil {
		c.NetClaims = map[string]int64{}
	}
}

// backfillMintedTotals derives Colony.MintedTotal from the ledger for state
// files written before the field existed. It runs once at load.
func backfillMintedTotals(s *AppState) {
	for _, c := range s.Colonies {
		if c.MintedTotal != 0 {
			continue
		}
		for _, e := range c.Ledger {
			if e.Type != "funds_minted" {
				continue
			}
			var p struct {
				Amount int64 `json:"amount"`
			}
			if json.Unmarshal(e.Payload, &p) == nil && p.Amount > 0 {
				c.MintedTotal += p.Amount
			}
		}
	}
}

// normalizeRawPayloads restores the exact bytes of raw signed payloads. The
// state file is written with MarshalIndent, which re-indents embedded
// json.RawMessage values; ledger hashes cover those raw bytes. Originals were
// produced by json.Marshal (compact), so compacting is lossless.
func normalizeRawPayloads(s *AppState) {
	compact := func(raw json.RawMessage) json.RawMessage {
		var b bytes.Buffer
		if json.Compact(&b, raw) != nil {
			return raw
		}
		return b.Bytes()
	}
	for _, c := range s.Colonies {
		for _, e := range c.Ledger {
			e.Payload = compact(e.Payload)
		}
		for _, env := range c.Outbox {
			env.Payload = compact(env.Payload)
		}
	}
	for _, b := range s.Bundles {
		for _, env := range b.Messages {
			if env != nil {
				env.Payload = compact(env.Payload)
			}
		}
	}
}

// backfillLedgerHeads sets LastLedgerHash for files written before it existed.
func backfillLedgerHeads(s *AppState) {
	for _, c := range s.Colonies {
		if c.LastLedgerHash == "" && len(c.Ledger) > 0 {
			c.LastLedgerHash = c.Ledger[len(c.Ledger)-1].Hash
		}
	}
}

// StoreRead / StoreUpdate use the zero (everything) Scope. Request handlers
// must use the *Scoped variants with the narrowest scope that works.
func StoreRead[T any](s *Store, fn func(*AppState) (T, error)) (T, error) {
	return StoreReadScoped(s, Scope{}, fn)
}
func StoreUpdate[T any](s *Store, fn func(*AppState) (T, error)) (T, error) {
	return StoreUpdateScoped(s, Scope{}, fn)
}

func StoreReadScoped[T any](s *Store, sc Scope, fn func(*AppState) (T, error)) (T, error) {
	if s.pg != nil {
		var out T
		err := s.pg.run(sc, false, func(st *AppState) error {
			var e error
			out, e = fn(st)
			return e
		})
		if err != nil {
			var zero T
			return zero, err
		}
		return out, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fn(s.state)
}

func StoreUpdateScoped[T any](s *Store, sc Scope, fn func(*AppState) (T, error)) (T, error) {
	var zero T
	if s.pg != nil {
		var out T
		err := s.pg.run(sc, true, func(st *AppState) error {
			var e error
			out, e = fn(st)
			return e
		})
		if err != nil {
			return zero, err
		}
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Copy-on-write: fn mutates a private copy. The live state is replaced
	// only after fn succeeded AND the copy was durably written, so any failure
	// leaves memory and disk untouched.
	work, err := cloneJSON(s.state)
	if err != nil {
		return zero, internalErr(err.Error())
	}
	ensureStateMaps(work)
	out, err := fn(work)
	if err != nil {
		return zero, err
	}
	work.Meta.UpdatedAt = nowUTC()
	if err := saveJSONAtomic(s.path, work); err != nil {
		return zero, err
	}
	s.state = work
	return out, nil
}

// userCount works for both backends (scoped PG states carry Counts).
func userCount(state *AppState) int {
	if state.Counts != nil {
		return state.Counts.Users
	}
	return len(state.Users)
}
