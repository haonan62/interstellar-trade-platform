package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- logging ----

// setupLogging installs a JSON logger on stdout as the default (so the few
// remaining log.Printf calls and the std library are structured too).
func setupLogging(w io.Writer, level slog.Level) {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h).With("service", "interstellar-trade"))
}

func logLevelFromEnv() slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		return slog.LevelInfo
	}
	return l
}

// ---- request context: request id + W3C trace context ----

type reqInfo struct {
	RequestID string
	TraceID   string // from `traceparent` when valid, else derived from the request id
	SpanID    string // this service's span id
	IP        string
	UserID    string
	Route     string
}

type ctxKey struct{}

func reqInfoFrom(ctx context.Context) *reqInfo {
	if v, ok := ctx.Value(ctxKey{}).(*reqInfo); ok {
		return v
	}
	return &reqInfo{}
}

var (
	traceparentRe = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}$`)
	requestIDRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)
)

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newReqInfo reads an inbound traceparent / X-Request-ID (set by nginx) and
// validates them before they reach the logs (no log injection through headers).
func newReqInfo(r *http.Request) *reqInfo {
	ri := &reqInfo{SpanID: randHex(8), IP: clientIP(r)}
	if m := traceparentRe.FindStringSubmatch(r.Header.Get("Traceparent")); m != nil && m[1] != strings.Repeat("0", 32) {
		ri.TraceID = m[1]
	}
	if id := r.Header.Get("X-Request-Id"); requestIDRe.MatchString(id) {
		ri.RequestID = id
	} else {
		ri.RequestID = randHex(8)
	}
	if ri.TraceID == "" {
		ri.TraceID = randHex(16)
	}
	return ri
}

// clientIP trusts X-Real-IP only when the direct peer is loopback/private,
// i.e. our own reverse proxy; otherwise a client could forge it.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if real := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-Ip"))); real != nil {
			return real.String()
		}
	}
	return host
}

// ---- metrics (Prometheus text exposition, no dependency) ----
//
// client_golang would pull in ~10 modules for four metric types we do not
// need. This registry supports counters, one histogram and gauges, which is
// everything the service exports.

var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type histogram struct {
	counts []uint64
	sum    float64
	n      uint64
}

type Metrics struct {
	mu       sync.Mutex
	counters map[string]float64
	hists    map[string]*histogram
	inflight atomic.Int64
	start    time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{counters: map[string]float64{}, hists: map[string]*histogram{}, start: time.Now()}
}

func labelPairs(kv []string) string {
	if len(kv) == 0 {
		return ""
	}
	parts := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, kv[i], escapeLabel(kv[i+1])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

// Inc adds 1 to a counter series. Label values must come from bounded sets.
func (m *Metrics) Inc(name string, kv ...string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.counters[name+labelPairs(kv)]++
	m.mu.Unlock()
}

func (m *Metrics) observe(method, route string, seconds float64) {
	key := labelPairs([]string{"method", method, "route", route})
	m.mu.Lock()
	h := m.hists[key]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(latencyBuckets))}
		m.hists[key] = h
	}
	for i, b := range latencyBuckets {
		if seconds <= b {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.n++
	m.mu.Unlock()
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m.mu.Lock()
	keys := make([]string, 0, len(m.counters))
	for k := range m.counters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	lastName := ""
	for _, k := range keys {
		name := k
		if i := strings.IndexByte(k, '{'); i >= 0 {
			name = k[:i]
		}
		if name != lastName {
			fmt.Fprintf(&sb, "# TYPE %s counter\n", name)
			lastName = name
		}
		fmt.Fprintf(&sb, "%s %g\n", k, m.counters[k])
	}
	hkeys := make([]string, 0, len(m.hists))
	for k := range m.hists {
		hkeys = append(hkeys, k)
	}
	sort.Strings(hkeys)
	if len(hkeys) > 0 {
		sb.WriteString("# TYPE itp_http_request_duration_seconds histogram\n")
	}
	for _, k := range hkeys {
		h := m.hists[k]
		inner := strings.TrimSuffix(strings.TrimPrefix(k, "{"), "}")
		for i, b := range latencyBuckets {
			fmt.Fprintf(&sb, "itp_http_request_duration_seconds_bucket{%s,le=\"%g\"} %d\n", inner, b, h.counts[i])
		}
		fmt.Fprintf(&sb, "itp_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", inner, h.n)
		fmt.Fprintf(&sb, "itp_http_request_duration_seconds_sum%s %g\n", k, h.sum)
		fmt.Fprintf(&sb, "itp_http_request_duration_seconds_count%s %d\n", k, h.n)
	}
	m.mu.Unlock()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(&sb, "# TYPE itp_http_in_flight_requests gauge\nitp_http_in_flight_requests %d\n", m.inflight.Load())
	fmt.Fprintf(&sb, "# TYPE itp_uptime_seconds gauge\nitp_uptime_seconds %d\n", int(time.Since(m.start).Seconds()))
	fmt.Fprintf(&sb, "# TYPE go_goroutines gauge\ngo_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(&sb, "# TYPE go_memstats_alloc_bytes gauge\ngo_memstats_alloc_bytes %d\n", ms.Alloc)
	fmt.Fprintf(&sb, "# TYPE itp_build_info gauge\nitp_build_info{go_version=\"%s\"} 1\n", runtime.Version())
	_, _ = io.WriteString(w, sb.String())
}

// ---- middleware ----

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func knownMethod(m string) string {
	switch m {
	case "GET", "POST", "HEAD", "OPTIONS":
		return m
	}
	return "OTHER"
}

func isMutating(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// requestToken is the session token of a request, from the Authorization
// header or (for the browser UI) the session cookie.
func requestToken(r *http.Request) string {
	if t := bearerToken(r.Header.Get("Authorization")); t != "" {
		return t
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// withObs is the outermost middleware: request/trace ids, panic recovery,
// access log, metrics and the audit trail for every state-changing request.
func (a *API) withObs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ri := newReqInfo(r)
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, ri))
		w.Header().Set("X-Request-Id", ri.RequestID)
		w.Header().Set("Traceresponse", "00-"+ri.TraceID+"-"+ri.SpanID+"-01")
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		a.metrics.inflight.Add(1)

		mutating := isMutating(r.Method) && strings.HasPrefix(r.URL.Path, "/api/")
		if mutating { // resolve before the handler: logout destroys the session
			ri.UserID = a.service.ActorID(requestToken(r))
		}
		defer func() {
			a.metrics.inflight.Add(-1)
			if rv := recover(); rv != nil {
				slog.Error("panic", "request_id", ri.RequestID, "trace_id", ri.TraceID, "panic", fmt.Sprint(rv), "stack", string(trimStack()))
				if rec.status == 0 {
					writeJSON(rec, http.StatusInternalServerError, &AppError{Code: "internal_error", Message: "internal server error"})
				}
			}
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			route := r.Pattern // set by the mux on this same *Request
			if route == "" {
				route = "unmatched"
			} else if i := strings.IndexByte(route, ' '); i >= 0 {
				route = route[i+1:]
			}
			ri.Route = route
			dur := time.Since(start)
			a.metrics.Inc("itp_http_requests_total", "method", knownMethod(r.Method), "route", route, "code", fmt.Sprint(rec.status))
			a.metrics.observe(knownMethod(r.Method), route, dur.Seconds())
			lvl := slog.LevelInfo
			if route == "/api/v1/healthz" || route == "/api/v1/readyz" {
				lvl = slog.LevelDebug
			}
			slog.Log(r.Context(), lvl, "request", "method", r.Method, "route", route, "status", rec.status, "bytes", rec.bytes,
				"duration_ms", dur.Milliseconds(), "request_id", ri.RequestID, "trace_id", ri.TraceID, "span_id", ri.SpanID, "ip", ri.IP, "user_id", ri.UserID)
			if mutating {
				a.audit(r, "http.mutation", outcomeOf(rec.status), "route", route, "method", r.Method, "status", rec.status)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func trimStack() []byte {
	buf := make([]byte, 4096)
	return buf[:runtime.Stack(buf, false)]
}

func outcomeOf(status int) string {
	if status >= 200 && status < 400 {
		return "success"
	}
	return "failure"
}

// audit records a security-relevant event with who/where/when. It goes to the
// same structured log stream (audit=true) and to a counter. A tamper-evident
// store is out of scope (see docs/phase4-design.md).
func (a *API) audit(r *http.Request, event, outcome string, kv ...any) {
	ri := reqInfoFrom(r.Context())
	attrs := []any{"audit", true, "event", event, "outcome", outcome, "request_id", ri.RequestID, "trace_id", ri.TraceID, "ip", ri.IP, "user_id", ri.UserID}
	slog.Info("audit", append(attrs, kv...)...)
	a.metrics.Inc("itp_audit_events_total", "event", event, "outcome", outcome)
}
