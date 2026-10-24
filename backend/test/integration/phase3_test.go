package integration

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/httpapi"
	"interstellar-trade-platform/backend/internal/service"
	"interstellar-trade-platform/backend/internal/storage"
)

func TestArgon2idRoundTrip(t *testing.T) {
	h, err := domain.HashPasswordArgon2id("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected PHC string %q", h)
	}
	if !domain.VerifyArgon2id("correct horse", h) || domain.VerifyArgon2id("wrong", h) {
		t.Fatal("verify mismatch")
	}
	h2, _ := domain.HashPasswordArgon2id("correct horse")
	if h == h2 {
		t.Fatal("salts must differ")
	}
	// A tampered record must not be able to demand huge memory.
	bomb := strings.Replace(h, "m=19456", "m=4000000", 1)
	if domain.VerifyArgon2id("correct horse", bomb) {
		t.Fatal("absurd parameters accepted")
	}
}

func TestLegacyHashIsUpgradedOnLogin(t *testing.T) {
	f := newFixture(t)
	const pw = "password-seller"
	salt := "legacy-salt-value"
	legacy := base64.StdEncoding.EncodeToString(domain.DerivePBKDF2([]byte(pw), []byte(salt), domain.PasswordIterations, 32))
	_, _ = storage.StoreUpdate(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
		s.Users[f.seller].PasswordSalt, s.Users[f.seller].PasswordHash = salt, legacy
		return struct{}{}, nil
	})
	if _, err := f.svc.Login(service.LoginRequest{Username: "seller", Password: "nope-nope"}); err == nil {
		t.Fatal("wrong password accepted against legacy hash")
	}
	if _, err := f.svc.Login(service.LoginRequest{Username: "seller", Password: pw}); err != nil {
		t.Fatalf("legacy login failed: %v", err)
	}
	hash, saltAfter := "", "x"
	_, _ = storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
		hash, saltAfter = s.Users[f.seller].PasswordHash, s.Users[f.seller].PasswordSalt
		return struct{}{}, nil
	})
	if !domain.IsArgon2idHash(hash) || saltAfter != "" {
		t.Fatalf("hash not upgraded: %q salt=%q", hash, saltAfter)
	}
	if _, err := f.svc.Login(service.LoginRequest{Username: "seller", Password: pw}); err != nil {
		t.Fatalf("login after upgrade failed: %v", err)
	}
}

func TestLoginRejectsUnknownAndInactiveUsers(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Login(service.LoginRequest{Username: "ghost", Password: "whatever-pw"}); err == nil {
		t.Fatal("unknown user logged in")
	}
	_, _ = storage.StoreUpdate(f.svc.Store(), func(s *domain.AppState) (struct{}, error) {
		s.Users[f.seller].Active = false
		return struct{}{}, nil
	})
	if _, err := f.svc.Login(service.LoginRequest{Username: "seller", Password: "password-seller"}); err == nil {
		t.Fatal("inactive user logged in")
	}
}

func TestUnauthorisedCallersDoNotTriggerHashing(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateUser("bogus-token", service.CreateUserRequest{Username: "x", Password: "password-x", ColonyID: f.mars}); err == nil {
		t.Fatal("create user with bad token succeeded")
	}
	if _, err := f.svc.Bootstrap(service.BootstrapRequest{Username: "again", Password: "password-again"}); err == nil {
		t.Fatal("second bootstrap succeeded")
	}
}

func TestSessionsAreCappedPerUser(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < service.MaxSessionsPerUser+5; i++ {
		if _, err := f.svc.Login(service.LoginRequest{Username: "seller", Password: "password-seller"}); err != nil {
			t.Fatal(err)
		}
	}
	n, _ := storage.StoreRead(f.svc.Store(), func(s *domain.AppState) (int, error) {
		c := 0
		for _, sess := range s.Sessions {
			if sess.UserID == f.seller {
				c++
			}
		}
		return c, nil
	})
	if n != service.MaxSessionsPerUser {
		t.Fatalf("%d sessions kept, want %d", n, service.MaxSessionsPerUser)
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	f := newFixture(t)
	h := httpapi.NewAPI(f.svc, []string{"http://x"}, "").Handler()
	big := `{"username":"` + strings.Repeat("a", int(httpapi.MaxBodyBytes)+10) + `"}`
	// Declared length known: rejected before reading.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(big)))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rr.Code)
	}
	// Unknown length (chunked): the read is cut off and the request fails.
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(big))
	req.ContentLength = -1
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("chunked oversize status %d, want 400", rr.Code)
	}
}

func TestServerTimeoutsAreSet(t *testing.T) {
	s := httpapi.NewHTTPServer(":0", http.NewServeMux())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 || s.MaxHeaderBytes == 0 {
		t.Fatalf("missing server limits: %+v", s)
	}
}

func TestGracefulShutdownDrainsInFlightRequests(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		started.Store(true)
		<-release
		_, _ = w.Write([]byte("done"))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- httpapi.ServeUntil(ctx, httpapi.NewHTTPServer("", mux), ln, 5*time.Second) }()

	type res struct {
		body string
		err  error
	}
	got := make(chan res, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err != nil {
			got <- res{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b := make([]byte, 16)
		n, _ := resp.Body.Read(b)
		got <- res{body: string(b[:n])}
	}()
	for i := 0; !started.Load(); i++ {
		if i > 200 {
			t.Fatal("request never reached the handler")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel() // SIGTERM equivalent while the request is in flight
	select {
	case <-served:
		t.Fatal("server exited before the in-flight request finished")
	case <-time.After(200 * time.Millisecond):
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), 300*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatal("still accepting new connections during shutdown")
	}
	close(release)
	r := <-got
	if r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request was cut off: %+v", r)
	}
	if err := <-served; err != nil {
		t.Fatalf("serveUntil: %v", err)
	}
}

func TestLedgerPagination(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		if _, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1}); err != nil {
			t.Fatal(err)
		}
	}
	full, err := f.svc.ListLedgerPage(f.admin, f.mars, 1000, 0)
	if err != nil || len(full) < 6 {
		t.Fatalf("need a ledger with several entries: %d %v", len(full), err)
	}
	var walked []int64
	before := int64(0)
	for {
		page, err := f.svc.ListLedgerPage(f.admin, f.mars, 2, before)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatalf("page too large: %d", len(page))
		}
		// pages walk backwards; each page is ascending
		for i := len(page) - 1; i >= 0; i-- {
			walked = append(walked, page[i].Seq)
		}
		before = page[0].Seq
	}
	if len(walked) != len(full) {
		t.Fatalf("walked %d entries, ledger has %d", len(walked), len(full))
	}
	for i, seq := range walked {
		if want := full[len(full)-1-i].Seq; seq != want {
			t.Fatalf("entry %d: seq %d, want %d", i, seq, want)
		}
	}
	if def, _ := f.svc.ListLedger(f.admin, f.mars, 0); len(def) != len(full) {
		t.Fatalf("default limit returned %d entries", len(def))
	}
	if _, err := f.svc.ListLedgerPage(f.admin, "col_nope", 1, 0); err == nil {
		t.Fatal("unknown colony accepted")
	}
}

func TestOneLoginDoesNotHoldTheStoreLock(t *testing.T) {
	// Deterministic counterpart of the timing test: while a login is verifying
	// (hashing), a write must be able to proceed. We hold hashSem permits so
	// every Login blocks inside hashing, then check a write still completes.
	f := newFixture(t)
	for i := 0; i < cap(domain.HashSem); i++ {
		domain.HashSem <- struct{}{}
	}
	done := make(chan struct{})
	go func() {
		_, _ = f.svc.Login(service.LoginRequest{Username: "seller", Password: "password-seller"})
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // login is now parked in verifyPassword
	wrote := make(chan error, 1)
	go func() {
		_, err := f.svc.Mint(f.admin, service.MintRequest{ColonyID: f.mars, UserID: f.buyer, Amount: 1})
		wrote <- err
	}()
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("write blocked while a login was hashing (%d permits held)", cap(domain.HashSem))
	}
	for i := 0; i < cap(domain.HashSem); i++ {
		<-domain.HashSem
	}
	<-done
}
