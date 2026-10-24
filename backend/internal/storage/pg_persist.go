package storage

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
)

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseTimeOrNil(s string) any {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return t.UTC()
}

// persist writes only what changed since load. Insert-only tables (ledger,
// processed envelopes, bundles) rely on their primary keys for safety: a
// duplicate ledger seq or processed envelope aborts the whole transaction.
func (p *pgStore) persist(ctx context.Context, tx *sql.Tx, st *domain.AppState, o *pgOrig, _ Scope) error {
	ex := func(q string, args ...any) error { _, err := tx.ExecContext(ctx, q, args...); return err }
	owned := func(id string) bool { // colony rows this tx may write
		if o.locked[id] {
			return true
		}
		_, loaded := o.colonies[id]
		return !loaded // brand-new colony created in this tx
	}

	for _, id := range sortedKeys(st.Colonies) {
		c := st.Colonies[id]
		hdr := jsonOf(colonyHeader(c))
		prev, loaded := o.colonies[id]
		switch {
		case !loaded:
			if err := ex(`INSERT INTO colonies (id, name_lower, doc) VALUES ($1, $2, $3::jsonb)`, id, strings.ToLower(c.Name), hdr); err != nil {
				return err
			}
		case o.locked[id] && prev != hdr:
			if err := ex(`UPDATE colonies SET name_lower = $2, doc = $3::jsonb WHERE id = $1`, id, strings.ToLower(c.Name), hdr); err != nil {
				return err
			}
		}
	}
	for _, id := range sortedKeys(st.Users) {
		u := st.Users[id]
		doc := jsonOf(u)
		prev, loaded := o.users[id]
		switch {
		case !loaded:
			if err := ex(`INSERT INTO users (id, username_lower, colony_id, doc) VALUES ($1, $2, $3, $4::jsonb)`, id, strings.ToLower(u.Username), u.ColonyID, doc); err != nil {
				return err
			}
		case prev != doc:
			if err := ex(`UPDATE users SET username_lower = $2, colony_id = $3, doc = $4::jsonb WHERE id = $1`, id, strings.ToLower(u.Username), u.ColonyID, doc); err != nil {
				return err
			}
		}
	}
	for _, tok := range sortedKeys(o.sessions) {
		if _, still := st.Sessions[tok]; !still {
			if err := ex(`DELETE FROM sessions WHERE token = $1`, tok); err != nil {
				return err
			}
		}
	}
	for _, tok := range sortedKeys(st.Sessions) {
		s := st.Sessions[tok]
		doc := jsonOf(s)
		prev, loaded := o.sessions[tok]
		exp := parseTimeOrNil(s.ExpiresAt)
		if exp == nil {
			exp = time.Now().UTC()
		}
		switch {
		case !loaded:
			if err := ex(`INSERT INTO sessions (token, user_id, expires_at, doc) VALUES ($1, $2, $3, $4::jsonb)`, tok, s.UserID, exp, doc); err != nil {
				return err
			}
		case prev != doc:
			if err := ex(`UPDATE sessions SET user_id = $2, expires_at = $3, doc = $4::jsonb WHERE token = $1`, tok, s.UserID, exp, doc); err != nil {
				return err
			}
		}
	}
	for _, id := range sortedKeys(st.Trades) {
		t := st.Trades[id]
		doc := jsonOf(t)
		prev, loaded := o.trades[id]
		switch {
		case !loaded:
			if err := ex(`INSERT INTO trades (id, seller_user_id, buyer_user_id, seller_colony_id, buyer_colony_id, status, created_at, expires_at, doc) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`,
				id, t.SellerUserID, t.BuyerUserID, t.SellerColonyID, t.BuyerColonyID, t.Status, t.CreatedAt, parseTimeOrNil(t.ExpiresAt), doc); err != nil {
				return err
			}
		case prev != doc:
			if err := ex(`UPDATE trades SET status = $2, expires_at = $3, doc = $4::jsonb WHERE id = $1`, id, t.Status, parseTimeOrNil(t.ExpiresAt), doc); err != nil {
				return err
			}
		}
	}
	for _, id := range sortedKeys(st.PendingActions) {
		a := st.PendingActions[id]
		doc := jsonOf(a)
		prev, loaded := o.actions[id]
		switch {
		case !loaded:
			if err := ex(`INSERT INTO pending_actions (id, colony_id, trade_id, user_id, status, proposed_at, doc) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`,
				id, a.ColonyID, a.TradeID, a.UserID, a.Status, a.ProposedAt, doc); err != nil {
				return err
			}
		case prev != doc:
			if err := ex(`UPDATE pending_actions SET status = $2, doc = $3::jsonb WHERE id = $1`, id, a.Status, doc); err != nil {
				return err
			}
		}
	}

	// ---- per-colony child rows ------------------------------------------
	present := map[string]bool{} // envelope ids currently in any loaded outbox
	for _, cid := range sortedKeys(st.Colonies) {
		if !owned(cid) {
			continue
		}
		c := st.Colonies[cid]
		for _, uid := range sortedKeys(c.Accounts) {
			bal := c.Accounts[uid].Balance
			if prev, ok := o.accounts[[2]string{cid, uid}]; !ok {
				if err := ex(`INSERT INTO accounts (colony_id, user_id, balance) VALUES ($1, $2, $3)`, cid, uid, bal); err != nil {
					return err
				}
			} else if prev != bal {
				if err := ex(`UPDATE accounts SET balance = $3 WHERE colony_id = $1 AND user_id = $2`, cid, uid, bal); err != nil {
					return err
				}
			}
		}
		for _, e := range c.Ledger {
			if err := ex(`INSERT INTO ledger_entries (colony_id, seq, doc) VALUES ($1, $2, $3::json)`, cid, e.Seq, jsonOf(e)); err != nil {
				return err
			}
		}
		for _, e := range c.Outbox {
			present[e.ID] = true
			if _, ok := o.outbox[e.ID]; !ok {
				if err := ex(`INSERT INTO outbox (colony_id, envelope_id, to_colony_id, doc) VALUES ($1, $2, $3, $4::json)`, cid, e.ID, e.ToColonyID, jsonOf(e)); err != nil {
					return err
				}
			}
		}
		for _, eid := range sortedKeys(c.ProcessedEnvelopes) {
			if !c.ProcessedEnvelopes[eid] || o.processed[[2]string{cid, eid}] {
				continue
			}
			if err := ex(`INSERT INTO processed_envelopes (colony_id, envelope_id) VALUES ($1, $2)`, cid, eid); err != nil {
				return err
			}
		}
	}
	for _, eid := range sortedKeys(o.outbox) {
		if !present[eid] {
			if err := ex(`DELETE FROM outbox WHERE envelope_id = $1`, eid); err != nil {
				return err
			}
		}
	}
	for _, id := range sortedKeys(st.Bundles) {
		// First writer wins: an imported bundle never overwrites a stored one.
		if err := ex(`INSERT INTO bundles (id, doc) VALUES ($1, $2::json) ON CONFLICT (id) DO NOTHING`, id, jsonOf(st.Bundles[id])); err != nil {
			return err
		}
	}
	return nil
}
