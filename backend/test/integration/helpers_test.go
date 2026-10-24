package integration

import (
	"testing"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/service"
)

// useClock installs a controllable clock for trade/expiry logic and returns a
// function that advances it. Tests using it must not run in parallel.
func useClock(t *testing.T) (advance func(d time.Duration)) {
	t.Helper()
	base := time.Now()
	var offset time.Duration
	prev := domain.NowFn
	domain.NowFn = func() time.Time { return base.Add(offset) }
	t.Cleanup(func() { domain.NowFn = prev })
	return func(d time.Duration) { offset += d }
}

// secondAdmin creates a colony_admin for the given colony and logs them in.
// Dual-control tests need two distinct admins.
func (f *fixture) secondAdmin(t *testing.T, colony, username string) (token, userID string) {
	t.Helper()
	u, err := f.svc.CreateUser(f.admin, service.CreateUserRequest{Username: username, Password: "password-" + username, ColonyID: colony, Roles: []string{"colony_admin"}})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := f.svc.Login(service.LoginRequest{Username: username, Password: "password-" + username})
	if err != nil {
		t.Fatal(err)
	}
	return auth.Token, u.ID
}

// acceptedTrade drives offer -> import at Mars -> accept; the acceptance
// envelope is left in Mars' outbox. Returns the trade.
func (f *fixture) acceptedTrade(t *testing.T, price int64) domain.TradeView {
	t.Helper()
	tr := f.offer(t, price)
	if _, err := f.importInto(f.mars, f.export(t, f.earth, f.mars)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AcceptTrade(f.admin, tr.ID); err != nil {
		t.Fatal(err)
	}
	return tr
}

func onlyType(b domain.Bundle, typ string) domain.Bundle {
	out := b
	out.Messages = nil
	for _, m := range b.Messages {
		if m.Type == typ {
			out.Messages = append(out.Messages, m)
		}
	}
	return out
}
