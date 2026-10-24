package service

import (
	"sort"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/storage"
)

func (s *Service) Dashboard(token string) (domain.Dashboard, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, ColonyHeaders: true, TradesForActor: 10, Counts: true}, func(state *domain.AppState) (domain.Dashboard, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.Dashboard{}, err
		}
		d := domain.Dashboard{
			User:            toUserView(actor),
			ColonySummaries: []domain.ColonySummary{},
			RecentTrades:    []domain.TradeView{},
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
			if domain.HasRole(actor, "super_admin") || actor.ColonyID == c.ID {
				colonyIDs = append(colonyIDs, c.ID)
			}
		}
		sort.Strings(colonyIDs)
		for _, colonyID := range colonyIDs {
			colony := state.Colonies[colonyID]
			summary := domain.ColonySummary{Colony: toColonyView(colony), Accounts: len(colony.Accounts)}
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
func (s *Service) ListColonies(token string) ([]domain.ColonyView, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, ColonyHeaders: true}, func(state *domain.AppState) ([]domain.ColonyView, error) {
		if _, err := actorFromToken(state, token); err != nil {
			return nil, err
		}
		out := make([]domain.ColonyView, 0, len(state.Colonies))
		for _, c := range state.Colonies {
			out = append(out, toColonyView(c))
		}
		domain.SortColonies(out)
		return out, nil
	})
}
func (s *Service) ListUsers(token string) ([]domain.UserView, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, AllUsers: true, UserLimit: 1000}, func(state *domain.AppState) ([]domain.UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		out := make([]domain.UserView, 0, len(state.Users))
		for _, u := range state.Users {
			out = append(out, userViewFor(actor, u))
		}
		domain.SortUsers(out)
		return out, nil
	})
}

// userViewFor limits what one user learns about another. super_admin and the
// user's own colony see the full view. Everyone else only gets what is needed
// to pick a trade counterparty (id, username, display name, colony, active):
// no roles, creation time or MFA state.
func userViewFor(actor, u *domain.User) domain.UserView {
	if domain.HasRole(actor, "super_admin") || actor.ID == u.ID || (actor.ColonyID != "" && actor.ColonyID == u.ColonyID) {
		return toUserView(u)
	}
	return domain.UserView{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, ColonyID: u.ColonyID, Roles: []string{}, Active: u.Active}
}
func (s *Service) ListAccounts(token string) ([]domain.AccountView, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, ColonyHeaders: true, AccountsLimit: 2000}, func(state *domain.AppState) ([]domain.AccountView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		out := []domain.AccountView{}
		for _, colony := range state.Colonies {
			if !domain.HasRole(actor, "super_admin") && actor.ColonyID != colony.ID {
				continue
			}
			for userID, acc := range colony.Accounts {
				user := state.Users[userID]
				if user == nil {
					continue
				}
				out = append(out, domain.AccountView{UserID: acc.UserID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: acc.ColonyID, ColonyName: colony.Name, Balance: acc.Balance})
			}
		}
		domain.SortAccounts(out)
		return out, nil
	})
}
func (s *Service) ListTrades(token string) ([]domain.TradeView, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, TradesForActor: 500}, func(state *domain.AppState) ([]domain.TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		return filterTradesForActor(state, actor), nil
	})
}
func (s *Service) GetTrade(token, tradeID string) (domain.TradeView, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, Trades: []string{tradeID}}, func(state *domain.AppState) (domain.TradeView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.TradeView{}, err
		}
		trade := state.Trades[tradeID]
		if trade == nil {
			return domain.TradeView{}, domain.NotFound("trade not found")
		}
		if !tradeVisibleToActor(trade, actor) {
			return domain.TradeView{}, domain.Forbidden("not allowed to view this trade")
		}
		return toTradeView(state, trade), nil
	})
}

func (s *Service) ListLedger(token, colonyID string, limit int) ([]*domain.LedgerEntry, error) {
	return s.ListLedgerPage(token, colonyID, limit, 0)
}

// ListLedgerPage returns up to `limit` entries in ascending seq order, ending
// just before beforeSeq (0 = newest page). Only the returned page is copied.
func (s *Service) ListLedgerPage(token, colonyID string, limit int, beforeSeq int64) ([]*domain.LedgerEntry, error) {
	if limit <= 0 {
		limit = domain.DefaultLedgerLimit
	}
	if limit > domain.MaxLedgerLimit {
		limit = domain.MaxLedgerLimit
	}
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, Ledger: &storage.LedgerQuery{ColonyID: colonyID, Limit: limit, BeforeSeq: beforeSeq}}, func(state *domain.AppState) ([]*domain.LedgerEntry, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		colony := state.Colonies[colonyID]
		if colony == nil {
			return nil, domain.NotFound("colony not found")
		}
		if !domain.HasRole(actor, "super_admin") && actor.ColonyID != colonyID {
			return nil, domain.Forbidden("not allowed to read this colony ledger")
		}
		end := len(colony.Ledger)
		if beforeSeq > 0 { // entries are in ascending seq order: binary search
			end = sort.Search(len(colony.Ledger), func(i int) bool { return colony.Ledger[i].Seq >= beforeSeq })
		}
		start := end - limit
		if start < 0 {
			start = 0
		}
		entries, err := domain.CloneJSON(colony.Ledger[start:end])
		if err != nil {
			return nil, domain.InternalErr(err.Error())
		}
		return entries, nil
	})
}
func filterTradesForActor(state *domain.AppState, actor *domain.User) []domain.TradeView {
	out := []domain.TradeView{}
	for _, t := range state.Trades {
		if tradeVisibleToActor(t, actor) {
			out = append(out, toTradeView(state, t))
		}
	}
	domain.SortTrades(out)
	return out
}
func tradeVisibleToActor(trade *domain.Trade, actor *domain.User) bool {
	if actor == nil || trade == nil {
		return false
	}
	if domain.HasRole(actor, "super_admin") {
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
func toUserView(user *domain.User) domain.UserView {
	if user == nil {
		return domain.UserView{}
	}
	roles := append([]string{}, user.Roles...)
	sort.Strings(roles)
	return domain.UserView{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: user.ColonyID, Roles: roles, CreatedAt: user.CreatedAt, Active: user.Active, MFAEnabled: user.MFAEnabled}
}
func toColonyView(colony *domain.Colony) domain.ColonyView {
	if colony == nil {
		return domain.ColonyView{}
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
	return domain.ColonyView{ID: colony.ID, Name: colony.Name, PublicKey: colony.PublicKey, CreatedAt: colony.CreatedAt, TrustedColonies: trusted, NetObligations: obligations, NetClaims: claims, MintCap: domain.EffectiveMintCap(colony), MintedTotal: colony.MintedTotal, RequireDualControl: colony.RequireDualControl}
}
func toTradeView(state *domain.AppState, trade *domain.Trade) domain.TradeView {
	seller, buyer := state.Users[trade.SellerUserID], state.Users[trade.BuyerUserID]
	sellerColony, buyerColony := state.Colonies[trade.SellerColonyID], state.Colonies[trade.BuyerColonyID]
	view := domain.TradeView{ID: trade.ID, Asset: trade.Asset, Price: trade.Price, SellerUserID: trade.SellerUserID, BuyerUserID: trade.BuyerUserID, SellerColonyID: trade.SellerColonyID, BuyerColonyID: trade.BuyerColonyID, Status: trade.Status, CreatedAt: trade.CreatedAt, AcceptedAt: trade.AcceptedAt, SettledAt: trade.SettledAt, CompletedAt: trade.CompletedAt, OfferEnvelopeID: trade.OfferEnvelopeID, AcceptanceEnvelopeID: trade.AcceptanceEnvelopeID, SettlementEnvelopeID: trade.SettlementEnvelopeID, ExpiresAt: trade.ExpiresAt, RefundedAt: trade.RefundedAt}
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
