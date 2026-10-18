package main

import (
	"testing"
	"time"
)

func TestExpiredOfferCannotBeAcceptedAndIsSwept(t *testing.T) {
	advance := useClock(t)
	f := newFixture(t)
	tr, err := f.svc.CreateOffer(f.admin, CreateOfferRequest{SellerUserID: f.seller, BuyerUserID: f.buyer, Asset: "ore", Price: 100, TTLSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if tr.ExpiresAt == "" {
		t.Fatal("offer has no expiry")
	}
	offerBundle := f.export(t, f.earth, f.mars)
	if _, err := f.importInto(f.mars, offerBundle); err != nil {
		t.Fatal(err)
	}
	advance(2 * time.Minute)
	if _, err := f.svc.AcceptTrade(f.admin, tr.ID); err == nil {
		t.Fatal("accepted an expired offer")
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Fatalf("buyer debited: %d", got)
	}
	if n, err := f.svc.SweepExpired(); err != nil || n != 1 {
		t.Fatalf("sweep n=%d err=%v", n, err)
	}
	if got := f.status(t, tr.ID); got != StatusExpired {
		t.Fatalf("status %s", got)
	}
	if n, _ := f.svc.SweepExpired(); n != 0 {
		t.Fatal("sweep not idempotent")
	}
	// A late-delivered duplicate offer must not resurrect it.
	replay := f.signedEnvelope(t, f.earth, f.mars, "offer", offerPayloadOf(t, f, tr.ID))
	_, _ = f.importInto(f.mars, bundleOf(f.earth, f.mars, replay))
	if got := f.status(t, tr.ID); got != StatusExpired {
		t.Fatalf("expired offer resurrected: %s", got)
	}
}

func TestSweepNeverTouchesEscrow(t *testing.T) {
	advance := useClock(t)
	f := newFixture(t)
	tr, err := f.svc.CreateOffer(f.admin, CreateOfferRequest{SellerUserID: f.seller, BuyerUserID: f.buyer, Asset: "ore", Price: 100, TTLSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AcceptTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	advance(24 * time.Hour)
	if n, _ := f.svc.SweepExpired(); n != 0 {
		t.Fatal("sweep touched an escrowed trade")
	}
	if got := f.status(t, tr.ID); got != StatusAcceptancePendingExport {
		t.Fatalf("status %s", got)
	}
}

func TestCancelOfferBySeller(t *testing.T) {
	f := newFixture(t)
	tr := f.offer(t, 100)
	b := f.export(t, f.earth, f.mars)
	if _, err := f.svc.CancelTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, tr.ID); got != StatusCancelled {
		t.Fatalf("status %s", got)
	}
	// The stale offer arrives afterwards and must be ignored.
	res, err := f.importInto(f.mars, b)
	if err != nil || res.ImportedCount != 0 {
		t.Fatalf("stale offer imported: %+v %v", res, err)
	}
	if got := f.status(t, tr.ID); got != StatusCancelled {
		t.Fatalf("status %s", got)
	}
}

// Lost acceptance + cancel: the buyer is refunded ONLY through the seller
// colony's signed decision, and a late copy of the acceptance cannot credit the
// seller afterwards (no double spend).
func TestCancelRequestRefundsAndLateAcceptanceIsIgnored(t *testing.T) {
	f := newFixture(t)
	tr := f.acceptedTrade(t, 100)
	if got := f.balance(t, f.mars, f.buyer); got != 900 {
		t.Fatalf("buyer balance %d", got)
	}
	if _, err := f.svc.CancelTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 900 {
		t.Fatalf("buyer refunded locally before seller decision: %d", got)
	}
	all := f.export(t, f.mars, f.earth)
	if len(all.Messages) != 2 {
		t.Fatalf("want acceptance+cancel, got %d", len(all.Messages))
	}
	// The acceptance is lost in transit; only the cancel arrives.
	if _, err := f.importInto(f.earth, onlyType(all, "cancel_request")); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, tr.ID); got != StatusRefundPendingExport {
		t.Fatalf("status %s", got)
	}
	if got := f.balance(t, f.earth, f.seller); got != 0 {
		t.Fatalf("seller credited %d", got)
	}
	// Refund travels back.
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, tr.ID); got != StatusRefunded {
		t.Fatalf("status %s", got)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Fatalf("buyer balance after refund %d", got)
	}
	// Now the delayed acceptance finally arrives.
	res, err := f.importInto(f.earth, onlyType(all, "acceptance"))
	if err != nil || res.ImportedCount != 0 {
		t.Fatalf("late acceptance applied: %+v %v", res, err)
	}
	if got := f.balance(t, f.earth, f.seller); got != 0 {
		t.Fatalf("seller credited %d after refund (double spend)", got)
	}
}

// Acceptance wins the race: the seller colony credits, the cancel is moot and
// the buyer is NOT refunded.
func TestCancelAfterSellerCreditedIsMoot(t *testing.T) {
	f := newFixture(t)
	tr := f.acceptedTrade(t, 100)
	if _, err := f.svc.CancelTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	all := f.export(t, f.mars, f.earth)
	// acceptance is ordered before cancel in the outbox
	if _, err := f.importInto(f.earth, all); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.earth, f.seller); got != 100 {
		t.Fatalf("seller balance %d", got)
	}
	if got := f.status(t, tr.ID); got != StatusSettlementPendingExport {
		t.Fatalf("status %s", got)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 900 {
		t.Fatalf("buyer wrongly refunded: %d", got)
	}
}

func TestLateAcceptanceIsRefundedNotCredited(t *testing.T) {
	f := newFixture(t)
	tr := f.acceptedTrade(t, 100)
	// Simulate: offer expired before the buyer-attested acceptance time.
	_, _ = StoreUpdate(f.svc.store, func(s *AppState) (struct{}, error) {
		s.Trades[tr.ID].ExpiresAt = time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
		return struct{}{}, nil
	})
	// ExpiresAt is covered by the seller's signature on the offer, but the
	// seller colony enforces its own stored value, which is what we changed.
	if _, err := f.importInto(f.earth, f.export(t, f.mars, f.earth)); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, tr.ID); got != StatusRefundPendingExport {
		t.Fatalf("status %s", got)
	}
	if got := f.balance(t, f.earth, f.seller); got != 0 {
		t.Fatalf("seller credited %d", got)
	}
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Fatalf("buyer balance %d", got)
	}
}

func TestAcceptanceForClosedOfferIsRefunded(t *testing.T) {
	f := newFixture(t)
	tr := f.acceptedTrade(t, 100)
	// Seller colony's view: it had already expired the offer.
	_, _ = StoreUpdate(f.svc.store, func(s *AppState) (struct{}, error) {
		s.Trades[tr.ID].Status = StatusExpired
		return struct{}{}, nil
	})
	if _, err := f.importInto(f.earth, f.export(t, f.mars, f.earth)); err != nil {
		t.Fatal(err)
	}
	if got := f.status(t, tr.ID); got != StatusRefundPendingExport {
		t.Fatalf("status %s", got)
	}
	if got := f.balance(t, f.earth, f.seller); got != 0 {
		t.Fatalf("seller credited %d", got)
	}
}

func TestRefundWithoutSellerDecisionIsRejected(t *testing.T) {
	f := newFixture(t)
	tr := f.acceptedTrade(t, 100)
	env := f.signedEnvelope(t, f.earth, f.mars, "refund", RefundPayload{TradeID: tr.ID, BuyerUserID: f.buyer, BuyerColonyID: f.mars, SellerColonyID: f.earth, RefundAmount: 100, Reason: "forged", DecidedAt: nowUTC()})
	if _, err := f.importInto(f.mars, bundleOf(f.earth, f.mars, env)); err == nil {
		t.Fatal("refund accepted although the seller colony never decided it")
	}
	if got := f.balance(t, f.mars, f.buyer); got != 900 {
		t.Fatalf("buyer balance %d", got)
	}
}

func TestReplayedRefundDoesNotDoubleCredit(t *testing.T) {
	f := newFixture(t)
	tr := f.acceptedTrade(t, 100)
	if _, err := f.svc.CancelTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	all := f.export(t, f.mars, f.earth)
	if _, err := f.importInto(f.earth, onlyType(all, "cancel_request")); err != nil {
		t.Fatal(err)
	}
	refundBundle := f.export(t, f.earth, f.mars)
	if _, err := f.importInto(f.mars, refundBundle); err != nil {
		t.Fatal(err)
	}
	rewrapped := f.signedEnvelope(t, f.earth, f.mars, "refund", refundBundle.Messages[0].Payload)
	_, _ = f.importInto(f.mars, bundleOf(f.earth, f.mars, rewrapped))
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Fatalf("buyer balance %d, want 1000", got)
	}
}

func TestForceRefundNeedsSecondAdmin(t *testing.T) {
	f := newFixture(t)
	marsAdmin, _ := f.secondAdmin(t, f.mars, "marsadmin")
	tr := f.acceptedTrade(t, 100)
	pa, err := f.svc.ProposeForceRefund(marsAdmin, tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 900 {
		t.Fatalf("proposal changed balance: %d", got)
	}
	if _, err := f.svc.DecideApproval(marsAdmin, pa.ID, true); err == nil {
		t.Fatal("proposer approved their own proposal")
	}
	if _, err := f.svc.DecideApproval(f.admin, pa.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Fatalf("buyer balance %d", got)
	}
	if got := f.status(t, tr.ID); got != StatusRefunded {
		t.Fatalf("status %s", got)
	}
	if _, err := f.svc.DecideApproval(f.admin, pa.ID, true); err == nil {
		t.Fatal("approval executed twice")
	}
	// The original acceptance arriving later must not credit the seller.
	res, err := f.importInto(f.earth, f.export(t, f.mars, f.earth))
	if err != nil || res.ImportedCount != 0 {
		t.Fatalf("acceptance applied after force refund: %+v %v", res, err)
	}
	if got := f.balance(t, f.earth, f.seller); got != 0 {
		t.Fatalf("seller credited %d", got)
	}
}

func TestMintDualControlAndCap(t *testing.T) {
	advance := useClock(t)
	f := newFixture(t)
	marsAdmin, _ := f.secondAdmin(t, f.mars, "marsadmin")
	on := true
	if _, err := f.svc.SetPolicy(f.admin, f.mars, PolicyRequest{RequireDualControl: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Mint(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 10}); err == nil {
		t.Fatal("direct mint allowed under dual control")
	}
	out, err := f.svc.MintOrPropose(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 10})
	if err != nil || out.Pending == nil {
		t.Fatalf("want pending, got %+v %v", out, err)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1000 {
		t.Fatalf("proposal minted: %d", got)
	}
	if _, err := f.svc.DecideApproval(f.admin, out.Pending.ID, true); err == nil {
		t.Fatal("self-approval allowed")
	}
	if _, err := f.svc.DecideApproval(marsAdmin, out.Pending.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := f.balance(t, f.mars, f.buyer); got != 1010 {
		t.Fatalf("balance %d", got)
	}
	// Expired proposals cannot be approved.
	out2, err := f.svc.MintOrPropose(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 5})
	if err != nil || out2.Pending == nil {
		t.Fatal("no pending")
	}
	advance(PendingActionTTL + time.Hour)
	if _, err := f.svc.DecideApproval(marsAdmin, out2.Pending.ID, true); err == nil {
		t.Fatal("expired approval executed")
	}
	// Policy changes are themselves dual-controlled while enabled.
	off := false
	pol, err := f.svc.SetPolicy(f.admin, f.mars, PolicyRequest{RequireDualControl: &off})
	if err != nil || pol.Pending == nil {
		t.Fatalf("policy change not proposed: %+v %v", pol, err)
	}
}

func TestMintCapEnforced(t *testing.T) {
	f := newFixture(t) // 1000 already minted on Mars
	low := int64(500)
	if _, err := f.svc.SetPolicy(f.admin, f.mars, PolicyRequest{MintCap: &low}); err == nil {
		t.Fatal("cap below already-minted supply accepted")
	}
	capv := int64(1500)
	if _, err := f.svc.SetPolicy(f.admin, f.mars, PolicyRequest{MintCap: &capv}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Mint(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 600}); err == nil {
		t.Fatal("mint beyond cap accepted")
	}
	if _, err := f.svc.Mint(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Mint(f.admin, MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1}); err == nil {
		t.Fatal("mint beyond cap accepted")
	}
}

func TestMintedTotalBackfillFromLedger(t *testing.T) {
	skipOnPG(t, "backfill applies to state files")
	f := newFixture(t)
	_, _ = StoreUpdate(f.svc.store, func(s *AppState) (struct{}, error) {
		s.Colonies[f.mars].MintedTotal = 0
		return struct{}{}, nil
	})
	store, err := NewStore(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := StoreRead(store, func(s *AppState) (int64, error) { return s.Colonies[f.mars].MintedTotal, nil })
	if got != 1000 {
		t.Fatalf("backfilled %d, want 1000", got)
	}
}

func TestOldOfferWithoutExpiryStillVerifies(t *testing.T) {
	// omitempty must keep pre-expiry signatures valid.
	with := OfferUserSignable{TradeID: "t", Asset: "a", Price: 1}
	b1 := string(mustJSON(with))
	if want := `{"trade_id":"t","asset":"a","price":1,"seller_user_id":"","seller_colony_id":"","buyer_user_id":"","buyer_colony_id":"","created_at":""}`; b1 != want {
		t.Fatalf("signable bytes changed for legacy offers:\n%s", b1)
	}
}

// offerPayloadOf rebuilds the offer exactly as the seller colony signed it.
func offerPayloadOf(t *testing.T, f *fixture, tradeID string) OfferPayload {
	t.Helper()
	var p OfferPayload
	_, _ = StoreRead(f.svc.store, func(s *AppState) (struct{}, error) {
		tr := s.Trades[tradeID]
		p = OfferPayload{TradeID: tr.ID, Asset: tr.Asset, Price: tr.Price, SellerUserID: tr.SellerUserID, SellerName: s.Users[tr.SellerUserID].DisplayName, SellerColonyID: tr.SellerColonyID, BuyerUserID: tr.BuyerUserID, BuyerName: s.Users[tr.BuyerUserID].DisplayName, BuyerColonyID: tr.BuyerColonyID, CreatedAt: tr.CreatedAt, ExpiresAt: tr.ExpiresAt, SellerSignature: tr.SellerSignature}
		return struct{}{}, nil
	})
	return p
}
