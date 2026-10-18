package main

import (
	"fmt"
	"math"
)

// Trade state machine.
//
//	offer_pending_export -> offer_received -> acceptance_pending_export
//	  -> settlement_pending_export -> completed                (happy path)
//
//	acceptance_pending_export -> refund_pending_export -> refunded
//	      (seller colony decided: cancel requested / late acceptance)
//	acceptance_pending_export -> refunded   (dual-control force refund only)
//	offer_pending_export | offer_received -> expired | cancelled  (no money moved)
//	expired | cancelled -> refund_pending_export  (late acceptance reached a
//	      seller colony that had already closed the offer)
//
// Every mutation of Trade.Status MUST go through advanceTrade/stepTrade.
//
// Two-generals choice: the SELLER colony is the sole authority on whether the
// buyer's funds were consumed. The buyer colony never refunds on its own
// timeout. See docs/phase1-design.md.
const (
	StatusOfferPendingExport      = "offer_pending_export"
	StatusOfferReceived           = "offer_received"
	StatusAcceptancePendingExport = "acceptance_pending_export"
	StatusSettlementPendingExport = "settlement_pending_export"
	StatusCompleted               = "completed"
	StatusRefundPendingExport     = "refund_pending_export"
	StatusRefunded                = "refunded"
	StatusExpired                 = "expired"
	StatusCancelled               = "cancelled"
)

var tradeTransitions = map[string][]string{
	StatusOfferPendingExport:      {StatusOfferReceived, StatusExpired, StatusCancelled},
	StatusOfferReceived:           {StatusAcceptancePendingExport, StatusExpired, StatusCancelled},
	StatusAcceptancePendingExport: {StatusSettlementPendingExport, StatusRefundPendingExport, StatusRefunded},
	StatusSettlementPendingExport: {StatusCompleted},
	StatusRefundPendingExport:     {StatusRefunded},
	StatusExpired:                 {StatusRefundPendingExport},
	StatusCancelled:               {StatusRefundPendingExport},
	StatusCompleted:               {},
	StatusRefunded:                {},
}

// isEscrowed reports whether the buyer has been debited but the money has not
// yet been credited to the seller or refunded to the buyer.
func isEscrowed(status string) bool {
	return status == StatusAcceptancePendingExport || status == StatusRefundPendingExport
}

func transitionAllowed(from, to string) bool {
	for _, n := range tradeTransitions[from] {
		if n == to {
			return true
		}
	}
	return false
}

// reachable reports whether `to` can be reached from `from` in >= 1 steps.
func reachable(from, to string) bool {
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, n := range tradeTransitions[cur] {
			if n == to {
				return true
			}
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

// advanceTrade moves the trade to `to` or returns a conflict error.
func advanceTrade(t *Trade, to string) error {
	if !transitionAllowed(t.Status, to) {
		return conflict(fmt.Sprintf("illegal trade transition %s -> %s", t.Status, to))
	}
	t.Status = to
	return nil
}

type stepKind int

const (
	stepApply     stepKind = iota // trade is in the predecessor state
	stepDuplicate                 // trade already left/never needs this step
	stepTooEarly                  // trade has not reached the predecessor yet
)

// classifyStep implements per-trade idempotency for imported messages.
func classifyStep(status, pred string) stepKind {
	switch {
	case status == pred:
		return stepApply
	case reachable(status, pred):
		return stepTooEarly
	default:
		return stepDuplicate
	}
}

// stepTrade applies pred -> to if the trade is in pred. A trade that is past
// (or diverged from) pred is a duplicate: apply=false, no error, no mutation.
// A trade that has not reached pred yet is a conflict (message arrived early).
func stepTrade(t *Trade, pred, to string) (apply bool, err error) {
	switch classifyStep(t.Status, pred) {
	case stepApply:
		return true, advanceTrade(t, to)
	case stepTooEarly:
		return false, conflict(fmt.Sprintf("trade is %s, expected %s", t.Status, pred))
	default:
		return false, nil
	}
}

// Amount limits. MaxAmount keeps a single price/mint far below int64 overflow.
const (
	MaxAmount int64 = 1_000_000_000_000
	// DefaultMintCap bounds the total supply a colony can mint when no explicit
	// cap is configured.
	DefaultMintCap int64 = 1_000_000_000_000_000
)

func validAmount(a int64) bool { return a > 0 && a <= MaxAmount }

func effectiveMintCap(c *Colony) int64 {
	if c.MintCap > 0 {
		return c.MintCap
	}
	return DefaultMintCap
}

func addChecked(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, conflict("amount overflow")
	}
	return a + b, nil
}
