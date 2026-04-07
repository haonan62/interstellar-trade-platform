package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type API struct {
	service        *Service
	allowedOrigins map[string]bool
	staticDir      string
}

func NewAPI(service *Service, allowedOrigins []string, staticDir string) *API {
	origins := map[string]bool{}
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o != "" {
			origins[o] = true
		}
	}
	return &API{service: service, allowedOrigins: origins, staticDir: staticDir}
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/healthz", a.handleHealthz)
	mux.HandleFunc("GET /api/v1/bootstrap", a.handleNeedsBootstrap)
	mux.HandleFunc("POST /api/v1/auth/bootstrap", a.handleBootstrap)
	mux.HandleFunc("POST /api/v1/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", a.handleMe)
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
	mux.HandleFunc("POST /api/v1/relay/export", a.handleExportBundle)
	mux.HandleFunc("POST /api/v1/relay/import", a.handleImportBundle)
	mux.HandleFunc("GET /api/v1/ledger", a.handleListLedger)
	if a.staticDir != "" {
		if st, err := os.Stat(a.staticDir); err == nil && st.IsDir() {
			fs := http.FileServer(http.Dir(a.staticDir))
			mux.Handle("/", spaFileServer(a.staticDir, fs))
		}
	}
	return a.withLogging(a.withCORS(mux))
}
func (a *API) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/healthz" {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (len(a.allowedOrigins) == 0 || a.allowedOrigins[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
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
			fs.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}
func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
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
	var appErr *AppError
	if errors.As(err, &appErr) {
		writeJSON(w, appErr.Status, appErr)
		return
	}
	log.Printf("internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, &AppError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error"})
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
	var req BootstrapRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.Bootstrap(req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.Login(req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Logout(bearerToken(r.Header.Get("Authorization"))); err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.Me(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleDashboard(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.Dashboard(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListColonies(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListColonies(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleCreateColony(w http.ResponseWriter, r *http.Request) {
	var req CreateColonyRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.CreateColony(bearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleTrustPeer(w http.ResponseWriter, r *http.Request) {
	var req TrustPeerRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.TrustPeer(bearerToken(r.Header.Get("Authorization")), r.PathValue("id"), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListUsers(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.CreateUser(bearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListAccounts(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleMint(w http.ResponseWriter, r *http.Request) {
	var req MintRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.Mint(bearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleListTrades(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.ListTrades(bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleGetTrade(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.GetTrade(bearerToken(r.Header.Get("Authorization")), r.PathValue("id"))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleCreateOffer(w http.ResponseWriter, r *http.Request) {
	var req CreateOfferRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.CreateOffer(bearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (a *API) handleAcceptTrade(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.AcceptTrade(bearerToken(r.Header.Get("Authorization")), r.PathValue("id"))
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleExportBundle(w http.ResponseWriter, r *http.Request) {
	var req ExportBundleRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.ExportBundle(bearerToken(r.Header.Get("Authorization")), req)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (a *API) handleImportBundle(w http.ResponseWriter, r *http.Request) {
	var req ImportBundleRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.ImportBundle(bearerToken(r.Header.Get("Authorization")), req)
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
			a.writeError(w, badRequest("invalid limit"))
			return
		}
		limit = parsed
	}
	v, err := a.service.ListLedger(bearerToken(r.Header.Get("Authorization")), colonyID, limit)
	if err != nil {
		a.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
