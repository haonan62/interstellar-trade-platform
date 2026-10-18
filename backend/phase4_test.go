package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- helpers ----

func appErr(t *testing.T, err error) *AppError {
	t.Helper()
	var ae *AppError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AppError, got %v", err)
	}
	return ae
}

func mustLogin(t *testing.T, f *fixture, user, pw, code string) AuthResponse {
	t.Helper()
	r, err := f.svc.Login(LoginRequest{Username: user, Password: pw, TOTPCode: code})
	if err != nil {
		t.Fatalf("login %s: %v", user, err)
	}
	return r
}

func setClock(t *testing.T, now time.Time) *time.Time {
	t.Helper()
	cur := now
	old := lockClock
	lockClock = func() time.Time { return cur }
	t.Cleanup(func() { lockClock = old })
	return &cur
}

func totpNow(t *testing.T, secret string) string {
	t.Helper()
	c, err := totpAt(secret, lockClock().Unix()/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newColonyAdmin(t *testing.T, f *fixture, name, colony string) string {
	t.Helper()
	if _, err := f.svc.CreateUser(f.admin, CreateUserRequest{Username: name, Password: "password-" + name, ColonyID: colony, Roles: []string{"colony_admin"}}); err != nil {
		t.Fatal(err)
	}
	return mustLogin(t, f, name, "password-"+name, "").Token
}

func call(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// ---- TOTP ----

func TestTOTPMatchesRFC6238Vector(t *testing.T) {
	// RFC 6238 appendix B: SHA-1, secret "12345678901234567890", T=59s -> 94287082
	// (8 digits); the 6-digit code is its last six digits.
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	got, err := totpAt(secret, 59/30)
	if err != nil || got != "287082" {
		t.Fatalf("totp = %q, %v; want 287082", got, err)
	}
}

// ---- lockout ----

func TestLoginLockoutAndRecovery(t *testing.T) {
	f := newFixture(t)
	now := setClock(t, time.Now())
	for i := 0; i < maxFailedLogins; i++ {
		if _, err := f.svc.Login(LoginRequest{Username: "seller", Password: "wrong-password"}); appErr(t, err).Status != 401 {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// Locked: even the right password is refused, with the same public answer.
	_, err := f.svc.Login(LoginRequest{Username: "seller", Password: "password-seller"})
	ae := appErr(t, err)
	if ae.Status != 401 || ae.Message != "invalid credentials" || ae.Reason != "locked" {
		t.Fatalf("locked login: %+v", ae)
	}
	// Unknown user answers identically (no account enumeration through status/message).
	_, err = f.svc.Login(LoginRequest{Username: "nobody", Password: "whatever-pass"})
	if u := appErr(t, err); u.Status != ae.Status || u.Message != ae.Message || u.Code != ae.Code {
		t.Fatalf("unknown user differs from locked: %+v vs %+v", u, ae)
	}
	*now = now.Add(lockoutDuration + time.Second)
	mustLogin(t, f, "seller", "password-seller", "")
	// The counter was reset by the success: four more failures do not lock.
	for i := 0; i < maxFailedLogins-1; i++ {
		_, _ = f.svc.Login(LoginRequest{Username: "seller", Password: "wrong-password"})
	}
	mustLogin(t, f, "seller", "password-seller", "")
}

func TestLockoutIsPerAccount(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < maxFailedLogins; i++ {
		_, _ = f.svc.Login(LoginRequest{Username: "seller", Password: "bad-password"})
	}
	mustLogin(t, f, "buyer", "password-buyer", "") // other accounts unaffected
}

// ---- MFA ----

func TestMFAEnrolLoginAndReplay(t *testing.T) {
	f := newFixture(t)
	now := setClock(t, time.Now())
	en, err := f.svc.EnrollMFA(f.admin)
	if err != nil || en.Secret == "" || !strings.HasPrefix(en.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("enroll: %+v %v", en, err)
	}
	if _, err := f.svc.ConfirmMFA(f.admin, MFAConfirmRequest{Code: "000000"}); err == nil {
		t.Fatal("wrong confirmation code accepted")
	}
	if _, err := f.svc.ConfirmMFA(f.admin, MFAConfirmRequest{Code: totpNow(t, en.Secret)}); err != nil {
		t.Fatal(err)
	}
	// Password alone is no longer enough, and says so only after the password is right.
	_, err = f.svc.Login(LoginRequest{Username: "root", Password: "password-root"})
	if ae := appErr(t, err); ae.Code != "mfa_required" {
		t.Fatalf("want mfa_required, got %+v", ae)
	}
	_, err = f.svc.Login(LoginRequest{Username: "root", Password: "bad-password"})
	if ae := appErr(t, err); ae.Code == "mfa_required" {
		t.Fatal("mfa_required leaked for a wrong password")
	}
	// The step used to confirm enrolment cannot be replayed.
	if _, err := f.svc.Login(LoginRequest{Username: "root", Password: "password-root", TOTPCode: totpNow(t, en.Secret)}); err == nil {
		t.Fatal("replayed TOTP step accepted")
	}
	*now = now.Add(31 * time.Second)
	code := totpNow(t, en.Secret)
	mustLogin(t, f, "root", "password-root", code)
	if _, err := f.svc.Login(LoginRequest{Username: "root", Password: "password-root", TOTPCode: code}); err == nil {
		t.Fatal("same code accepted twice")
	}
	// A wrong code counts toward lockout like a wrong password.
	*now = now.Add(31 * time.Second)
	for i := 0; i < maxFailedLogins; i++ {
		_, _ = f.svc.Login(LoginRequest{Username: "root", Password: "password-root", TOTPCode: "123456"})
	}
	_, err = f.svc.Login(LoginRequest{Username: "root", Password: "password-root", TOTPCode: totpNow(t, en.Secret)})
	if ae := appErr(t, err); ae.Reason != "locked" {
		t.Fatalf("want locked, got %+v", ae)
	}
}

func TestAdminMFARequiredGate(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	adminMFARequired.Store(true)
	t.Cleanup(func() { adminMFARequired.Store(false) })

	if _, err := f.svc.ListUsers(f.admin); appErr(t, err).Code != "mfa_enrollment_required" {
		t.Fatalf("admin without MFA must be gated, got %v", err)
	}
	if _, err := f.svc.Me(f.admin); err != nil { // the UI needs this to show the enrolment screen
		t.Fatalf("me: %v", err)
	}
	// Traders are not affected.
	trader := mustLogin(t, f, "seller", "password-seller", "").Token
	if _, err := f.svc.ListUsers(trader); err != nil {
		t.Fatal(err)
	}
	en, err := f.svc.EnrollMFA(f.admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ConfirmMFA(f.admin, MFAConfirmRequest{Code: totpNow(t, en.Secret)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListUsers(f.admin); err != nil {
		t.Fatalf("after enrolment: %v", err)
	}
}

// ---- password reset / change ----

func TestPasswordResetFlow(t *testing.T) {
	f := newFixture(t)
	now := setClock(t, time.Now())
	earthAdmin := newColonyAdmin(t, f, "earthadmin", f.earth)
	victim := mustLogin(t, f, "seller", "password-seller", "").Token

	if _, err := f.svc.IssueResetToken(victim, f.seller); appErr(t, err).Status != 403 {
		t.Fatalf("trader issued a reset: %v", err)
	}
	if _, err := f.svc.IssueResetToken(earthAdmin, f.buyer); appErr(t, err).Status != 403 { // Mars user
		t.Fatalf("colony admin reached another colony: %v", err)
	}
	rt, err := f.svc.IssueResetToken(earthAdmin, f.seller)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.CompleteReset(CompleteResetRequest{ResetToken: rt.ResetToken, NewPassword: "short"}); appErr(t, err).Status != 400 {
		t.Fatal("weak password accepted")
	}
	if err := f.svc.CompleteReset(CompleteResetRequest{ResetToken: f.seller + ".deadbeef", NewPassword: "a-new-password"}); err == nil {
		t.Fatal("forged token accepted")
	}
	if err := f.svc.CompleteReset(CompleteResetRequest{ResetToken: rt.ResetToken, NewPassword: "a-new-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Me(victim); err == nil {
		t.Fatal("old session survived a password reset")
	}
	if _, err := f.svc.Login(LoginRequest{Username: "seller", Password: "password-seller"}); err == nil {
		t.Fatal("old password still works")
	}
	mustLogin(t, f, "seller", "a-new-password", "")
	if err := f.svc.CompleteReset(CompleteResetRequest{ResetToken: rt.ResetToken, NewPassword: "another-password"}); err == nil {
		t.Fatal("reset token is not single-use")
	}
	// Expiry.
	rt2, _ := f.svc.IssueResetToken(earthAdmin, f.seller)
	*now = now.Add(resetTokenTTL + time.Second)
	if err := f.svc.CompleteReset(CompleteResetRequest{ResetToken: rt2.ResetToken, NewPassword: "another-password"}); err == nil {
		t.Fatal("expired reset token accepted")
	}
}

func TestResetClearsLockout(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	for i := 0; i < maxFailedLogins; i++ {
		_, _ = f.svc.Login(LoginRequest{Username: "seller", Password: "bad-password"})
	}
	rt, err := f.svc.IssueResetToken(f.admin, f.seller)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.CompleteReset(CompleteResetRequest{ResetToken: rt.ResetToken, NewPassword: "fresh-password"}); err != nil {
		t.Fatal(err)
	}
	mustLogin(t, f, "seller", "fresh-password", "")
}

func TestChangePassword(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	a := mustLogin(t, f, "seller", "password-seller", "").Token
	b := mustLogin(t, f, "seller", "password-seller", "").Token
	if err := f.svc.ChangePassword(a, ChangePasswordRequest{CurrentPassword: "nope-nope", NewPassword: "brand-new-pass"}); appErr(t, err).Status != 403 {
		t.Fatal("wrong current password accepted")
	}
	if err := f.svc.ChangePassword(a, ChangePasswordRequest{CurrentPassword: "password-seller", NewPassword: "brand-new-pass"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Me(a); err != nil {
		t.Fatal("current session must survive")
	}
	if _, err := f.svc.Me(b); err == nil {
		t.Fatal("other sessions must be revoked")
	}
	mustLogin(t, f, "seller", "brand-new-pass", "")
}

// ---- deactivation / roles ----

func TestDeactivateRevokesSessionsAndBlocksLogin(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	earthAdmin := newColonyAdmin(t, f, "earthadmin", f.earth)
	sess := mustLogin(t, f, "seller", "password-seller", "").Token

	if _, err := f.svc.SetUserActive(sess, f.seller, false); appErr(t, err).Status != 403 {
		t.Fatal("trader deactivated someone")
	}
	if _, err := f.svc.SetUserActive(earthAdmin, f.buyer, false); appErr(t, err).Status != 403 {
		t.Fatal("colony admin deactivated a user of another colony")
	}
	v, err := f.svc.SetUserActive(earthAdmin, f.seller, false)
	if err != nil || v.Active {
		t.Fatalf("deactivate: %+v %v", v, err)
	}
	if _, err := f.svc.Me(sess); err == nil {
		t.Fatal("session survived deactivation")
	}
	if _, err := f.svc.Login(LoginRequest{Username: "seller", Password: "password-seller"}); err == nil {
		t.Fatal("deactivated user can log in")
	}
	if _, err := f.svc.SetUserActive(earthAdmin, f.seller, true); err != nil {
		t.Fatal(err)
	}
	mustLogin(t, f, "seller", "password-seller", "")
}

func TestAdminCannotModifySelfOrSuperAdmin(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	earthAdmin := newColonyAdmin(t, f, "earthadmin", f.earth)
	rootID := ""
	if us, err := f.svc.ListUsers(f.admin); err == nil {
		for _, u := range us {
			if u.Username == "root" {
				rootID = u.ID
			}
		}
	}
	if _, err := f.svc.SetUserActive(earthAdmin, rootID, false); appErr(t, err).Status != 403 {
		t.Fatal("colony admin deactivated super_admin")
	}
	if _, err := f.svc.SetUserActive(f.admin, rootID, false); appErr(t, err).Status != 403 {
		t.Fatal("super_admin deactivated themself (would risk a lock-out of the whole system)")
	}
	if _, err := f.svc.SetUserRoles(f.admin, rootID, SetRolesRequest{Roles: []string{"trader"}}); appErr(t, err).Status != 403 {
		t.Fatal("super_admin demoted themself")
	}
}

func TestSetUserRoles(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	earthAdmin := newColonyAdmin(t, f, "earthadmin", f.earth)
	if _, err := f.svc.SetUserRoles(earthAdmin, f.seller, SetRolesRequest{Roles: []string{"super_admin"}}); appErr(t, err).Status != 403 {
		t.Fatal("colony admin escalated to super_admin")
	}
	if _, err := f.svc.SetUserRoles(earthAdmin, f.seller, SetRolesRequest{Roles: []string{"bogus"}}); appErr(t, err).Status != 400 {
		t.Fatal("invalid role accepted")
	}
	v, err := f.svc.SetUserRoles(earthAdmin, f.seller, SetRolesRequest{Roles: []string{"trader", "relay_operator"}})
	if err != nil || len(v.Roles) != 2 {
		t.Fatalf("set roles: %+v %v", v, err)
	}
	// The change is written to the colony ledger.
	led, _ := f.svc.ListLedger(f.admin, f.earth, 50)
	found := false
	for _, e := range led {
		found = found || e.Type == "user_roles_changed"
	}
	if !found {
		t.Fatal("role change not in ledger")
	}
}

func TestResetMFA(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	tok := mustLogin(t, f, "seller", "password-seller", "").Token
	en, _ := f.svc.EnrollMFA(tok)
	if _, err := f.svc.ConfirmMFA(tok, MFAConfirmRequest{Code: totpNow(t, en.Secret)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.EnrollMFA(tok); appErr(t, err).Status != 409 {
		t.Fatal("re-enrolment over an active authenticator must be refused")
	}
	if _, err := f.svc.ResetMFA(f.admin, f.seller); err != nil {
		t.Fatal(err)
	}
	mustLogin(t, f, "seller", "password-seller", "") // no code needed any more
}

// ---- visibility ----

func TestListUsersIsScoped(t *testing.T) {
	f := newFixture(t)
	setClock(t, time.Now())
	trader := mustLogin(t, f, "seller", "password-seller", "").Token // Earth
	us, err := f.svc.ListUsers(trader)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range us {
		switch {
		case u.ColonyID == f.earth || u.ID == f.seller:
			if u.CreatedAt == "" {
				t.Fatalf("own colony should be full view: %+v", u)
			}
		default:
			if len(u.Roles) != 0 || u.CreatedAt != "" || u.MFAEnabled {
				t.Fatalf("foreign user leaks details: %+v", u)
			}
		}
	}
	all, _ := f.svc.ListUsers(f.admin)
	for _, u := range all {
		if u.CreatedAt == "" {
			t.Fatalf("super_admin should see full views: %+v", u)
		}
	}
}

// ---- HTTP layer ----

func newHandler(t *testing.T, f *fixture, origins ...string) (*API, http.Handler) {
	t.Helper()
	a := NewAPI(f.svc, origins, "")
	return a, a.Handler()
}

func TestBootstrapIsGated(t *testing.T) {
	st := newTestStore(t, t.TempDir()+"/s.json")
	svc := NewService(st)
	a := NewAPI(svc, nil, "").WithBootstrapToken("s3cret-token")
	h := a.Handler()
	body := `{"username":"root","password":"password-root"}`
	if rr := call(h, "POST", "/api/v1/auth/bootstrap", body, nil); rr.Code != 403 {
		t.Fatalf("no token: %d", rr.Code)
	}
	if rr := call(h, "POST", "/api/v1/auth/bootstrap", body, map[string]string{"X-Bootstrap-Token": "guess"}); rr.Code != 403 {
		t.Fatalf("wrong token: %d", rr.Code)
	}
	if needs, _ := svc.NeedsBootstrap(); !needs {
		t.Fatal("a refused bootstrap must not create anything")
	}
	if rr := call(h, "POST", "/api/v1/auth/bootstrap", body, map[string]string{"X-Bootstrap-Token": "s3cret-token"}); rr.Code != 201 {
		t.Fatalf("right token: %d %s", rr.Code, rr.Body)
	}
	if rr := call(h, "POST", "/api/v1/auth/bootstrap", body, map[string]string{"X-Bootstrap-Token": "s3cret-token"}); rr.Code != 409 {
		t.Fatalf("second bootstrap: %d", rr.Code)
	}
	// No token configured at all: refused (fail closed).
	b := NewAPI(NewService(newTestStore(t, t.TempDir()+"/s2.json")), nil, "").Handler()
	if rr := call(b, "POST", "/api/v1/auth/bootstrap", body, map[string]string{"X-Bootstrap-Token": ""}); rr.Code != 403 {
		t.Fatalf("unconfigured: %d", rr.Code)
	}
}

func TestCORSFailsClosed(t *testing.T) {
	f := newFixture(t)
	_, h := newHandler(t, f) // empty allowlist
	rr := call(h, "GET", "/api/v1/healthz", "", map[string]string{"Origin": "https://evil.example"})
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("empty allowlist must not allow any origin")
	}
	_, h = newHandler(t, f, "https://ok.example")
	if got := call(h, "GET", "/api/v1/healthz", "", map[string]string{"Origin": "https://ok.example"}).Header().Get("Access-Control-Allow-Origin"); got != "https://ok.example" {
		t.Fatalf("allowed origin missing: %q", got)
	}
	if got := call(h, "GET", "/api/v1/healthz", "", map[string]string{"Origin": "https://evil.example"}).Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatal("unlisted origin allowed")
	}
}

func TestSecurityHeaders(t *testing.T) {
	f := newFixture(t)
	_, h := newHandler(t, f)
	rr := call(h, "GET", "/api/v1/healthz", "", map[string]string{"X-Forwarded-Proto": "https"})
	hd := rr.Header()
	for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Cache-Control": "no-store"} {
		if hd.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, hd.Get(k), want)
		}
	}
	csp := hd.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self';") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("csp: %s", csp)
	}
	// httptest peers are 192.0.2.1 (not private), so a forged proto header must not enable HSTS.
	if hd.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS set from an untrusted X-Forwarded-Proto")
	}
}

func TestCookieSessionAndCSRF(t *testing.T) {
	f := newFixture(t)
	_, h := newHandler(t, f)
	login := call(h, "POST", "/api/v1/auth/login", `{"username":"seller","password":"password-seller"}`, map[string]string{"X-Session-Mode": "cookie"})
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	var resp AuthResponse
	_ = json.Unmarshal(login.Body.Bytes(), &resp)
	if resp.Token != "" {
		t.Fatal("cookie mode must not return the token to JavaScript")
	}
	var ck *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == sessionCookie {
			ck = c
		}
	}
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Value == "" {
		t.Fatalf("bad session cookie: %+v", ck)
	}
	withCookie := func(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(ck)
		req.Host = "app.example"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := withCookie("GET", "/api/v1/auth/me", "", nil); rr.Code != 200 {
		t.Fatalf("cookie GET: %d", rr.Code)
	}
	// State-changing requests need a matching Origin.
	if rr := withCookie("POST", "/api/v1/auth/logout", "", nil); rr.Code != 403 {
		t.Fatalf("no Origin: %d", rr.Code)
	}
	if rr := withCookie("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": "https://evil.example"}); rr.Code != 403 {
		t.Fatalf("foreign Origin: %d", rr.Code)
	}
	if rr := withCookie("POST", "/api/v1/auth/logout", "", map[string]string{"Origin": "https://app.example"}); rr.Code != 200 {
		t.Fatalf("same Origin: %d %s", rr.Code, rr.Body)
	}
	if rr := withCookie("GET", "/api/v1/auth/me", "", nil); rr.Code != 401 {
		t.Fatalf("session must be gone after logout, got %d", rr.Code)
	}
	// Bearer clients keep working with no Origin (unchanged behaviour).
	tok := mustLogin(t, f, "seller", "password-seller", "").Token
	if rr := call(h, "POST", "/api/v1/auth/logout", "", map[string]string{"Authorization": "Bearer " + tok}); rr.Code != 200 {
		t.Fatalf("bearer logout: %d", rr.Code)
	}
}

func TestRequestIDsAndMetrics(t *testing.T) {
	f := newFixture(t)
	a, h := newHandler(t, f)
	rr := call(h, "GET", "/api/v1/healthz", "", map[string]string{"X-Request-Id": "abc12345-req"})
	if rr.Header().Get("X-Request-Id") != "abc12345-req" {
		t.Fatalf("request id not echoed: %q", rr.Header().Get("X-Request-Id"))
	}
	// Hostile values never reach logs or headers.
	rr = call(h, "GET", "/api/v1/healthz", "", map[string]string{"X-Request-Id": "evil\nforged=1"})
	if id := rr.Header().Get("X-Request-Id"); strings.ContainsAny(id, "\n ") || id == "" {
		t.Fatalf("unsafe request id accepted: %q", id)
	}
	rr = call(h, "GET", "/api/v1/healthz", "", map[string]string{"Traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"})
	if !strings.Contains(rr.Header().Get("Traceresponse"), "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("trace id not propagated: %q", rr.Header().Get("Traceresponse"))
	}
	if rr := call(h, "GET", "/api/v1/readyz", "", nil); rr.Code != 200 {
		t.Fatalf("readyz: %d", rr.Code)
	}
	_ = call(h, "POST", "/api/v1/auth/login", `{"username":"seller","password":"nope-nope"}`, nil)
	m := httptest.NewRecorder()
	a.MetricsHandler().ServeHTTP(m, httptest.NewRequest("GET", "/metrics", nil))
	out := m.Body.String()
	for _, want := range []string{
		`itp_http_requests_total{method="GET",route="/api/v1/healthz",code="200"}`,
		`itp_http_requests_total{method="POST",route="/api/v1/auth/login",code="401"} 1`,
		`itp_login_failures_total{reason="bad_password"} 1`,
		`itp_http_request_duration_seconds_bucket{method="GET",route="/api/v1/healthz",le="+Inf"}`,
		"itp_http_in_flight_requests 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics missing %q\n%s", want, out)
		}
	}
	// Unknown paths share one label: no unbounded cardinality.
	_ = call(h, "GET", "/api/v1/zzz-1", "", nil)
	_ = call(h, "GET", "/api/v1/zzz-2", "", nil)
	m = httptest.NewRecorder()
	a.MetricsHandler().ServeHTTP(m, httptest.NewRequest("GET", "/metrics", nil))
	if strings.Contains(m.Body.String(), "zzz") {
		t.Error("raw paths leaked into metric labels")
	}
}

func TestAuditLogCarriesIdentityAndNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	f := newFixture(t)
	setClock(t, time.Now())
	_, h := newHandler(t, f)
	bearer := map[string]string{"Authorization": "Bearer " + f.admin, "X-Request-Id": "req-audit-0001"}
	_ = call(h, "POST", "/api/v1/auth/login", `{"username":"seller","password":"super-secret-guess"}`, map[string]string{"X-Request-Id": "req-login-0001"})
	rr := call(h, "POST", "/api/v1/users/"+f.seller+"/reset-password", "", bearer)
	if rr.Code != 200 {
		t.Fatalf("issue reset: %d %s", rr.Code, rr.Body)
	}
	var rt ResetTokenView
	_ = json.Unmarshal(rr.Body.Bytes(), &rt)

	logs := buf.String()
	if !strings.Contains(logs, `"event":"auth.login"`) || !strings.Contains(logs, `"outcome":"failure"`) || !strings.Contains(logs, `"request_id":"req-login-0001"`) {
		t.Errorf("login failure not audited with request id:\n%s", logs)
	}
	if !strings.Contains(logs, `"event":"password.reset_issued"`) || !strings.Contains(logs, `"target_user_id":"`+f.seller+`"`) || !strings.Contains(logs, `"request_id":"req-audit-0001"`) {
		t.Errorf("reset issue not audited with actor/target/request id:\n%s", logs)
	}
	adminID := f.svc.ActorID(f.admin)
	if adminID == "" || !strings.Contains(logs, `"user_id":"`+adminID+`"`) {
		t.Errorf("audit lines must carry the acting user id")
	}
	for _, secret := range []string{"super-secret-guess", rt.ResetToken, f.admin} {
		if strings.Contains(logs, secret) {
			t.Errorf("secret leaked into logs: %q", secret)
		}
	}
}
