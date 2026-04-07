package main

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

type Store struct {
	mu    sync.RWMutex
	path  string
	state *AppState
}

func NewStore(path string) (*Store, error) {
	st := &Store{path: path}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		now := nowUTC()
		st.state = &AppState{Meta: Meta{CreatedAt: now, UpdatedAt: now}, Colonies: map[string]*Colony{}, Users: map[string]*User{}, Sessions: map[string]*Session{}, Trades: map[string]*Trade{}, Bundles: map[string]*Bundle{}}
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
	st.state = &state
	return st, nil
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
	for _, c := range s.Colonies {
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
}

func StoreRead[T any](s *Store, fn func(*AppState) (T, error)) (T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fn(s.state)
}
func StoreUpdate[T any](s *Store, fn func(*AppState) (T, error)) (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := fn(s.state)
	if err != nil {
		var zero T
		return zero, err
	}
	s.state.Meta.UpdatedAt = nowUTC()
	if err := saveJSONAtomic(s.path, s.state); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
