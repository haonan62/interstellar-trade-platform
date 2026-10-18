package main

import "encoding/json"

// bundleScope derives the rows an import touches from the bundle itself: the
// two colonies (locked), the envelope IDs (idempotency lookups) and every
// trade/user the payloads reference.
func bundleScope(token string, req ImportBundleRequest) Scope {
	sc := Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID, req.Bundle.FromColonyID}}
	for _, env := range req.Bundle.Messages {
		if env == nil {
			continue
		}
		sc.EnvelopeIDs = append(sc.EnvelopeIDs, env.ID)
		var p struct {
			TradeID      string `json:"trade_id"`
			SellerUserID string `json:"seller_user_id"`
			BuyerUserID  string `json:"buyer_user_id"`
		}
		if json.Unmarshal(env.Payload, &p) == nil {
			if p.TradeID != "" {
				sc.Trades = append(sc.Trades, p.TradeID)
			}
			sc.Users = append(sc.Users, p.SellerUserID, p.BuyerUserID)
		}
	}
	return sc
}
