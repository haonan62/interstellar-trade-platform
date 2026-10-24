package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/httpapi"
	"interstellar-trade-platform/backend/internal/service"
	"interstellar-trade-platform/backend/internal/storage"
)

func splitCSV(input string) []string {
	parts := strings.Split(input, ",")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// runHealthcheck is the container health probe. The runtime image is FROM
// scratch (no shell, no wget/curl), so the binary probes itself.
func runHealthcheck(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/v1/readyz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

// version is stamped at build time: -ldflags "-X main.version=<git describe>".
var version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataPath := flag.String("data", "./data/state.json", "path to state file")
	corsOrigins := flag.String("cors", os.Getenv("CORS_ORIGINS"), "comma-separated allowed CORS origins (empty = none; the UI is same-origin)")
	staticDir := flag.String("static", "", "optional path to built frontend assets")
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URL; when set the JSON state file is not used")
	migrateFrom := flag.String("migrate-state-from", "", "import this state.json into the (empty) PostgreSQL database, then exit")
	bootstrapToken := flag.String("bootstrap-token", os.Getenv("BOOTSTRAP_TOKEN"), "one-time secret required by POST /auth/bootstrap (generated and logged once when unset and the system is not bootstrapped)")
	requireAdminMFA := flag.Bool("require-admin-mfa", envOr("REQUIRE_ADMIN_MFA", "") == "true", "administrators must enrol TOTP MFA before they can use the API")
	metricsAddr := flag.String("metrics-addr", os.Getenv("METRICS_ADDR"), "listen address for /metrics (empty = disabled); keep it off the public network")
	healthcheck := flag.Bool("healthcheck", false, "probe http://127.0.0.1:<port>/api/v1/readyz and exit 0/1 (container HEALTHCHECK)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *healthcheck {
		os.Exit(runHealthcheck(*addr))
	}
	httpapi.SetupLogging(os.Stdout, httpapi.LogLevelFromEnv())
	domain.AdminMFARequired.Store(*requireAdminMFA)

	// SIGTERM/SIGINT => stop accepting, drain in-flight requests, then exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var store *storage.Store
	var err error
	if *databaseURL != "" {
		store, err = storage.NewPGStore(ctx, *databaseURL, "")
		if err != nil {
			fatal("open postgres", "error", err.Error())
		}
		defer func() { _ = store.Close() }()
		if *migrateFrom != "" {
			st, err := storage.LoadStateFile(*migrateFrom)
			if err != nil {
				fatal("read state file", "path", *migrateFrom, "error", err.Error())
			}
			if err := store.ImportState(ctx, st); err != nil {
				fatal("migrate", "error", err.Error())
			}
			slog.Info("migrated state", "colonies", len(st.Colonies), "users", len(st.Users), "trades", len(st.Trades), "from", *migrateFrom)
			return
		}
	} else {
		if *migrateFrom != "" {
			fatal("--migrate-state-from requires --database-url")
		}
		store, err = storage.NewStore(*dataPath)
		if err != nil {
			fatal("load store", "error", err.Error())
		}
	}
	svc := service.NewService(store)

	// Bootstrap gate. Without a token nobody could bootstrap, so when the
	// system is still empty and none was configured, make one and say so once.
	token := *bootstrapToken
	if needs, err := svc.NeedsBootstrap(); err == nil && needs && token == "" {
		if token, err = domain.RandomBase64URL(24); err != nil {
			fatal("generate bootstrap token", "error", err.Error())
		}
		slog.Warn("system is not bootstrapped: use this one-time token in the X-Bootstrap-Token header (or the UI field); set BOOTSTRAP_TOKEN to choose your own", "bootstrap_token", token)
	}

	api := httpapi.NewAPI(svc, splitCSV(*corsOrigins), *staticDir).WithBootstrapToken(token)
	go func() { // close unaccepted offers past their expiry
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := svc.SweepExpired(); err != nil {
					slog.Error("sweep expired offers", "error", err.Error())
				} else if n > 0 {
					api.Metrics().Inc("itp_offers_expired_total")
					slog.Info("expired offers", "count", n)
				}
			}
		}
	}()

	if *metricsAddr != "" {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("GET /metrics", api.MetricsHandler())
			ms := &http.Server{Addr: *metricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			go func() { <-ctx.Done(); _ = ms.Close() }()
			if err := ms.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server", "error", err.Error())
			}
		}()
	}

	srv := httpapi.NewHTTPServer(*addr, api.Handler())
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal("listen", "error", err.Error())
	}
	slog.Info("listening", "version", version, "addr", ln.Addr().String(), "admin_mfa_required", *requireAdminMFA, "cors_origins", len(splitCSV(*corsOrigins)), "metrics_addr", *metricsAddr)
	if err := httpapi.ServeUntil(ctx, srv, ln, httpapi.ShutdownTimeout); err != nil {
		slog.Error("server stopped with error", "error", err.Error())
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
