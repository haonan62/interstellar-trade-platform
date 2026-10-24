package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/service"
	"interstellar-trade-platform/backend/internal/storage"
)

// Run the whole suite against PostgreSQL with:
//
//	TEST_DATABASE_URL=postgres://postgres:itp@localhost:55432/itp go test -race ./...
//
// Without it the file store is used. Each test gets its own schema.

var pgSchemas sync.Map // *Store -> schema name

func usingPG() bool { return os.Getenv("TEST_DATABASE_URL") != "" }

func skipOnPG(t *testing.T, why string) {
	t.Helper()
	if usingPG() {
		t.Skip("file-store specific: " + why)
	}
}

func onlyOnPG(t *testing.T) {
	t.Helper()
	if !usingPG() {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL tests")
	}
}

func newTestStore(t *testing.T, path string) *storage.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		store, err := storage.NewStore(path)
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "t_" + hex.EncodeToString(b[:])
	store, err := storage.NewPGStore(context.Background(), dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	pgSchemas.Store(store, schema)
	t.Cleanup(func() {
		_ = store.DropSchema(context.Background(), schema)
		_ = store.Close()
	})
	return store
}

// openReplica opens a second, independent store on the same database/schema,
// as another API replica would.
func openReplica(t *testing.T, s *storage.Store) *storage.Store {
	t.Helper()
	v, ok := pgSchemas.Load(s)
	if !ok {
		t.Fatal("not a PG test store")
	}
	r, err := storage.NewPGStore(context.Background(), os.Getenv("TEST_DATABASE_URL"), v.(string))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func (f *fixture) exec(t *testing.T, q string, args ...any) error {
	t.Helper()
	_, err := f.svc.Store().DB().ExecContext(context.Background(), q, args...)
	return err
}

// ---------------------------------------------------------------- tests

func TestConcurrentMintsAreSerialised(t *testing.T) {
	f := newFixture(t)
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000+n {
		t.Fatalf("lost update: balance %d, want %d", got, 1000+n)
	}
	total, _ := storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (int64, error) { return s.Colonies[f.mars].MintedTotal, nil })
	if total != 1000+n {
		t.Fatalf("minted_total %d, want %d", total, 1000+n)
	}
}

func TestLedgerChainSurvivesWithoutLoadingTheLedger(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := f.svc.ListLedgerPage(f.admin, f.mars, 1000, 0)
	if err != nil || len(entries) < 8 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	prev := ""
	for i, e := range entries {
		if e.Seq != int64(i+1) || e.PrevHash != prev {
			t.Fatalf("chain broken at %d (seq %d)", i, e.Seq)
		}
		if domain.HashLedgerEntry(e.Seq, e.ColonyID, e.Time, e.Type, e.ActorUserID, e.Payload, e.PrevHash) != e.Hash {
			t.Fatalf("bad hash at %d", i)
		}
		prev = e.Hash
	}
}

func TestPG_DifferentColoniesDoNotBlockEachOther(t *testing.T) {
	onlyOnPG(t)
	f := newFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() { // a long transaction that owns Earth
		_, err := storage.StoreUpdateScoped(f.svc.Store(), storage.Scope{Colonies: []string{f.earth}}, func(*domain.AppState) (struct{}, error) {
			close(started)
			<-release
			return struct{}{}, nil
		})
		held <- err
	}()
	<-started

	// Mars is independent: must complete while Earth is held.
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("write to Mars was blocked by a transaction holding Earth (global lock)")
	}

	// Earth is contended: must wait for the holder.
	earthDone := make(chan error, 1)
	go func() {
		_, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.earth, UserID: f.seller, Amount: 1})
		earthDone <- err
	}()
	select {
	case <-earthDone:
		t.Fatal("write to Earth did not wait for the transaction that locks Earth")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-earthDone; err != nil {
		t.Fatal(err)
	}
}

func TestPG_ConstraintsEnforceInvariants(t *testing.T) {
	onlyOnPG(t)
	f := newFixture(t)
	if err := f.exec(t, `UPDATE accounts SET balance = -1 WHERE colony_id = $1 AND user_id = $2`, f.mars, f.buyer); err == nil {
		t.Error("negative balance accepted by the database")
	}
	if err := f.exec(t, `INSERT INTO ledger_entries (colony_id, seq, doc) VALUES ($1, 1, '{}')`, f.mars); err == nil {
		t.Error("duplicate ledger sequence accepted")
	}
	if err := f.exec(t, `INSERT INTO processed_envelopes (colony_id, envelope_id) VALUES ($1, 'env_x'), ($1, 'env_x')`, f.mars); err == nil {
		t.Error("duplicate processed envelope accepted")
	}
	if err := f.exec(t, `INSERT INTO users (id, username_lower, doc) VALUES ('usr_dup', 'seller', '{}')`); err == nil {
		t.Error("duplicate username accepted")
	}
	// ... and the service maps it to a conflict instead of a 500.
	if _, err := f.svc.CreateUser(f.admin, service.CreateUserRequest{Username: "SELLER", Password: "password-dup", ColonyID: f.mars}); err == nil {
		t.Error("duplicate username accepted by the service")
	}
	if _, err := f.svc.CreateColony(f.admin, service.CreateColonyRequest{Name: "earth"}); err == nil {
		t.Error("duplicate colony name accepted")
	}
}

func TestPG_ImportedBundleCannotOverwriteAStoredBundle(t *testing.T) {
	onlyOnPG(t)
	f := newFixture(t)
	f.offer(t, 100)
	real := f.export(t, f.earth, f.mars) // stored at export time
	forged := domain.Bundle{ID: real.ID, FromColonyID: f.earth, ToColonyID: f.mars, ExportedAt: domain.NowUTC()}
	if _, err := f.importInto(f.mars, forged); err != nil {
		t.Fatal(err)
	}
	var msgs int
	if err := f.svc.Store().DB().QueryRowContext(context.Background(), `SELECT json_array_length(doc->'messages') FROM bundles WHERE id = $1`, real.ID).Scan(&msgs); err != nil {
		t.Fatal(err)
	}
	if msgs != 1 {
		t.Fatalf("stored bundle was overwritten (messages=%d)", msgs)
	}
}

func TestPG_ReplicasShareNoProcessState(t *testing.T) {
	onlyOnPG(t)
	f := newFixture(t)
	b := service.NewService(openReplica(t, f.svc.Store()))
	// Log in through replica B, use the token on replica A and vice versa.
	auth, err := b.Login(service.LoginRequest{Username: "seller", Password: "password-seller"})
	if err != nil {
		t.Fatal(err)
	}
	if me, err := f.svc.Me(auth.Token); err != nil || me.Username != "seller" {
		t.Fatalf("session from replica B not valid on A: %+v %v", me, err)
	}
	if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 7}); err != nil {
		t.Fatal(err)
	}
	accts, err := b.ListAccounts(f.admin)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range accts {
		if a.UserID == f.buyer && a.Balance == 1007 {
			found = true
		}
	}
	if !found {
		t.Fatalf("replica B does not see A's write: %+v", accts)
	}
	if err := b.Logout(auth.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Me(auth.Token); err == nil {
		t.Fatal("logout on B did not invalidate the session on A")
	}
}

func TestPG_MigrationFromStateFile(t *testing.T) {
	onlyOnPG(t)
	// Build a realistic state with the FILE store, then migrate it.
	path := t.TempDir() + "/state.json"
	fileStore, err := storage.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	src := newFixtureWith(t, fileStore, path)
	done, _ := src.completeTrade(t, 120)
	pending := src.acceptedTrade(t, 30) // escrowed trade must survive migration
	_, _ = src.svc.Mint(src.admin, service.MintRequest{ColonyID: src.mars, UserID: src.buyer, Amount: 5})

	st, err := storage.LoadStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dst := newTestStore(t, "")
	if err := dst.ImportState(context.Background(), st); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := dst.ImportState(context.Background(), st); err == nil {
		t.Fatal("second import into a non-empty database must be refused")
	}
	svc := service.NewService(dst)
	// Same balances.
	for cid, c := range st.Colonies {
		for uid, a := range c.Accounts {
			got, _ := storage.StoreRead(dst, func(s *domain.AppState) (int64, error) { return s.Colonies[cid].Accounts[uid].Balance, nil })
			if got != a.Balance {
				t.Fatalf("balance mismatch for %s: %d != %d", uid, got, a.Balance)
			}
		}
	}
	// Same trades; ledger hash chain intact and extendable.
	if tr, err := svc.GetTrade(src.admin, done.ID); err != nil || tr.Status != domain.StatusCompleted {
		t.Fatalf("trade lost in migration: %+v %v", tr, err)
	}
	entries, err := svc.ListLedgerPage(src.admin, src.mars, 1000, 0)
	if err != nil || len(entries) != len(st.Colonies[src.mars].Ledger) {
		t.Fatalf("ledger entries %d vs %d (%v)", len(entries), len(st.Colonies[src.mars].Ledger), err)
	}
	// The migrated database keeps working: old sessions, imports, settlement.
	g := &fixture{svc: svc, admin: src.admin, earth: src.earth, mars: src.mars, seller: src.seller, buyer: src.buyer}
	if _, err := g.importInto(g.earth, g.export(t, g.mars, g.earth)); err != nil {
		t.Fatalf("post-migration import: %v", err)
	}
	if _, err := g.importInto(g.mars, g.export(t, g.earth, g.mars)); err != nil {
		t.Fatalf("post-migration settlement: %v", err)
	}
	if got := g.status(t, pending.ID); got != domain.StatusCompleted {
		t.Fatalf("escrowed trade did not complete after migration: %s", got)
	}
	if _, err := svc.Mint(g.admin, service.MintRequest{ColonyID: g.mars, UserID: g.buyer, Amount: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRefusesCorruptedState(t *testing.T) {
	onlyOnPG(t)
	f := newFixture(t)
	_, _ = f.completeTrade(t, 50)
	st, _ := storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (*domain.AppState, error) { return domain.CloneJSON(s) })
	st.Colonies[f.mars].Accounts[f.buyer].Balance += 1 // money from nowhere
	dst := newTestStore(t, "")
	if err := dst.ImportState(context.Background(), st); err == nil {
		t.Fatal("migrated a state that violates money conservation")
	}
}
