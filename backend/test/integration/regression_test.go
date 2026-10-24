package integration

import (
	"os"
	"sync"
	"testing"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/service"
	"interstellar-trade-platform/backend/internal/storage"
)

// ---------------------------------------------------------------- Issue 1

func TestIssue1_ImportBundleFailureLeavesNoPartialState(t *testing.T) {
	f := newFixture(t)
	t1 := f.offer(t, 100)
	f.offer(t, 50)
	b := f.export(t, f.earth, f.mars)
	if len(b.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(b.Messages))
	}
	// Tamper with the second envelope: its colony signature no longer verifies.
	b.Messages[1].Payload = domain.MustJSON(map[string]any{"tampered": true})

	before := f.snapshot(t)
	if _, err := f.importInto(f.mars, b); err == nil {
		t.Fatal("expected import of tampered bundle to fail")
	}
	if got := f.status(t, t1.ID); got != "offer_pending_export" {
		t.Errorf("trade status leaked partial mutation: %q", got)
	}
	if after := f.snapshot(t); after != before {
		t.Error("in-memory state changed although the request failed")
	}
}

func TestIssue1_AcceptTradeFailureDoesNotDebitBuyer(t *testing.T) {
	f := newFixture(t)
	tr := f.offer(t, 100)
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	// Make signing fail after the debit has happened.
	_, _ = storage.StoreUpdate(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
		s.Users[f.buyer].PrivateKey = "!!not-base64!!"
		return struct{}{}, nil
	})
	before := f.snapshot(t)
	if _, err := f.svc.AcceptTrade(f.admin, tr.ID); err == nil {
		t.Fatal("expected AcceptTrade to fail")
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Errorf("buyer debited despite failure: balance=%d", got)
	}
	if after := f.snapshot(t); after != before {
		t.Error("in-memory state changed although the request failed")
	}
}

func TestIssue1_PersistFailureRollsBackMemory(t *testing.T) {
	skipOnPG(t, "simulates an unwritable state file; PostgreSQL rollback is covered by the other issue-1 tests")
	f := newFixture(t)
	// A directory where the temp file goes makes the atomic write fail.
	if err := os.Mkdir(f.statePath+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 5}); err == nil {
		t.Fatal("expected persist failure")
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Errorf("memory kept unpersisted mint: balance=%d", got)
	}
	if after := f.snapshot(t); after != before {
		t.Error("in-memory state changed although persisting failed")
	}
}

// ---------------------------------------------------------------- Issue 2

func TestIssue2_ReplayedAcceptanceDoesNotDoubleCredit(t *testing.T) {
	f := newFixture(t)
	tr, accBundle := f.completeTrade(t, 100)
	if got := f.balance(t, f.earth, f.seller); got != 100 {
		t.Fatalf("precondition: seller balance %d, want 100", got)
	}
	// Re-wrap the same signed acceptance payload in a fresh, validly
	// colony-signed envelope with a new ID.
	orig := accBundle.Messages[0]
	if orig.Type != "acceptance" {
		t.Fatalf("unexpected message type %q", orig.Type)
	}
	replay := f.signedEnvelope(t, f.mars, f.earth, "acceptance", orig.Payload)
	_, _ = f.importInto(f.earth, bundleOf(f.mars, f.earth, replay))
	if got := f.balance(t, f.earth, f.seller); got != 100 {
		t.Errorf("seller credited %d for a 100 trade (replay)", got)
	}
	if got := f.status(t, tr.ID); got != "completed" {
		t.Errorf("status regressed to %q", got)
	}
}

func TestIssue2_ReplayedOfferCannotRegressStatus(t *testing.T) {
	f := newFixture(t)
	tr, _ := f.completeTrade(t, 100)
	var offerPayload domain.OfferPayload
	_, _ = storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
		// Rebuild the offer exactly as the seller colony originally signed it.
		t := s.Trades[tr.ID]
		offerPayload = domain.OfferPayload{TradeID: t.ID, Asset: t.Asset, Price: t.Price, SellerUserID: t.SellerUserID, SellerName: s.Users[t.SellerUserID].DisplayName, SellerColonyID: t.SellerColonyID, BuyerUserID: t.BuyerUserID, BuyerName: s.Users[t.BuyerUserID].DisplayName, BuyerColonyID: t.BuyerColonyID, CreatedAt: t.CreatedAt, SellerSignature: t.SellerSignature}
		return struct{}{}, nil
	})
	env := f.signedEnvelope(t, f.earth, f.mars, "offer", offerPayload)
	_, _ = f.importInto(f.mars, bundleOf(f.earth, f.mars, env))
	if got := f.status(t, tr.ID); got != "completed" {
		t.Errorf("replayed offer regressed status to %q", got)
	}
}

func TestIssue2_SettlementCannotCompleteTradeFromAnyState(t *testing.T) {
	f := newFixture(t)
	tr := f.offer(t, 100) // status: offer_pending_export; buyer never accepted
	env := f.signedEnvelope(t, f.earth, f.mars, "settlement", domain.SettlementPayload{TradeID: tr.ID, SellerUserID: f.seller, SellerColonyID: f.earth, CreditAmount: 100, AcceptedFromColony: f.mars, SettledAt: domain.NowUTC()})
	_, _ = f.importInto(f.mars, bundleOf(f.earth, f.mars, env))
	if got := f.status(t, tr.ID); got == "completed" {
		t.Error("trade completed without offer/acceptance having happened")
	}
}

// ---------------------------------------------------------------- Issue 3

func TestIssue3_LoginHashingDoesNotBlockReads(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = f.svc.Login(service.LoginRequest{Username: "seller", Password: "password-seller"})
		}()
	}
	close(start)
	var worst time.Duration
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		t0 := time.Now()
		if _, err := f.svc.Me(f.admin); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(t0); d > worst {
			worst = d
		}
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()
	const limit = 80 * time.Millisecond
	if worst > limit {
		t.Errorf("a read waited %v behind concurrent logins (limit %v)", worst, limit)
	}
}

func TestIssue3_ExpiredSessionsArePruned(t *testing.T) {
	f := newFixture(t)
	past := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	_, _ = storage.StoreUpdate(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
		for _, k := range []string{"a", "b", "c", "d", "e"} {
			s.Sessions[k] = &domain.Session{Token: k, UserID: f.seller, CreatedAt: past, ExpiresAt: past, LastSeenAt: past}
		}
		return struct{}{}, nil
	})
	if _, err := f.svc.Login(service.LoginRequest{Username: "seller", Password: "password-seller"}); err != nil {
		t.Fatal(err)
	}
	n, _ := storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (int, error) {
		c := 0
		for _, k := range []string{"a", "b", "c", "d", "e"} {
			if _, ok := s.Sessions[k]; ok {
				c++
			}
		}
		return c, nil
	})
	if n != 0 {
		t.Errorf("%d expired sessions still stored", n)
	}
}
