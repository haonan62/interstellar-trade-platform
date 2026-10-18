package main

import "sort"

func (s *Service) Dashboard(token string) (Dashboard, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, ColonyHeaders: true, TradesForActor: 10, Counts: true}, func(state *AppState) (Dashboard, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return Dashboard{}, err
		}
		d := Dashboard{
			User:            toUserView(actor),
			ColonySummaries: []ColonySummary{},
			RecentTrades:    []TradeView{},
		}
		d.Counts.Colonies = len(state.Colonies)
		d.Counts.Users = len(state.Users)
		d.Counts.Trades = len(filterTradesForActor(state, actor))
		accounts := 0
		for _, c := range state.Colonies {
			accounts += len(c.Accounts)
		}
		d.Counts.Accounts = accounts
		if cn := state.Counts; cn != nil { // scoped (PostgreSQL) state holds only a subset of rows
			d.Counts.Colonies, d.Counts.Users, d.Counts.Trades, d.Counts.Accounts = cn.Colonies, cn.Users, cn.VisibleTrades, cn.Accounts
		}
		colonyIDs := []string{}
		for _, c := range state.Colonies {
			if hasRole(actor, "super_admin") || actor.ColonyID == c.ID {
				colonyIDs = append(colonyIDs, c.ID)
			}
		}
		sort.Strings(colonyIDs)
		for _, colonyID := range colonyIDs {
			colony := state.Colonies[colonyID]
			summary := ColonySummary{Colony: toColonyView(colony), Accounts: len(colony.Accounts)}
			for _, t := range state.Trades {
				if t.SellerColonyID == colonyID || t.BuyerColonyID == colonyID {
					summary.TradesInvolved++
				}
			}
			if cn := state.Counts; cn != nil {
				summary.Accounts, summary.TradesInvolved = cn.AccountsByColony[colonyID], cn.TradesByColony[colonyID]
			}
			d.ColonySummaries = append(d.ColonySummaries, summary)
		}
		trades := filterTradesForActor(state, actor)
		if len(trades) > 10 {
			trades = trades[:10]
		}
		d.RecentTrades = trades
		return d, nil
	})
}
func (s *Service) ListColonies(token string) ([]ColonyView, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, ColonyHeaders: true}, func(state *AppState) ([]ColonyView, error) {
		if _, err := actorFromToken(state, token); err != nil {
			return nil, err
		}
		out := make([]ColonyView, 0, len(state.Colonies))
		for _, c := range state.Colonies {
			out = append(out, toColonyView(c))
		}
		sortColonies(out)
		return out, nil
	})
}
func (s *Service) ListUsers(token string) ([]UserView, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, AllUsers: true, UserLimit: 1000}, func(state *AppState) ([]UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		out := make([]UserView, 0, len(state.Users))
		for _, u := range state.Users {
			out = append(out, userViewFor(actor, u))
		}
		sortUsers(out)
		return out, nil
	})
}

// userViewFor limits what one user learns about another. super_admin and the
// user's own colony see the full view. Everyone else only gets what is needed
// to pick a trade counterparty (id, username, display name, colony, active):
// no roles, creation time or MFA state.
func userViewFor(actor, u *User) UserView {
	if hasRole(actor, "super_admin") || actor.ID == u.ID || (actor.ColonyID != "" && actor.ColonyID == u.ColonyID) {
		return toUserView(u)
	}
	return UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, ColonyID: u.ColonyID, Roles: []string{}, Active: u.Active}
}
func (s *Service) ListAccounts(token string) ([]AccountView, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, ColonyHeaders: true, AccountsLimit: 2000}, func(state *AppState) ([]AccountView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		out := []AccountView{}
		for _, colony := range state.Colonies {
			if !hasRole(actor, "super_admin") && actor.ColonyID != colony.ID {
				continue
			}
			for userID, acc := range colony.Accounts {
				user := state.Users[userID]
				if user == nil {
					continue
				}
				out = append(out, AccountView{UserID: acc.UserID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: acc.ColonyID, ColonyName: colony.Name, Balance: acc.Balance})
			}
		}
		sortAccounts(out)
		return out, nil
	})
}
func (s *Service) ListTrades(token string) ([]TradeView, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, TradesForActor: 500}, func(state *AppState) ([]TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		return filterTradesForActor(state, actor), nil
	})
}
func (s *Service) GetTrade(token, tradeID string) (TradeView, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, Trades: []string{tradeID}}, func(state *AppState) (TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return TradeView{}, err
		}
		trade := state.Trades[tradeID]
		if trade == nil {
			return TradeView{}, notFound("trade not found")
		}
		if !tradeVisibleToActor(trade, actor) {
			return TradeView{}, forbidden("not allowed to view this trade")
		}
		return toTradeView(state, trade), nil
	})
}

// Ledger page sizes. A missing/zero limit used to mean "everything"; it now
// means DefaultLedgerLimit so one request cannot copy an unbounded ledger.
const (
	DefaultLedgerLimit = 200
	MaxLedgerLimit     = 1000
)

func (s *Service) ListLedger(token, colonyID string, limit int) ([]*LedgerEntry, error) {
	return s.ListLedgerPage(token, colonyID, limit, 0)
}

// ListLedgerPage returns up to `limit` entries in ascending seq order, ending
// just before beforeSeq (0 = newest page). Only the returned page is copied.
func (s *Service) ListLedgerPage(token, colonyID string, limit int, beforeSeq int64) ([]*LedgerEntry, error) {
	if limit <= 0 {
		limit = DefaultLedgerLimit
	}
	if limit > MaxLedgerLimit {
		limit = MaxLedgerLimit
	}
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, Ledger: &LedgerQuery{ColonyID: colonyID, Limit: limit, BeforeSeq: beforeSeq}}, func(state *AppState) ([]*LedgerEntry, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		colony := state.Colonies[colonyID]
		if colony == nil {
			return nil, notFound("colony not found")
		}
		if !hasRole(actor, "super_admin") && actor.ColonyID != colonyID {
			return nil, forbidden("not allowed to read this colony ledger")
		}
		end := len(colony.Ledger)
		if beforeSeq > 0 { // entries are in ascending seq order: binary search
			end = sort.Search(len(colony.Ledger), func(i int) bool { return colony.Ledger[i].Seq >= beforeSeq })
		}
		start := end - limit
		if start < 0 {
			start = 0
		}
		entries, err := cloneJSON(colony.Ledger[start:end])
		if err != nil {
			return nil, internalErr(err.Error())
		}
		return entries, nil
	})
}
func filterTradesForActor(state *AppState, actor *User) []TradeView {
	out := []TradeView{}
	for _, t := range state.Trades {
		if tradeVisibleToActor(t, actor) {
			out = append(out, toTradeView(state, t))
		}
	}
	sortTrades(out)
	return out
}
func tradeVisibleToActor(trade *Trade, actor *User) bool {
	if actor == nil || trade == nil {
		return false
	}
	if hasRole(actor, "super_admin") {
		return true
	}
	if actor.ID == trade.SellerUserID || actor.ID == trade.BuyerUserID {
		return true
	}
	if actor.ColonyID != "" && (actor.ColonyID == trade.SellerColonyID || actor.ColonyID == trade.BuyerColonyID) {
		return true
	}
	return false
}
func toUserView(user *User) UserView {
	if user == nil {
		return UserView{}
	}
	roles := append([]string{}, user.Roles...)
	sort.Strings(roles)
	return UserView{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: user.ColonyID, Roles: roles, CreatedAt: user.CreatedAt, Active: user.Active, MFAEnabled: user.MFAEnabled}
}
func toColonyView(colony *Colony) ColonyView {
	if colony == nil {
		return ColonyView{}
	}
	trusted, claims, obligations := map[string]string{}, map[string]int64{}, map[string]int64{}
	for k, v := range colony.TrustedColonies {
		trusted[k] = v
	}
	for k, v := range colony.NetClaims {
		claims[k] = v
	}
	for k, v := range colony.NetObligations {
		obligations[k] = v
	}
	return ColonyView{ID: colony.ID, Name: colony.Name, PublicKey: colony.PublicKey, CreatedAt: colony.CreatedAt, TrustedColonies: trusted, NetObligations: obligations, NetClaims: claims, MintCap: effectiveMintCap(colony), MintedTotal: colony.MintedTotal, RequireDualControl: colony.RequireDualControl}
}
func toTradeView(state *AppState, trade *Trade) TradeView {
	seller, buyer := state.Users[trade.SellerUserID], state.Users[trade.BuyerUserID]
	sellerColony, buyerColony := state.Colonies[trade.SellerColonyID], state.Colonies[trade.BuyerColonyID]
	view := TradeView{ID: trade.ID, Asset: trade.Asset, Price: trade.Price, SellerUserID: trade.SellerUserID, BuyerUserID: trade.BuyerUserID, SellerColonyID: trade.SellerColonyID, BuyerColonyID: trade.BuyerColonyID, Status: trade.Status, CreatedAt: trade.CreatedAt, AcceptedAt: trade.AcceptedAt, SettledAt: trade.SettledAt, CompletedAt: trade.CompletedAt, OfferEnvelopeID: trade.OfferEnvelopeID, AcceptanceEnvelopeID: trade.AcceptanceEnvelopeID, SettlementEnvelopeID: trade.SettlementEnvelopeID, ExpiresAt: trade.ExpiresAt, RefundedAt: trade.RefundedAt}
	if seller != nil {
		view.SellerName = seller.DisplayName
	}
	if buyer != nil {
		view.BuyerName = buyer.DisplayName
	}
	if sellerColony != nil {
		view.SellerColonyName = sellerColony.Name
	}
	if buyerColony != nil {
		view.BuyerColonyName = buyerColony.Name
	}
	return view
}
