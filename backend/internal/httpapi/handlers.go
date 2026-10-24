package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/service"
)

type API struct {
	service        *service.Service
	allowedOrigins map[string]bool
	staticDir      string
	metrics        *Metrics
	// bootstrapToken gates POST /auth/bootstrap (header X-Bootstrap-Token).
	// Empty means bootstrap is refused outright (fail closed).
	bootstrapToken string
}

func NewAPI(service *service.Service, allowedOrigins []string, staticDir string) *API {
	origins := map[string]bool{}
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o != "" {
			origins[o] = true
		}
	}
	return &API{service: service, allowedOrigins: origins, staticDir: staticDir, metrics: NewMetrics()}
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", a.handleHealthz)
	mux.HandleFunc("GET /api/v1/readyz", a.handleReadyz)
	mux.HandleFunc("GET /api/v1/bootstrap", a.handleNeedsBootstrap)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", a.handleBootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", a.handleMe)
	a.securityRoutes(mux)
	mux.HandleFunc("GET /api/v1/dashboard", a.handleDashboard)
	mux.HandleFunc("GET /api/v1/colonies", a.handleListColonies)
	mux.HandleFunc("POST /api/v1/colonies", a.handleCreateColony)
	mux.HandleFunc("POST /api/v1/colonies/{id}/trust", a.handleTrustPeer)
	mux.HandleFunc("GET /api/v1/users", a.handleListUsers)
	mux.HandleFunc("POST /api/v1/users", a.handleCreateUser)
	mux.HandleFunc("GET /api/v1/accounts", a.handleListAccounts)
	mux.HandleFunc("POST /api/v1/accounts/mint", a.handleMint)
	mux.HandleFunc("GET /api/v1/trades", a.handleListTrades)
	mux.HandleFunc("GET /api/v1/trades/{id}", a.handleGetTrade)
	mux.HandleFunc("POST /api/v1/trades/offers", a.handleCreateOffer)
	mux.HandleFunc("POST /api/v1/trades/{id}/accept", a.handleAcceptTrade)
	mux.HandleFunc("POST /api/v1/trades/{id}/cancel", a.handleCancelTrade)
	mux.HandleFunc("POST /api/v1/trades/{id}/force-refund", a.handleForceRefund)
	mux.HandleFunc("POST /api/v1/colonies/{id}/policy", a.handleSetPolicy)
	mux.HandleFunc("GET /api/v1/approvals", a.handleListApprovals)
	mux.HandleFunc("POST /api/v1/approvals/{id}/approve", a.handleApprove)
	mux.HandleFunc("POST /api/v1/approvals/{id}/reject", a.handleReject)
	mux.HandleFunc("POST /api/v1/relay/export", a.handleExportBundle)
	mux.HandleFunc("POST /api/v1/relay/import", a.handleImportBundle)
	mux.HandleFunc("GET /api/v1/ledger", a.handleListLedger)
	if a.staticDir != "" {
		if st, err := os.Stat(a.staticDir); err == nil && st.IsDir() {
			fs := http.FileServer(http.Dir(a.staticDir))
			mux.Handle("/", spaFileServer(a.staticDir, fs))
		}
	}
	return a.withObs(a.withSecurityHeaders(withBodyLimit(a.withCORS(a.withSession(mux)))))
}
func (a *API) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Fail closed: only explicitly allowed origins get CORS headers. The
		// UI is served from the same origin and needs none.
		if origin != "" && a.allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Session-Mode, X-Bootstrap-Token")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func spaFileServer(root string, fs http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(root, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if strings.HasPrefix(r.URL.Path, "/assets/") { // vite content-hashes these names
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fs.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}
func decodeJSON(r *http.Request, out any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (a *API) writeError(w http.ResponseWriter, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		writeJSON(w, appErr.Status, appErr)
		return
	}
	slog.Error("internal error", "error", err.Error())
	writeJSON(w, http.StatusInternalServerError, &domain.AppError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error"})
}
func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
func (a *API) handleNeedsBootstrap(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.NeedsBootstrap()
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needs_bootstrap": v})
}
func (a *API) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	// The one-time secret is checked before the body is even parsed.
	if !a.bootstrapAllowed(r) {
		a.audit(r, "auth.bootstrap", "failure", "reason", "bad_or_missing_bootstrap_token")
		a.writeError(w, domain.Forbidden("a valid bootstrap token is required (X-Bootstrap-Token)"))
		return
	}
	var req service.BootstrapRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.Bootstrap(req)
	if err != nil {
		a.audit(r, "auth.bootstrap", "failure", "reason", err.Error())
		a.writeError(w, err)
		return
	}
	reqInfoFrom(r.Context()).UserID = v.User.ID
	a.audit(r, "auth.bootstrap", "success", "username", v.User.Username)
	a.respondAuth(w, r, http.StatusCreated, v)
}
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req service.LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.Login(req)
	if err != nil {
		reason := ""
		var ae *domain.AppError
		if errors.As(err, &ae) {
			reason = ae.Reason
		}
		a.audit(r, "auth.login", "failure", "username", service.NormalizeUsername(req.Username), "reason", reason)
		a.metrics.Inc("itp_login_failures_total", "reason", metricReason(reason))
		a.writeError(w, err)
		return
	}
	reqInfoFrom(r.Context()).UserID = v.User.ID
	a.audit(r, "auth.login", "success", "username", v.User.Username)
	a.respondAuth(w, r, http.StatusOK, v)
}
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Logout(requestToken(r)); err != nil {
		a.writeError(w, err)
		return
	}
	a.clearSessionCookie(w, r)
	a.audit(r, "auth.logout", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.Me(domain.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleDashboard(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.Dashboard(domain.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListColonies(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListColonies(domain.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleCreateColony(w http.ResponseWriter, r *http.Request) {
	var req service.CreateColonyRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.CreateColony(domain.BearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleTrustPeer(w http.ResponseWriter, r *http.Request) {
	var req service.TrustPeerRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.TrustPeer(domain.BearerToken(r.Header.Get("Authorization")), r.PathValue("id"), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListUsers(domain.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req service.CreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.CreateUser(domain.BearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListAccounts(domain.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleMint(w http.ResponseWriter, r *http.Request) {
	var req service.MintRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.MintOrPropose(domain.BearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	if v.Pending != nil {
		// Dual control: nothing minted yet; a second admin must approve.
		writeJSON(w, http.StatusAccepted, v.Pending)
		return
	}
	writeJSON(w, http.StatusOK, v.Account)
}

func (a *API) token(r *http.Request) string { return domain.BearerToken(r.Header.Get("Authorization")) }

func (a *API) handleCancelTrade(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.CancelTrade(a.token(r), r.PathValue("id"))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleForceRefund(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ProposeForceRefund(a.token(r), r.PathValue("id"))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, v)
}
func (a *API) handleSetPolicy(w http.ResponseWriter, r *http.Request) {
	var req service.PolicyRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.SetPolicy(a.token(r), r.PathValue("id"), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	if v.Pending != nil {
		writeJSON(w, http.StatusAccepted, v.Pending)
		return
	}
	writeJSON(w, http.StatusOK, v.Colony)
}
func (a *API) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListApprovals(a.token(r))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleApprove(w http.ResponseWriter, r *http.Request) { a.decide(w, r, true) }
func (a *API) handleReject(w http.ResponseWriter, r *http.Request)  { a.decide(w, r, false) }
func (a *API) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	v, err := a.service.DecideApproval(a.token(r), r.PathValue("id"), approve)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListTrades(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListTrades(domain.BearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleGetTrade(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.GetTrade(domain.BearerToken(r.Header.Get("Authorization")), r.PathValue("id"))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleCreateOffer(w http.ResponseWriter, r *http.Request) {
	var req service.CreateOfferRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.CreateOffer(domain.BearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleAcceptTrade(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.AcceptTrade(domain.BearerToken(r.Header.Get("Authorization")), r.PathValue("id"))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleExportBundle(w http.ResponseWriter, r *http.Request) {
	var req service.ExportBundleRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.ExportBundle(domain.BearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleImportBundle(w http.ResponseWriter, r *http.Request) {
	var req service.ImportBundleRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, domain.BadRequest(err.Error()))
		return
	}
	v, err := a.service.ImportBundle(domain.BearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListLedger(w http.ResponseWriter, r *http.Request) {
	colonyID := r.URL.Query().Get("colony_id")
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			a.writeError(w, domain.BadRequest("invalid limit"))
			return
		}
		limit = parsed
	}
	var beforeSeq int64
	if raw := r.URL.Query().Get("before_seq"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			a.writeError(w, domain.BadRequest("invalid before_seq"))
			return
		}
		beforeSeq = parsed
	}
	v, err := a.service.ListLedgerPage(domain.BearerToken(r.Header.Get("Authorization")), colonyID, limit, beforeSeq)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
