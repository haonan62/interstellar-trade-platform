package service

import (
	"encoding/json"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/storage"
)

func isExpired(t *domain.Trade, now time.Time) bool {
	if t.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, t.ExpiresAt)
	return err == nil && now.After(exp)
}

// acceptedAfterExpiry reports whether the buyer-attested acceptance time is
// later than the offer's expiry. We use the signed acceptance timestamp, not
// the arrival time: light delay must not turn a timely acceptance into a late one.
func acceptedAfterExpiry(t *domain.Trade, acceptedAt string) (bool, error) {
	if t.ExpiresAt == "" {
		return false, nil
	}
	exp, err := time.Parse(time.RFC3339, t.ExpiresAt)
	if err != nil {
		return false, nil
	}
	at, err := time.Parse(time.RFC3339, acceptedAt)
	if err != nil {
		return false, domain.BadRequest("invalid accepted_at")
	}
	return at.After(exp), nil
}

// decideRefund records the seller colony's decision NOT to credit the seller
// and emits a signed refund envelope to the buyer colony. No balance changes
// here: the buyer is credited only when the refund envelope is imported.
func decideRefund(source, target *domain.Colony, trade *domain.Trade, trigger *domain.Envelope, reason string) (*domain.Trade, bool, error) {
	decidedAt := domain.NowUTC()
	payload := domain.RefundPayload{TradeID: trade.ID, BuyerUserID: trade.BuyerUserID, BuyerColonyID: source.ID, SellerColonyID: target.ID, RefundAmount: trade.Price, Reason: reason, DecidedAt: decidedAt}
	envID, err := domain.NewID("env")
	if err != nil {
		return nil, false, domain.InternalErr(err.Error())
	}
	env := &domain.Envelope{ID: envID, Type: "refund", FromColonyID: target.ID, ToColonyID: source.ID, Payload: domain.MustJSON(payload), CreatedAt: decidedAt}
	if env.ColonySignature, err = SignEnvelope(target, env); err != nil {
		return nil, false, domain.InternalErr(err.Error())
	}
	if err := domain.AdvanceTrade(trade, domain.StatusRefundPendingExport); err != nil {
		return nil, false, err
	}
	switch trigger.Type {
	case "cancel_request":
		trade.CancelEnvelopeID = trigger.ID
	case "acceptance":
		trade.AcceptanceEnvelopeID = trigger.ID
	}
	trade.RefundEnvelopeID = envID
	target.Outbox = append(target.Outbox, env)
	if err := appendLedger(target, "trade_refund_decided", trade.SellerUserID, map[string]any{"trade_id": trade.ID, "trigger_envelope_id": trigger.ID, "refund_envelope_id": envID, "reason": reason}); err != nil {
		return nil, false, domain.InternalErr(err.Error())
	}
	return trade, true, nil
}

// importCancel runs on the SELLER colony. If it has not yet credited the
// acceptance it decides a refund; if it already credited, the request is moot.
func importCancel(state *domain.AppState, source, target *domain.Colony, env *domain.Envelope) (*domain.Trade, bool, error) {
	var payload domain.CancelPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, false, domain.BadRequest("invalid cancel payload")
	}
	if payload.BuyerColonyID != source.ID {
		return nil, false, domain.BadRequest("cancel buyer colony does not match envelope source")
	}
	buyer := state.Users[payload.BuyerUserID]
	if buyer == nil || buyer.ColonyID != source.ID {
		return nil, false, domain.BadRequest("cancel buyer unknown or not in source colony")
	}
	signable := domain.CancelUserSignable{TradeID: payload.TradeID, BuyerUserID: payload.BuyerUserID, BuyerColonyID: payload.BuyerColonyID, RequestedAt: payload.RequestedAt}
	if err := domain.VerifyWithPublicKey(buyer.PublicKey, signable, payload.BuyerSignature); err != nil {
		return nil, false, domain.BadRequest("invalid buyer signature")
	}
	trade := state.Trades[payload.TradeID]
	if trade == nil {
		return nil, false, domain.NotFound("trade not found")
	}
	if trade.SellerColonyID != target.ID || trade.BuyerColonyID != source.ID || trade.BuyerUserID != buyer.ID {
		return nil, false, domain.BadRequest("cancel payload does not match trade")
	}
	switch domain.ClassifyStep(trade.Status, domain.StatusAcceptancePendingExport) {
	case domain.StepApply:
		return decideRefund(source, target, trade, env, "cancel requested by buyer before acceptance was credited")
	case domain.StepTooEarly:
		return nil, false, domain.Conflict("trade is " + trade.Status + "; nothing to cancel yet")
	default:
		return trade, false, nil // already credited / refunded / closed
	}
}

// applyRefund credits the buyer and closes the trade as refunded. It is the
// single place where a refund touches a balance.
func applyRefund(state *domain.AppState, trade *domain.Trade, actorID, ledgerType string, extra map[string]any) error {
	colony := state.Colonies[trade.BuyerColonyID]
	if colony == nil {
		return domain.NotFound("buyer colony not found")
	}
	acc := colony.Accounts[trade.BuyerUserID]
	if acc == nil {
		return domain.NotFound("buyer account not found")
	}
	newBalance, err := domain.AddChecked(acc.Balance, trade.Price)
	if err != nil {
		return err
	}
	if err := domain.AdvanceTrade(trade, domain.StatusRefunded); err != nil {
		return err
	}
	acc.Balance = newBalance
	trade.RefundedAt = domain.NowUTC()
	payload := map[string]any{"trade_id": trade.ID, "refund_amount": trade.Price, "new_balance": newBalance}
	for k, v := range extra {
		payload[k] = v
	}
	if err := appendLedger(colony, ledgerType, actorID, payload); err != nil {
		return domain.InternalErr(err.Error())
	}
	return nil
}

// importRefund runs on the BUYER colony.
func importRefund(state *domain.AppState, source, target *domain.Colony, env *domain.Envelope) (*domain.Trade, bool, error) {
	var payload domain.RefundPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, false, domain.BadRequest("invalid refund payload")
	}
	trade := state.Trades[payload.TradeID]
	if trade == nil {
		return nil, false, domain.NotFound("trade not found")
	}
	if payload.SellerColonyID != source.ID || payload.BuyerColonyID != target.ID ||
		trade.SellerColonyID != source.ID || trade.BuyerColonyID != target.ID ||
		trade.BuyerUserID != payload.BuyerUserID || trade.Price != payload.RefundAmount {
		return nil, false, domain.BadRequest("refund payload does not match trade")
	}
	switch domain.ClassifyStep(trade.Status, domain.StatusRefundPendingExport) {
	case domain.StepApply:
		if err := applyRefund(state, trade, trade.BuyerUserID, "trade_refunded", map[string]any{"refund_envelope_id": env.ID}); err != nil {
			return nil, false, err
		}
		return trade, true, nil
	case domain.StepTooEarly:
		return nil, false, domain.Conflict("trade is " + trade.Status + "; refund was not decided by the seller colony")
	default:
		return trade, false, nil
	}
}

// CancelTrade has two meanings depending on the trade state:
//   - offer not yet accepted: the seller withdraws it (status cancelled);
//   - acceptance in flight: the buyer asks the seller colony for a refund by
//     emitting a cancel_request. The buyer is NOT refunded here; only a signed
//     refund from the seller colony (or a dual-control force refund) does that.
//     Calling it again re-emits the request (safe, idempotent at the seller).
func (s *Service) CancelTrade(token, tradeID string) (domain.TradeView, error) {
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Trades: []string{tradeID}}, func(state *domain.AppState) (domain.TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.TradeView{}, err
		}
		trade := state.Trades[tradeID]
		if trade == nil {
			return domain.TradeView{}, domain.NotFound("trade not found")
		}
		if !domain.HasRole(actor, "super_admin") && !domain.HasRole(actor, "trader") {
			return domain.TradeView{}, domain.Forbidden("trader role is required")
		}
		switch trade.Status {
		case domain.StatusOfferPendingExport, domain.StatusOfferReceived:
			if actor.ID != trade.SellerUserID && !domain.HasRole(actor, "super_admin") {
				return domain.TradeView{}, domain.Forbidden("only the seller can cancel an unaccepted offer")
			}
			if err := domain.AdvanceTrade(trade, domain.StatusCancelled); err != nil {
				return domain.TradeView{}, err
			}
			if colony := state.Colonies[trade.SellerColonyID]; colony != nil {
				if err := appendLedger(colony, "trade_offer_cancelled", actor.ID, map[string]any{"trade_id": trade.ID}); err != nil {
					return domain.TradeView{}, domain.InternalErr(err.Error())
				}
			}
			return toTradeView(state, trade), nil
		case domain.StatusAcceptancePendingExport:
			buyer := state.Users[trade.BuyerUserID]
			if buyer == nil {
				return domain.TradeView{}, domain.NotFound("buyer not found")
			}
			if actor.ID != buyer.ID && !domain.HasRole(actor, "super_admin") {
				return domain.TradeView{}, domain.Forbidden("only the buyer can request cancellation of an acceptance")
			}
			buyerColony := state.Colonies[buyer.ColonyID]
			if buyerColony == nil {
				return domain.TradeView{}, domain.NotFound("buyer colony not found")
			}
			requestedAt := domain.NowUTC()
			sig, err := domain.SignWithPrivateKey(buyer.PrivateKey, domain.CancelUserSignable{TradeID: trade.ID, BuyerUserID: buyer.ID, BuyerColonyID: buyer.ColonyID, RequestedAt: requestedAt})
			if err != nil {
				return domain.TradeView{}, domain.InternalErr(err.Error())
			}
			payload := domain.CancelPayload{TradeID: trade.ID, BuyerUserID: buyer.ID, BuyerColonyID: buyer.ColonyID, RequestedAt: requestedAt, BuyerSignature: sig}
			envID, err := domain.NewID("env")
			if err != nil {
				return domain.TradeView{}, domain.InternalErr(err.Error())
			}
			env := &domain.Envelope{ID: envID, Type: "cancel_request", FromColonyID: buyer.ColonyID, ToColonyID: trade.SellerColonyID, Payload: domain.MustJSON(payload), CreatedAt: requestedAt}
			if env.ColonySignature, err = SignEnvelope(buyerColony, env); err != nil {
				return domain.TradeView{}, domain.InternalErr(err.Error())
			}
			buyerColony.Outbox = append(buyerColony.Outbox, env)
			trade.CancelEnvelopeID = envID
			if err := appendLedger(buyerColony, "trade_cancel_requested", actor.ID, map[string]any{"trade_id": trade.ID, "envelope_id": envID}); err != nil {
				return domain.TradeView{}, domain.InternalErr(err.Error())
			}
			return toTradeView(state, trade), nil
		default:
			return domain.TradeView{}, domain.Conflict("trade cannot be cancelled in state " + trade.Status)
		}
	})
}

// SweepExpired closes unaccepted offers whose expiry has passed. It never
// touches escrowed trades: money in flight is resolved only by the seller
// colony (or dual-control), never by a local timer.
func (s *Service) SweepExpired() (int, error) {
	now := domain.NowFn()
	due := func(t *domain.Trade) bool {
		return (t.Status == domain.StatusOfferPendingExport || t.Status == domain.StatusOfferReceived) && isExpired(t, now)
	}
	// PostgreSQL: find due offers by index instead of scanning every trade.
	sc := storage.Scope{}
	if s.store.IsPostgres() {
		ids, err := s.store.DueTradeIDs(now, 200)
		if err != nil || len(ids) == 0 {
			return 0, err
		}
		sc = storage.Scope{Trades: ids}
	}
	n, err := storage.StoreReadScoped(s.store, sc, func(state *domain.AppState) (int, error) {
		c := 0
		for _, t := range state.Trades {
			if due(t) {
				c++
			}
		}
		return c, nil
	})
	if err != nil || n == 0 {
		return 0, err
	}
	return storage.StoreUpdateScoped(s.store, sc, func(state *domain.AppState) (int, error) {
		c := 0
		for _, t := range state.Trades {
			if !due(t) {
				continue
			}
			if err := domain.AdvanceTrade(t, domain.StatusExpired); err != nil {
				return 0, err
			}
			if colony := state.Colonies[t.SellerColonyID]; colony != nil {
				if err := appendLedger(colony, "trade_offer_expired", "", map[string]any{"trade_id": t.ID, "expires_at": t.ExpiresAt}); err != nil {
					return 0, domain.InternalErr(err.Error())
				}
			}
			c++
		}
		return c, nil
	})
}
