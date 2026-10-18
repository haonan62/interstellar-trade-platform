package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// pgOrig remembers what was loaded so persist can write only the differences.
type pgOrig struct {
	colonies  map[string]string // header JSON (Go-marshalled)
	users     map[string]string
	sessions  map[string]string
	trades    map[string]string
	actions   map[string]string
	locked    map[string]bool // colonies loaded for update (accounts/ledger/outbox are ours)
	accounts  map[[2]string]int64
	outbox    map[string]string  // envelope id -> colony id
	processed map[[2]string]bool // (colony, envelope)
	_         struct{}
}

func newOrig() *pgOrig {
	return &pgOrig{colonies: map[string]string{}, users: map[string]string{}, sessions: map[string]string{}, trades: map[string]string{}, actions: map[string]string{}, locked: map[string]bool{}, accounts: map[[2]string]int64{}, outbox: map[string]string{}, processed: map[[2]string]bool{}}
}

type idSet map[string]bool

func (s idSet) add(ids ...string) {
	for _, id := range ids {
		if id != "" {
			s[id] = true
		}
	}
}
func (s idSet) list() []string {
	out := make([]string, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func forUpdate(write bool) string {
	if write {
		return " FOR UPDATE"
	}
	return ""
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }

func colonyHeader(c *Colony) *Colony {
	h := *c
	h.Accounts, h.Ledger, h.Outbox, h.ProcessedEnvelopes = nil, nil, nil, nil
	return &h
}

// load builds the working AppState for sc. Lock order is always: advisory ->
// colonies (sorted) -> users -> trades -> actions -> sessions -> accounts, so
// concurrent requests can never deadlock.
func (p *pgStore) load(ctx context.Context, tx *sql.Tx, sc Scope, write bool) (*AppState, *pgOrig, error) {
	st := newEmptyState()
	o := newOrig()
	all := sc.isAll()
	users, trades, colonies, actions := idSet{}, idSet{}, idSet{}, idSet{}
	var tokens []string
	userSessions := append([]string(nil), sc.UserSessions...)

	// ---- discovery (unlocked reads of immutable relations) --------------
	var actor *User
	if all {
		for _, q := range []struct {
			set idSet
			sql string
		}{{users, `SELECT id FROM users`}, {trades, `SELECT id FROM trades`}, {colonies, `SELECT id FROM colonies`}, {actions, `SELECT id FROM pending_actions`}} {
			ids, err := p.strings(ctx, tx, q.sql)
			if err != nil {
				return nil, nil, err
			}
			q.set.add(ids...)
		}
	} else {
		users.add(sc.Users...)
		trades.add(sc.Trades...)
		colonies.add(sc.Colonies...)
		actions.add(sc.Actions...)
		if len(sc.Tokens) > 0 {
			rows, err := p.strings(ctx, tx, `SELECT user_id FROM sessions WHERE token = ANY($1) AND expires_at > now()`, sc.Tokens)
			if err != nil {
				return nil, nil, err
			}
			users.add(rows...)
			tokens = sc.Tokens
			if len(rows) > 0 {
				actor, err = p.userByID(ctx, tx, rows[0])
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if len(sc.Usernames) > 0 {
			ids, err := p.strings(ctx, tx, `SELECT id FROM users WHERE username_lower = ANY($1)`, sc.Usernames)
			if err != nil {
				return nil, nil, err
			}
			users.add(ids...)
		}
		if sc.AllUsers {
			limit := sc.UserLimit
			if limit <= 0 {
				limit = 1000
			}
			ids, err := p.strings(ctx, tx, `SELECT id FROM users ORDER BY id LIMIT $1`, limit)
			if err != nil {
				return nil, nil, err
			}
			users.add(ids...)
		}
		if sc.AllActions {
			ids, err := p.strings(ctx, tx, `SELECT id FROM pending_actions ORDER BY proposed_at DESC, id DESC LIMIT 500`)
			if err != nil {
				return nil, nil, err
			}
			actions.add(ids...)
		}
		if sc.TradesForActor > 0 && actor != nil {
			q, args := visibleTradesSQL(actor, "id", sc.TradesForActor)
			ids, err := p.strings(ctx, tx, q, args...)
			if err != nil {
				return nil, nil, err
			}
			trades.add(ids...)
		}
		if len(actions) > 0 {
			rows, err := tx.QueryContext(ctx, `SELECT colony_id, trade_id, user_id FROM pending_actions WHERE id = ANY($1)`, actions.list())
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var c, t, u string
				if err := rows.Scan(&c, &t, &u); err != nil {
					_ = rows.Close()
					return nil, nil, err
				}
				colonies.add(c)
				trades.add(t)
				users.add(u)
			}
			if err := rows.Close(); err != nil {
				return nil, nil, err
			}
		}
		if len(trades) > 0 {
			rows, err := tx.QueryContext(ctx, `SELECT seller_user_id, buyer_user_id, seller_colony_id, buyer_colony_id FROM trades WHERE id = ANY($1)`, trades.list())
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var su, bu, sc2, bc string
				if err := rows.Scan(&su, &bu, &sc2, &bc); err != nil {
					_ = rows.Close()
					return nil, nil, err
				}
				users.add(su, bu)
				colonies.add(sc2, bc)
			}
			if err := rows.Close(); err != nil {
				return nil, nil, err
			}
		}
		if sc.UserColonies && len(users) > 0 {
			ids, err := p.strings(ctx, tx, `SELECT DISTINCT colony_id FROM users WHERE id = ANY($1) AND colony_id <> ''`, users.list())
			if err != nil {
				return nil, nil, err
			}
			colonies.add(ids...)
		}
	}

	// ---- lock colonies (sorted), then load everything ------------------
	lockSuffix := forUpdate(write)
	lockedList := colonies.list()
	if len(lockedList) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, doc::text FROM colonies WHERE id = ANY($1) ORDER BY id`+lockSuffix, lockedList)
		if err != nil {
			return nil, nil, err
		}
		if err := p.scanColonies(rows, st, o, write); err != nil {
			return nil, nil, err
		}
	}
	if sc.ColonyHeaders || sc.Ledger != nil || sc.AccountsLimit > 0 {
		q, args := `SELECT id, doc::text FROM colonies ORDER BY id`, []any{}
		if !sc.ColonyHeaders && sc.Ledger != nil {
			q, args = `SELECT id, doc::text FROM colonies WHERE id = $1`, []any{sc.Ledger.ColonyID}
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, nil, err
		}
		if err := p.scanColonies(rows, st, o, false); err != nil { // header-only: no lock
			return nil, nil, err
		}
	}

	// accounts listing for AccountsLimit adds users before users are loaded
	type acctRow struct {
		c, u string
		b    int64
	}
	var listed []acctRow
	if sc.AccountsLimit > 0 {
		q, args := `SELECT colony_id, user_id, balance FROM accounts ORDER BY colony_id, user_id LIMIT $1`, []any{sc.AccountsLimit}
		if actor != nil && !hasRole(actor, "super_admin") {
			q, args = `SELECT colony_id, user_id, balance FROM accounts WHERE colony_id = $1 ORDER BY colony_id, user_id LIMIT $2`, []any{actor.ColonyID, sc.AccountsLimit}
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var r acctRow
			if err := rows.Scan(&r.c, &r.u, &r.b); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			listed = append(listed, r)
			users.add(r.u)
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}

	if len(users) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, doc::text FROM users WHERE id = ANY($1) ORDER BY id`+lockSuffix, users.list())
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id, doc string
			if err := rows.Scan(&id, &doc); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			var u User
			if err := json.Unmarshal([]byte(doc), &u); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			st.Users[id] = &u
			o.users[id] = jsonOf(&u)
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	if len(trades) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, doc::text FROM trades WHERE id = ANY($1) ORDER BY id`+lockSuffix, trades.list())
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id, doc string
			if err := rows.Scan(&id, &doc); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			var t Trade
			if err := json.Unmarshal([]byte(doc), &t); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			st.Trades[id] = &t
			o.trades[id] = jsonOf(&t)
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	if len(actions) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, doc::text FROM pending_actions WHERE id = ANY($1) ORDER BY id`+lockSuffix, actions.list())
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var id, doc string
			if err := rows.Scan(&id, &doc); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			var a PendingAction
			if err := json.Unmarshal([]byte(doc), &a); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			st.PendingActions[id] = &a
			o.actions[id] = jsonOf(&a)
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	if all || len(tokens) > 0 || len(userSessions) > 0 {
		q, args := `SELECT token, doc::text FROM sessions WHERE token = ANY($1) OR user_id = ANY($2) ORDER BY token`+lockSuffix, []any{tokens, userSessions}
		if all {
			q, args = `SELECT token, doc::text FROM sessions ORDER BY token`+lockSuffix, nil
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var tok, doc string
			if err := rows.Scan(&tok, &doc); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			var s Session
			if err := json.Unmarshal([]byte(doc), &s); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			st.Sessions[tok] = &s
			o.sessions[tok] = jsonOf(&s)
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}

	// accounts of loaded users in locked colonies (or all in the slow path)
	if len(lockedList) > 0 && (len(users) > 0 || all) {
		q, args := `SELECT colony_id, user_id, balance FROM accounts WHERE colony_id = ANY($1) AND user_id = ANY($2) ORDER BY colony_id, user_id`+lockSuffix, []any{lockedList, users.list()}
		if all {
			q, args = `SELECT colony_id, user_id, balance FROM accounts ORDER BY colony_id, user_id`+lockSuffix, nil
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var r acctRow
			if err := rows.Scan(&r.c, &r.u, &r.b); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			if col := st.Colonies[r.c]; col != nil {
				col.Accounts[r.u] = &Account{UserID: r.u, ColonyID: r.c, Balance: r.b}
				o.accounts[[2]string{r.c, r.u}] = r.b
			}
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	for _, r := range listed { // read-only listing (AccountsLimit)
		if col := st.Colonies[r.c]; col != nil {
			if _, ok := col.Accounts[r.u]; !ok {
				col.Accounts[r.u] = &Account{UserID: r.u, ColonyID: r.c, Balance: r.b}
			}
		}
	}

	if write && len(lockedList) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT colony_id, envelope_id, doc::text FROM outbox WHERE colony_id = ANY($1) ORDER BY ord`, lockedList)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var cid, eid, doc string
			if err := rows.Scan(&cid, &eid, &doc); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			var e Envelope
			if err := json.Unmarshal([]byte(doc), &e); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			if col := st.Colonies[cid]; col != nil {
				col.Outbox = append(col.Outbox, &e)
				o.outbox[eid] = cid
			}
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	if len(lockedList) > 0 && (len(sc.EnvelopeIDs) > 0 || all) {
		q, args := `SELECT colony_id, envelope_id FROM processed_envelopes WHERE colony_id = ANY($1) AND envelope_id = ANY($2)`, []any{lockedList, sc.EnvelopeIDs}
		if all {
			q, args = `SELECT colony_id, envelope_id FROM processed_envelopes`, nil
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var cid, eid string
			if err := rows.Scan(&cid, &eid); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			if col := st.Colonies[cid]; col != nil {
				col.ProcessedEnvelopes[eid] = true
				o.processed[[2]string{cid, eid}] = true
			}
		}
		if err := rows.Close(); err != nil {
			return nil, nil, err
		}
	}
	if sc.Ledger != nil {
		if col := st.Colonies[sc.Ledger.ColonyID]; col != nil {
			limit := sc.Ledger.Limit
			if limit <= 0 || limit > MaxLedgerLimit {
				limit = MaxLedgerLimit
			}
			rows, err := tx.QueryContext(ctx, `SELECT doc::text FROM ledger_entries WHERE colony_id = $1 AND ($2 = 0 OR seq < $2) ORDER BY seq DESC LIMIT $3`, sc.Ledger.ColonyID, sc.Ledger.BeforeSeq, limit)
			if err != nil {
				return nil, nil, err
			}
			var page []*LedgerEntry
			for rows.Next() {
				var doc string
				if err := rows.Scan(&doc); err != nil {
					_ = rows.Close()
					return nil, nil, err
				}
				var e LedgerEntry
				if err := json.Unmarshal([]byte(doc), &e); err != nil {
					_ = rows.Close()
					return nil, nil, err
				}
				page = append(page, &e)
			}
			if err := rows.Close(); err != nil {
				return nil, nil, err
			}
			for i := len(page) - 1; i >= 0; i-- { // ascending
				col.Ledger = append(col.Ledger, page[i])
			}
		}
	}
	if sc.Counts {
		if err := p.loadCounts(ctx, tx, st, actor); err != nil {
			return nil, nil, err
		}
	}
	if write && sc.PruneSessions {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
			return nil, nil, err
		}
	}
	return st, o, nil
}

func (p *pgStore) scanColonies(rows *sql.Rows, st *AppState, o *pgOrig, lock bool) error {
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, doc string
		if err := rows.Scan(&id, &doc); err != nil {
			return err
		}
		if _, exists := st.Colonies[id]; exists {
			continue // already loaded (locked) earlier
		}
		var c Colony
		if err := json.Unmarshal([]byte(doc), &c); err != nil {
			return err
		}
		ensureColonyMaps(&c)
		st.Colonies[id] = &c
		o.colonies[id] = jsonOf(colonyHeader(&c))
		if lock {
			o.locked[id] = true
		}
	}
	return rows.Err()
}

func (p *pgStore) strings(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *pgStore) userByID(ctx context.Context, tx *sql.Tx, id string) (*User, error) {
	var doc string
	err := tx.QueryRowContext(ctx, `SELECT doc::text FROM users WHERE id = $1`, id).Scan(&doc)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var u User
	if err := json.Unmarshal([]byte(doc), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// visibleTradesSQL returns a query selecting `col` of trades the actor may see
// (same rule as tradeVisibleToActor), newest first.
func visibleTradesSQL(actor *User, col string, limit int) (string, []any) {
	sel := "SELECT " + col + " FROM trades"
	order := " ORDER BY created_at DESC, id DESC"
	if hasRole(actor, "super_admin") {
		return sel + order + " LIMIT $1", []any{limit}
	}
	conds := []string{"seller_user_id = $1", "buyer_user_id = $1"}
	args := []any{actor.ID}
	if actor.ColonyID != "" {
		conds = append(conds, "seller_colony_id = $2", "buyer_colony_id = $2")
		args = append(args, actor.ColonyID)
	}
	args = append(args, limit)
	return sel + " WHERE " + strings.Join(conds, " OR ") + order + fmt.Sprintf(" LIMIT $%d", len(args)), args
}

func (p *pgStore) loadCounts(ctx context.Context, tx *sql.Tx, st *AppState, actor *User) error {
	c := &StateCounts{AccountsByColony: map[string]int{}, TradesByColony: map[string]int{}}
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM colonies), (SELECT count(*) FROM accounts)`).Scan(&c.Users, &c.Colonies, &c.Accounts); err != nil {
		return err
	}
	if actor != nil {
		q, args := visibleTradesSQL(actor, "count(*)", 1<<30)
		q = strings.Replace(q, " ORDER BY created_at DESC, id DESC", "", 1)
		q = q[:strings.LastIndex(q, " LIMIT")]
		if err := tx.QueryRowContext(ctx, q, args[:len(args)-1]...).Scan(&c.VisibleTrades); err != nil {
			return err
		}
	}
	for _, q := range []struct {
		sql string
		dst map[string]int
	}{
		{`SELECT colony_id, count(*) FROM accounts GROUP BY colony_id`, c.AccountsByColony},
		{`SELECT cid, count(*) FROM (SELECT seller_colony_id AS cid FROM trades UNION ALL SELECT buyer_colony_id FROM trades) x GROUP BY cid`, c.TradesByColony},
	} {
		rows, err := tx.QueryContext(ctx, q.sql)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				_ = rows.Close()
				return err
			}
			q.dst[id] = n
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	st.Counts = c
	return nil
}
