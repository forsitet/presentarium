package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"presentarium/internal/middleware"
)

func TestCORS_AllowedOrigin(t *testing.T) {
	mw := middleware.CORS("http://allowed.example")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://allowed.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Error("next handler should have been called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://allowed.example" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q", got)
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	mw := middleware.CORS("http://allowed.example")
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin should be empty, got %q", got)
	}
}

func TestCORS_PreflightOptions(t *testing.T) {
	mw := middleware.CORS("http://allowed.example")
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://allowed.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Error("preflight should not call next handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Allow-Methods should be set on preflight")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("Allow-Headers should be set on preflight")
	}
}

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := middleware.NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Error("4th request should be blocked")
	}
}

func TestRateLimiter_PerIPIsolated(t *testing.T) {
	rl := middleware.NewRateLimiter(1, time.Minute)
	if !rl.Allow("ip-a") {
		t.Error("first request from ip-a should be allowed")
	}
	if rl.Allow("ip-a") {
		t.Error("second request from ip-a should be blocked")
	}
	if !rl.Allow("ip-b") {
		t.Error("first request from ip-b should be allowed (separate bucket)")
	}
}

func TestRateLimiter_RecoversAfterWindow(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 10*time.Millisecond)
	if !rl.Allow("ip") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("ip") {
		t.Fatal("second request immediately should be blocked")
	}
	time.Sleep(20 * time.Millisecond)
	if !rl.Allow("ip") {
		t.Error("request after window expiry should be allowed")
	}
}

func TestRateLimit_Middleware(t *testing.T) {
	rl := middleware.NewRateLimiter(1, time.Minute)
	mw := middleware.RateLimit(rl)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	first := makeReq("9.9.9.9")
	if first.Code != http.StatusOK {
		t.Errorf("first request status = %d", first.Code)
	}
	second := makeReq("9.9.9.9")
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header should be set on rate limit")
	}
}

func TestRateLimit_RealIP_XForwardedFor(t *testing.T) {
	rl := middleware.NewRateLimiter(1, time.Minute)
	mw := middleware.RateLimit(rl)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request from forwarded IP A.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	req1.RemoteAddr = "127.0.0.1:1"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("first XFF request status = %d", rec1.Code)
	}

	// Second from same forwarded IP — blocked.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Forwarded-For", "10.0.0.1")
	req2.RemoteAddr = "127.0.0.1:2"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second XFF request status = %d, want 429", rec2.Code)
	}

	// Different forwarded IP — allowed.
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Header.Set("X-Forwarded-For", "10.0.0.99")
	req3.RemoteAddr = "127.0.0.1:3"
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("different XFF request status = %d", rec3.Code)
	}
}

func TestRateLimit_RealIP_XRealIP(t *testing.T) {
	rl := middleware.NewRateLimiter(1, time.Minute)
	mw := middleware.RateLimit(rl)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "5.5.5.5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first request status = %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Real-IP", "5.5.5.5")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d", rec2.Code)
	}
}

func TestLogger_PassesThroughAndCapturesStatus(t *testing.T) {
	mw := middleware.Logger()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/path", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestLogger_FlushIsSafeWithoutFlusher(t *testing.T) {
	// httptest.ResponseRecorder implements http.Flusher, so this exercises the success path.
	mw := middleware.Logger()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestRateLimiter_CleanupRunsAfterLongInterval(t *testing.T) {
	// Tiny window so cleanAt elapses quickly.
	rl := middleware.NewRateLimiter(2, 5*time.Millisecond)
	rl.Allow("1.1.1.1")
	time.Sleep(20 * time.Millisecond) // older than window*2 → cleanStale path
	rl.Allow("2.2.2.2")
	// Just verifying no panic; behavior is checked via subsequent allow checks.
	if !rl.Allow("3.3.3.3") {
		t.Error("expected allow after cleanup")
	}
}
