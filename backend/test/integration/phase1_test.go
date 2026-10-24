package integration

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/service"
	"interstellar-trade-platform/backend/internal/storage"
)

func TestTransitionTable(t *testing.T) {
	want := map[[2]string]bool{
		{domain.StatusOfferPendingExport, domain.StatusOfferReceived}:                true,
		{domain.StatusOfferPendingExport, domain.StatusExpired}:                      true,
		{domain.StatusOfferPendingExport, domain.StatusCancelled}:                    true,
		{domain.StatusOfferReceived, domain.StatusAcceptancePendingExport}:           true,
		{domain.StatusOfferReceived, domain.StatusExpired}:                           true,
		{domain.StatusOfferReceived, domain.StatusCancelled}:                         true,
		{domain.StatusAcceptancePendingExport, domain.StatusSettlementPendingExport}: true,
		{domain.StatusAcceptancePendingExport, domain.StatusRefundPendingExport}:     true,
		{domain.StatusAcceptancePendingExport, domain.StatusRefunded}:                true,
		{domain.StatusSettlementPendingExport, domain.StatusCompleted}:               true,
		{domain.StatusRefundPendingExport, domain.StatusRefunded}:                    true,
		{domain.StatusExpired, domain.StatusRefundPendingExport}:                     true,
		{domain.StatusCancelled, domain.StatusRefundPendingExport}:                   true,
	}
	all := []string{domain.StatusOfferPendingExport, domain.StatusOfferReceived, domain.StatusAcceptancePendingExport, domain.StatusSettlementPendingExport, domain.StatusCompleted, domain.StatusRefundPendingExport, domain.StatusRefunded, domain.StatusExpired, domain.StatusCancelled}
	for _, a := range all {
		for _, b := range all {
			if got := domain.TransitionAllowed(a, b); got != want[[2]string{a, b}] {
				t.Errorf("%s -> %s: allowed=%v want %v", a, b, got, want[[2]string{a, b}])
			}
		}
	}
	// Terminal states have no exits; every other state can reach a terminal.
	for _, term := range []string{domain.StatusCompleted, domain.StatusRefunded} {
		if len(domain.TradeTransitions[term]) != 0 {
			t.Errorf("%s must be terminal", term)
		}
	}
	for _, s := range all {
		if s == domain.StatusCompleted || s == domain.StatusRefunded || s == domain.StatusExpired || s == domain.StatusCancelled {
			continue
		}
		if !domain.Reachable(s, domain.StatusCompleted) && !domain.Reachable(s, domain.StatusRefunded) {
			t.Errorf("%s cannot reach a money-terminal state", s)
		}
	}
}

func TestStepTradeIdempotentAndOrdered(t *testing.T) {
	tr := &domain.Trade{Status: domain.StatusOfferPendingExport}
	if _, err := domain.StepTrade(tr, domain.StatusSettlementPendingExport, domain.StatusCompleted); err == nil {
		t.Error("skipping states must fail")
	}
	if tr.Status != domain.StatusOfferPendingExport {
		t.Error("failed step mutated trade")
	}
	for _, late := range []string{domain.StatusCompleted, domain.StatusRefunded, domain.StatusCancelled, domain.StatusExpired} {
		tr.Status = late
		applied, err := domain.StepTrade(tr, domain.StatusOfferPendingExport, domain.StatusOfferReceived)
		if err != nil || applied || tr.Status != late {
			t.Errorf("%s: late duplicate must be a no-op: applied=%v err=%v status=%s", late, applied, err, tr.Status)
		}
	}
}

func TestAddChecked(t *testing.T) {
	if _, err := domain.AddChecked(math.MaxInt64, 1); err == nil {
		t.Error("overflow not detected")
	}
	if v, err := domain.AddChecked(1, 2); err != nil || v != 3 {
		t.Errorf("got %d, %v", v, err)
	}
}

func TestMintLimits(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: domain.MaxAmount + 1}); err == nil {
		t.Error("over-limit mint accepted")
	}
	if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: -5}); err == nil {
		t.Error("negative mint accepted")
	}
	if _, err := f.svc.CreateOffer(f.admin, service.CreateOfferRequest{SellerUserID: f.seller, BuyerUserID: f.buyer, Asset: "x", Price: domain.MaxAmount + 1}); err == nil {
		t.Error("over-limit price accepted")
	}
	if _, err := f.svc.CreateOffer(f.admin, service.CreateOfferRequest{SellerUserID: f.seller, BuyerUserID: f.buyer, Asset: "x", Price: 1, TTLSeconds: 5}); err == nil {
		t.Error("too-short ttl accepted")
	}
}

// Conservation: minted == sum(balances) + escrow, where escrow is the price of
// trades whose buyer was debited but not yet credited to the seller or refunded
// (acceptance_pending_export, refund_pending_export). Bundles are randomly
// dropped, duplicated and reordered; offers expire, are cancelled, refunded and
// force-refunded; the clock jumps. Handler errors are expected and tolerated,
// but the invariant must hold after EVERY step.
func TestPropertyLedgerConservation(t *testing.T) {
	seeds := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	if testing.Short() || usingPG() { // PG round-trips make 8x250 steps slow
		seeds = seeds[:2]
	}
	// Guard against a vacuous walk: across all seeds every money outcome must
	// have been reached at least once.
	covered := map[string]int{}
	t.Cleanup(func() {
		t.Logf("coverage of final trade states: %v", covered)
		if testing.Short() || usingPG() {
			return
		}
		for _, st := range []string{domain.StatusCompleted, domain.StatusRefunded, domain.StatusExpired, domain.StatusCancelled, domain.StatusAcceptancePendingExport, domain.StatusRefundPendingExport} {
			if covered[st] == 0 {
				t.Errorf("property walk never ended a trade in %q; strengthen the walk", st)
			}
		}
	})
	for _, seed := range seeds {
		seed := seed
		t.Run("", func(t *testing.T) {
			advance := useClock(t)
			f := newFixture(t)
			marsAdmin, _ := f.secondAdmin(t, f.mars, "marsadmin")
			rng := rand.New(rand.NewSource(seed))
			minted := int64(1000) // fixture mint
			var trades []string
			var inflight []domain.Bundle // exported, not yet (fully) delivered
			routes := [][2]string{{f.earth, f.mars}, {f.mars, f.earth}}

			check := func(step int, what string) {
				total, mintedTotal := int64(0), int64(0)
				_, _ = storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
					for _, c := range s.Colonies {
						mintedTotal += c.MintedTotal
						for _, a := range c.Accounts {
							if a.Balance < 0 {
								t.Fatalf("seed %d step %d (%s): negative balance", seed, step, what)
							}
							total += a.Balance
						}
					}
					for _, tr := range s.Trades {
						if domain.IsEscrowed(tr.Status) {
							total += tr.Price
						}
						covered[tr.Status]++
						if _, ok := domain.TradeTransitions[tr.Status]; !ok {
							t.Fatalf("seed %d step %d (%s): unknown status %q", seed, step, what, tr.Status)
						}
					}
					return struct{}{}, nil
				})
				if total != minted {
					t.Fatalf("seed %d step %d (%s): balances+escrow=%d, minted=%d", seed, step, what, total, minted)
				}
				if mintedTotal != minted {
					t.Fatalf("seed %d step %d (%s): MintedTotal=%d, minted=%d", seed, step, what, mintedTotal, minted)
				}
			}

			pick := func() string { return trades[rng.Intn(len(trades))] }
			for step := 0; step < 250; step++ {
				var what string
				switch rng.Intn(10) {
				case 0:
					what = "offer"
					ttl := []int64{0, 60, 3600}[rng.Intn(3)]
					if tr, err := f.svc.CreateOffer(f.admin, service.CreateOfferRequest{SellerUserID: f.seller, BuyerUserID: f.buyer, Asset: "ore", Price: int64(1 + rng.Intn(300)), TTLSeconds: ttl}); err == nil {
						trades = append(trades, tr.ID)
					}
				case 1, 2:
					what = "accept"
					if len(trades) > 0 {
						_, _ = f.svc.AcceptTrade(f.admin, pick())
					}
				case 3:
					what = "export"
					r := routes[rng.Intn(2)]
					if b, err := f.svc.ExportBundle(f.admin, service.ExportBundleRequest{ColonyID: r[0], ToColonyID: r[1]}); err == nil && len(b.Messages) > 0 {
						if rng.Intn(4) == 0 {
							break // message loss: bundle never delivered
						}
						inflight = append(inflight, deepCopy(t, b))
					}
				case 4, 5:
					what = "import"
					if len(inflight) > 0 {
						i := rng.Intn(len(inflight)) // random order
						b := inflight[i]
						_, _ = f.importInto(b.ToColonyID, b)
						if rng.Intn(3) != 0 { // else keep it: duplicate delivery later
							inflight = append(inflight[:i], inflight[i+1:]...)
						}
					}
				case 6:
					what = "mint"
					amt := int64(1 + rng.Intn(200))
					if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: amt}); err == nil {
						minted += amt
					}
				case 7:
					what = "cancel"
					if len(trades) > 0 {
						_, _ = f.svc.CancelTrade(f.admin, pick())
					}
				case 8:
					what = "clock+sweep"
					d := time.Duration(rng.Intn(20)) * time.Minute
					if rng.Intn(12) == 0 {
						d = time.Duration(rng.Intn(3*24*60)) * time.Minute
					}
					advance(d)
					_, _ = f.svc.SweepExpired()
				case 9:
					what = "force-refund"
					if len(trades) > 0 && rng.Intn(3) == 0 {
						if pa, err := f.svc.ProposeForceRefund(marsAdmin, pick()); err == nil {
							if rng.Intn(2) == 0 {
								_, _ = f.svc.DecideApproval(f.admin, pa.ID, true)
							}
						}
					}
				}
				check(step, what)
			}
		})
	}
}
