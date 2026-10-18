package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// verifyStateIntegrity checks invariants that must hold before a state file is
// trusted as the source of a migration:
//   - every colony's ledger is a valid hash chain with contiguous sequence numbers;
//   - sum(balances) + escrow == total minted supply (money conservation).
func verifyStateIntegrity(st *AppState) error {
	var balances, escrow, minted int64
	for _, id := range sortedKeys(st.Colonies) {
		c := st.Colonies[id]
		prev := ""
		for i, e := range c.Ledger {
			if e.Seq != int64(i+1) {
				return fmt.Errorf("colony %s: ledger entry %d has seq %d (gap or reorder)", id, i, e.Seq)
			}
			if e.PrevHash != prev {
				return fmt.Errorf("colony %s: ledger seq %d breaks the hash chain", id, e.Seq)
			}
			if want := hashLedgerEntry(e.Seq, e.ColonyID, e.Time, e.Type, e.ActorUserID, e.Payload, e.PrevHash); want != e.Hash {
				return fmt.Errorf("colony %s: ledger seq %d has a wrong hash", id, e.Seq)
			}
			prev = e.Hash
		}
		if c.NextLedgerSeq != int64(len(c.Ledger)) {
			return fmt.Errorf("colony %s: next_ledger_seq %d != ledger length %d", id, c.NextLedgerSeq, len(c.Ledger))
		}
		for _, a := range c.Accounts {
			if a.Balance < 0 {
				return fmt.Errorf("colony %s: negative balance for %s", id, a.UserID)
			}
			balances += a.Balance
		}
		minted += c.MintedTotal
	}
	for _, t := range st.Trades {
		if isEscrowed(t.Status) {
			escrow += t.Price
		}
	}
	if balances+escrow != minted {
		return fmt.Errorf("conservation violated: balances %d + escrow %d != minted %d", balances, escrow, minted)
	}
	return nil
}

// ImportState loads a complete state (typically from state.json) into an EMPTY
// PostgreSQL database in a single transaction after verifying its integrity.
// It refuses to run against a database that already has data, so running it
// twice is safe.
func (s *Store) ImportState(ctx context.Context, st *AppState) error {
	if s.pg == nil {
		return errors.New("ImportState requires the PostgreSQL store")
	}
	if err := verifyStateIntegrity(st); err != nil {
		return fmt.Errorf("refusing to migrate: %w", err)
	}
	tx, err := s.pg.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM users) + (SELECT count(*) FROM colonies)`).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return errors.New("target database is not empty; refusing to import")
	}
	if err := s.pg.persist(ctx, tx, st, newOrig(), Scope{}); err != nil {
		return mapPGError(err)
	}
	return tx.Commit()
}
