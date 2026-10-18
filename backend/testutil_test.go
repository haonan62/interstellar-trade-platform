package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

type fixture struct {
	svc           *Service
	admin         string // super_admin token
	earth, mars   string // colony IDs
	seller, buyer string // seller on Earth, buyer on Mars
	statePath     string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	return newFixtureWith(t, newTestStore(t, path), path)
}

func newFixtureWith(t *testing.T, store *Store, path string) *fixture {
	t.Helper()
	svc := NewService(store)
	f := &fixture{svc: svc, statePath: path}
	auth, err := svc.Bootstrap(BootstrapRequest{Username: "root", Password: "password-root"})
	if err != nil {
		t.Fatal(err)
	}
	f.admin = auth.Token
	earth, err := svc.CreateColony(f.admin, CreateColonyRequest{Name: "Earth"})
	if err != nil {
		t.Fatal(err)
	}
	mars, err := svc.CreateColony(f.admin, CreateColonyRequest{Name: "Mars"})
	if err != nil {
		t.Fatal(err)
	}
	f.earth, f.mars = earth.ID, mars.ID
	for _, p := range [][2]string{{f.earth, f.mars}, {f.mars, f.earth}} {
		if _, err := svc.TrustPeer(f.admin, p[0], TrustPeerRequest{PeerColonyID: p[1]}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := svc.CreateUser(f.admin, CreateUserRequest{Username: "seller", Password: "password-seller", ColonyID: f.earth, Roles: []string{"trader"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateUser(f.admin, CreateUserRequest{Username: "buyer", Password: "password-buyer", ColonyID: f.mars, Roles: []string{"trader"}})
	if err != nil {
		t.Fatal(err)
	}
	f.seller, f.buyer = s.ID, b.ID
	if _, err := svc.Mint(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1000}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) offer(t *testing.T, price int64) TradeView {
	t.Helper()
	tr, err := f.svc.CreateOffer(f.admin, CreateOfferRequest{SellerUserID: f.seller, BuyerUserID: f.buyer, Asset: "ore", Price: price})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func (f *fixture) export(t *testing.T, from, to string) Bundle {
	t.Helper()
	b, err := f.svc.ExportBundle(f.admin, ExportBundleRequest{ColonyID: from, ToColonyID: to})
	if err != nil {
		t.Fatal(err)
	}
	return deepCopy(t, b)
}

func (f *fixture) importInto(colony string, b Bundle) (ImportBundleResult, error) {
	return f.svc.ImportBundle(f.admin, ImportBundleRequest{ColonyID: colony, Bundle: b})
}

// completeTrade drives one trade through offer -> acceptance -> settlement and
// returns the bundle that carried the acceptance (Mars -> Earth).
func (f *fixture) completeTrade(t *testing.T, price int64) (TradeView, Bundle) {
	t.Helper()
	tr := f.offer(t, price)
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AcceptTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	accBundle := f.export(t, f.mars, f.earth)
	if _, err := f.importInto(f.earth, accBundle); err != nil {
		t.Fatal(err)
	}
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	return tr, accBundle
}

func (f *fixture) balance(t *testing.T, colony, user string) int64 {
	t.Helper()
	v, err := StoreRead(f.svc.store, func(s *AppState) (int64, error) { return s.Colonies[colony].Accounts[user].Balance, nil })
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *fixture) status(t *testing.T, tradeID string) string {
	t.Helper()
	v, _ := StoreRead(f.svc.store, func(s *AppState) (string, error) { return s.Trades[tradeID].Status, nil })
	return v
}

// snapshot serialises the whole in-memory state.
func (f *fixture) snapshot(t *testing.T) string {
	t.Helper()
	v, err := StoreRead(f.svc.store, func(s *AppState) (string, error) {
		b, err := json.Marshal(s)
		return string(b), err
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// signedEnvelope forges a validly colony-signed envelope, i.e. what a
// compromised relay or a replaying peer can produce.
func (f *fixture) signedEnvelope(t *testing.T, from, to, typ string, payload any) *Envelope {
	t.Helper()
	id, _ := newID("env")
	env := &Envelope{ID: id, Type: typ, FromColonyID: from, ToColonyID: to, Payload: mustJSON(payload), CreatedAt: nowUTC()}
	_, err := StoreRead(f.svc.store, func(s *AppState) (struct{}, error) {
		sig, err := signEnvelope(s.Colonies[from], env)
		env.ColonySignature = sig
		return struct{}{}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func bundleOf(from, to string, envs ...*Envelope) Bundle {
	id, _ := newID("bdl")
	return Bundle{ID: id, FromColonyID: from, ToColonyID: to, ExportedAt: nowUTC(), Messages: envs}
}

func deepCopy[T any](t *testing.T, v T) T {
	t.Helper()
	out, err := cloneJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
