package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Service) CreateOffer(token string, req CreateOfferRequest) (TradeView, error) {
	asset := strings.TrimSpace(req.Asset)
	if asset == "" || req.Price <= 0 {
		return TradeView{}, badRequest("asset and positive price are required")
	}
	return StoreUpdate(s.store, func(state *AppState) (TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return TradeView{}, err
		}
		seller, buyer := state.Users[req.SellerUserID], state.Users[req.BuyerUserID]
		if seller == nil || buyer == nil {
			return TradeView{}, notFound("seller or buyer not found")
		}
		if seller.ColonyID == "" || buyer.ColonyID == "" {
			return TradeView{}, badRequest("seller and buyer must belong to colonies")
		}
		if seller.ID == buyer.ID {
			return TradeView{}, badRequest("seller and buyer must be different users")
		}
		if seller.ColonyID == buyer.ColonyID {
			return TradeView{}, badRequest("this POC expects inter-colony trades")
		}
		if actor.ID != seller.ID && !hasRole(actor, "super_admin") {
			return TradeView{}, forbidden("only the seller can create this offer")
		}
		if !hasRole(actor, "super_admin") && !hasRole(actor, "trader") {
			return TradeView{}, forbidden("trader role is required")
		}
		sellerColony, buyerColony := state.Colonies[seller.ColonyID], state.Colonies[buyer.ColonyID]
		if sellerColony == nil || buyerColony == nil {
			return TradeView{}, notFound("seller or buyer colony not found")
		}
		tradeID, err := newID("trd")
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		createdAt := nowUTC()
		signable := OfferUserSignable{TradeID: tradeID, Asset: asset, Price: req.Price, SellerUserID: seller.ID, SellerColonyID: seller.ColonyID, BuyerUserID: buyer.ID, BuyerColonyID: buyer.ColonyID, CreatedAt: createdAt}
		sellerSig, err := signWithPrivateKey(seller.PrivateKey, signable)
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		payload := OfferPayload{TradeID: tradeID, Asset: asset, Price: req.Price, SellerUserID: seller.ID, SellerName: seller.DisplayName, SellerColonyID: seller.ColonyID, BuyerUserID: buyer.ID, BuyerName: buyer.DisplayName, BuyerColonyID: buyer.ColonyID, CreatedAt: createdAt, SellerSignature: sellerSig}
		envID, err := newID("env")
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		env := &Envelope{ID: envID, Type: "offer", FromColonyID: seller.ColonyID, ToColonyID: buyer.ColonyID, Payload: mustJSON(payload), CreatedAt: createdAt}
		env.ColonySignature, err = signEnvelope(sellerColony, env)
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		sellerColony.Outbox = append(sellerColony.Outbox, env)
		trade := &Trade{ID: tradeID, Asset: asset, Price: req.Price, SellerUserID: seller.ID, BuyerUserID: buyer.ID, SellerColonyID: seller.ColonyID, BuyerColonyID: buyer.ColonyID, Status: "offer_pending_export", OfferEnvelopeID: envID, SellerSignature: sellerSig, CreatedAt: createdAt}
		state.Trades[trade.ID] = trade
		if err := appendLedger(sellerColony, "trade_offer_created", actor.ID, map[string]any{"trade_id": trade.ID, "asset": trade.Asset, "price": trade.Price, "buyer_user_id": buyer.ID, "envelope_id": envID}); err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		return toTradeView(state, trade), nil
	})
}

func (s *Service) AcceptTrade(token, tradeID string) (TradeView, error) {
	return StoreUpdate(s.store, func(state *AppState) (TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return TradeView{}, err
		}
		trade := state.Trades[tradeID]
		if trade == nil {
			return TradeView{}, notFound("trade not found")
		}
		if trade.Status != "offer_received" {
			return TradeView{}, conflict("trade is not ready for acceptance")
		}
		buyer := state.Users[trade.BuyerUserID]
		if buyer == nil {
			return TradeView{}, notFound("buyer not found")
		}
		if actor.ID != buyer.ID && !hasRole(actor, "super_admin") {
			return TradeView{}, forbidden("only the buyer can accept this trade")
		}
		if !hasRole(actor, "super_admin") && !hasRole(actor, "trader") {
			return TradeView{}, forbidden("trader role is required")
		}
		buyerColony := state.Colonies[buyer.ColonyID]
		if buyerColony == nil {
			return TradeView{}, notFound("buyer colony not found")
		}
		acc := buyerColony.Accounts[buyer.ID]
		if acc == nil {
			return TradeView{}, notFound("buyer account not found")
		}
		if acc.Balance < trade.Price {
			return TradeView{}, conflict("insufficient buyer balance")
		}
		acc.Balance -= trade.Price
		acceptedAt := nowUTC()
		signable := AcceptanceUserSignable{TradeID: trade.ID, BuyerUserID: buyer.ID, BuyerColonyID: buyer.ColonyID, DebitAmount: trade.Price, AcceptedAt: acceptedAt}
		buyerSig, err := signWithPrivateKey(buyer.PrivateKey, signable)
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		payload := AcceptancePayload{TradeID: trade.ID, BuyerUserID: buyer.ID, BuyerColonyID: buyer.ColonyID, DebitAmount: trade.Price, AcceptedAt: acceptedAt, BuyerSignature: buyerSig}
		envID, err := newID("env")
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		env := &Envelope{ID: envID, Type: "acceptance", FromColonyID: buyer.ColonyID, ToColonyID: trade.SellerColonyID, Payload: mustJSON(payload), CreatedAt: acceptedAt}
		env.ColonySignature, err = signEnvelope(buyerColony, env)
		if err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		buyerColony.Outbox = append(buyerColony.Outbox, env)
		trade.Status = "acceptance_pending_export"
		trade.AcceptedAt = acceptedAt
		trade.BuyerSignature = buyerSig
		trade.AcceptanceEnvelopeID = envID
		if err := appendLedger(buyerColony, "trade_accepted", actor.ID, map[string]any{"trade_id": trade.ID, "debit_amount": trade.Price, "new_balance": acc.Balance, "envelope_id": envID}); err != nil {
			return TradeView{}, internalErr(err.Error())
		}
		return toTradeView(state, trade), nil
	})
}

func (s *Service) ExportBundle(token string, req ExportBundleRequest) (Bundle, error) {
	if req.ColonyID == "" || req.ToColonyID == "" {
		return Bundle{}, badRequest("colony_id and to_colony_id are required")
	}
	return StoreUpdate(s.store, func(state *AppState) (Bundle, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return Bundle{}, err
		}
		if !canRelay(actor, req.ColonyID) {
			return Bundle{}, forbidden("not allowed to export for this colony")
		}
		colony := state.Colonies[req.ColonyID]
		if colony == nil {
			return Bundle{}, notFound("colony not found")
		}
		if _, ok := state.Colonies[req.ToColonyID]; !ok {
			return Bundle{}, notFound("destination colony not found")
		}
		messages, remaining := []*Envelope{}, []*Envelope{}
		for _, env := range colony.Outbox {
			if env.ToColonyID == req.ToColonyID {
				messages = append(messages, env)
			} else {
				remaining = append(remaining, env)
			}
		}
		colony.Outbox = remaining
		bundleID, err := newID("bdl")
		if err != nil {
			return Bundle{}, internalErr(err.Error())
		}
		bundle := Bundle{ID: bundleID, FromColonyID: req.ColonyID, ToColonyID: req.ToColonyID, ExportedAt: nowUTC(), Messages: messages}
		state.Bundles[bundle.ID] = &bundle
		if err := appendLedger(colony, "bundle_exported", actor.ID, map[string]any{"bundle_id": bundle.ID, "to_colony_id": req.ToColonyID, "message_count": len(messages)}); err != nil {
			return Bundle{}, internalErr(err.Error())
		}
		return bundle, nil
	})
}

func (s *Service) ImportBundle(token string, req ImportBundleRequest) (ImportBundleResult, error) {
	if req.ColonyID == "" {
		return ImportBundleResult{}, badRequest("colony_id is required")
	}
	return StoreUpdate(s.store, func(state *AppState) (ImportBundleResult, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return ImportBundleResult{}, err
		}
		if !canRelay(actor, req.ColonyID) {
			return ImportBundleResult{}, forbidden("not allowed to import for this colony")
		}
		target := state.Colonies[req.ColonyID]
		if target == nil {
			return ImportBundleResult{}, notFound("target colony not found")
		}
		if req.Bundle.ToColonyID != req.ColonyID {
			return ImportBundleResult{}, badRequest("bundle destination does not match target colony")
		}
		source := state.Colonies[req.Bundle.FromColonyID]
		if source == nil {
			return ImportBundleResult{}, notFound("source colony not found")
		}
		if target.TrustedColonies[source.ID] != source.PublicKey {
			return ImportBundleResult{}, forbidden("target colony does not trust the source colony")
		}
		result := ImportBundleResult{BundleID: req.Bundle.ID, ProcessedTypes: []string{}, Trades: []TradeView{}}
		generatedBefore := len(target.Outbox)
		for _, env := range req.Bundle.Messages {
			if env == nil {
				continue
			}
			if env.ToColonyID != target.ID || env.FromColonyID != source.ID {
				return ImportBundleResult{}, badRequest("bundle contains message with mismatched route")
			}
			if target.ProcessedEnvelopes[env.ID] {
				result.SkippedDuplicates++
				continue
			}
			if err := verifyEnvelope(source, env); err != nil {
				return ImportBundleResult{}, badRequest(fmt.Sprintf("invalid envelope %s: %v", env.ID, err))
			}
			trade, kind, err := importEnvelope(state, source, target, env)
			if err != nil {
				return ImportBundleResult{}, err
			}
			target.ProcessedEnvelopes[env.ID] = true
			result.ImportedCount++
			result.ProcessedTypes = append(result.ProcessedTypes, kind)
			if trade != nil {
				result.Trades = append(result.Trades, toTradeView(state, trade))
			}
		}
		result.GeneratedOutbox = len(target.Outbox) - generatedBefore
		state.Bundles[req.Bundle.ID] = &req.Bundle
		if err := appendLedger(target, "bundle_imported", actor.ID, map[string]any{"bundle_id": req.Bundle.ID, "from_colony_id": source.ID, "imported_count": result.ImportedCount, "skipped_duplicates": result.SkippedDuplicates}); err != nil {
			return ImportBundleResult{}, internalErr(err.Error())
		}
		return result, nil
	})
}

func importEnvelope(state *AppState, source, target *Colony, env *Envelope) (*Trade, string, error) {
	switch env.Type {
	case "offer":
		trade, err := importOffer(state, source, target, env)
		return trade, "offer", err
	case "acceptance":
		trade, err := importAcceptance(state, source, target, env)
		return trade, "acceptance", err
	case "settlement":
		trade, err := importSettlement(state, source, target, env)
		return trade, "settlement", err
	default:
		return nil, "", badRequest("unsupported envelope type")
	}
}
func importOffer(state *AppState, source, target *Colony, env *Envelope) (*Trade, error) {
	var payload OfferPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, badRequest("invalid offer payload")
	}
	seller, buyer := state.Users[payload.SellerUserID], state.Users[payload.BuyerUserID]
	if seller == nil || buyer == nil {
		return nil, notFound("seller or buyer not found")
	}
	signable := OfferUserSignable{TradeID: payload.TradeID, Asset: payload.Asset, Price: payload.Price, SellerUserID: payload.SellerUserID, SellerColonyID: payload.SellerColonyID, BuyerUserID: payload.BuyerUserID, BuyerColonyID: payload.BuyerColonyID, CreatedAt: payload.CreatedAt}
	if err := verifyWithPublicKey(seller.PublicKey, signable, payload.SellerSignature); err != nil {
		return nil, badRequest("invalid seller signature")
	}
	trade := state.Trades[payload.TradeID]
	if trade == nil {
		trade = &Trade{ID: payload.TradeID, Asset: payload.Asset, Price: payload.Price, SellerUserID: payload.SellerUserID, BuyerUserID: payload.BuyerUserID, SellerColonyID: payload.SellerColonyID, BuyerColonyID: payload.BuyerColonyID, CreatedAt: payload.CreatedAt, SellerSignature: payload.SellerSignature}
		state.Trades[trade.ID] = trade
	}
	trade.Status = "offer_received"
	trade.OfferEnvelopeID = env.ID
	trade.SellerSignature = payload.SellerSignature
	if err := appendLedger(target, "trade_offer_imported", buyer.ID, map[string]any{"trade_id": trade.ID, "from_colony_id": source.ID, "offer_envelope_id": env.ID}); err != nil {
		return nil, internalErr(err.Error())
	}
	return trade, nil
}
func importAcceptance(state *AppState, source, target *Colony, env *Envelope) (*Trade, error) {
	var payload AcceptancePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, badRequest("invalid acceptance payload")
	}
	buyer := state.Users[payload.BuyerUserID]
	if buyer == nil {
		return nil, notFound("buyer not found")
	}
	signable := AcceptanceUserSignable{TradeID: payload.TradeID, BuyerUserID: payload.BuyerUserID, BuyerColonyID: payload.BuyerColonyID, DebitAmount: payload.DebitAmount, AcceptedAt: payload.AcceptedAt}
	if err := verifyWithPublicKey(buyer.PublicKey, signable, payload.BuyerSignature); err != nil {
		return nil, badRequest("invalid buyer signature")
	}
	trade := state.Trades[payload.TradeID]
	if trade == nil {
		return nil, notFound("trade not found")
	}
	if trade.SellerColonyID != target.ID || trade.BuyerColonyID != source.ID || trade.Price != payload.DebitAmount {
		return nil, badRequest("acceptance payload does not match trade")
	}
	seller := state.Users[trade.SellerUserID]
	if seller == nil {
		return nil, notFound("seller not found")
	}
	sellerAcc := target.Accounts[seller.ID]
	if sellerAcc == nil {
		return nil, notFound("seller account not found")
	}
	sellerAcc.Balance += trade.Price
	target.NetClaims[source.ID] += trade.Price
	source.NetObligations[target.ID] += trade.Price
	trade.Status = "settlement_pending_export"
	trade.AcceptedAt = payload.AcceptedAt
	trade.AcceptanceEnvelopeID = env.ID
	trade.BuyerSignature = payload.BuyerSignature
	trade.SettledAt = nowUTC()
	settlementPayload := SettlementPayload{TradeID: trade.ID, SellerUserID: seller.ID, SellerColonyID: target.ID, CreditAmount: trade.Price, AcceptedFromColony: source.ID, SettledAt: trade.SettledAt}
	settlementEnvID, err := newID("env")
	if err != nil {
		return nil, internalErr(err.Error())
	}
	settlementEnv := &Envelope{ID: settlementEnvID, Type: "settlement", FromColonyID: target.ID, ToColonyID: source.ID, Payload: mustJSON(settlementPayload), CreatedAt: trade.SettledAt}
	settlementEnv.ColonySignature, err = signEnvelope(target, settlementEnv)
	if err != nil {
		return nil, internalErr(err.Error())
	}
	target.Outbox = append(target.Outbox, settlementEnv)
	trade.SettlementEnvelopeID = settlementEnv.ID
	if err := appendLedger(target, "trade_acceptance_imported", seller.ID, map[string]any{"trade_id": trade.ID, "acceptance_envelope_id": env.ID, "credited_amount": trade.Price, "new_balance": sellerAcc.Balance, "settlement_envelope_id": settlementEnv.ID}); err != nil {
		return nil, internalErr(err.Error())
	}
	return trade, nil
}
func importSettlement(state *AppState, source, target *Colony, env *Envelope) (*Trade, error) {
	var payload SettlementPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, badRequest("invalid settlement payload")
	}
	trade := state.Trades[payload.TradeID]
	if trade == nil {
		return nil, notFound("trade not found")
	}
	if trade.BuyerColonyID != target.ID || trade.SellerColonyID != source.ID || trade.Price != payload.CreditAmount {
		return nil, badRequest("settlement payload does not match trade")
	}
	trade.Status = "completed"
	trade.SettledAt = payload.SettledAt
	trade.CompletedAt = nowUTC()
	trade.SettlementEnvelopeID = env.ID
	if err := appendLedger(target, "trade_completed", trade.BuyerUserID, map[string]any{"trade_id": trade.ID, "settlement_envelope_id": env.ID, "completed_at": trade.CompletedAt}); err != nil {
		return nil, internalErr(err.Error())
	}
	return trade, nil
}
