package provider

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// newBackpressureServer answers /login like the metadata proxy and replies to
// GET requests with the given statuses in order (each with Retry-After) before
// finally returning a TVDB series record. It counts GET attempts.
func newBackpressureServer(t *testing.T, retryAfter string, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"silo-metadata-proxy"}}`))
			return
		}
		n := int(gets.Add(1))
		if n <= len(statuses) {
			w.Header().Set("Retry-After", retryAfter)
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(statuses[n-1])
			_, _ = w.Write([]byte(`{"code":"UPSTREAM_DEFERRED"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":81189,"name":"Breaking Bad"}}`))
	}))
	t.Cleanup(server.Close)
	return server, &gets
}

// meteredLimiter swaps in a limiter that never refills during a test, so the
// tokens it has lost equal the number of HTTP attempts that waited on it.
func meteredLimiter(c *Client) func() int {
	const burst = 100
	c.limiter = rate.NewLimiter(rate.Every(time.Hour), burst)
	return func() int { return burst - int(math.Round(c.limiter.Tokens())) }
}

func TestProxyModeWaitsForRetryAfterThenSucceeds(t *testing.T) {
	t.Parallel()
	server, gets := newBackpressureServer(t, "1", http.StatusServiceUnavailable)
	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}
	limited := meteredLimiter(c)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	var out apiResponse[SeriesExtendedRecord]
	if err := c.doGet(ctx, "/series/81189/extended", &out); err != nil {
		t.Fatalf("doGet: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("retried after %v, want at least the 1s Retry-After", elapsed)
	}
	if out.Data.ID != 81189 {
		t.Fatalf("decoded id = %d", out.Data.ID)
	}
	if got := gets.Load(); got != 2 {
		t.Fatalf("GET attempts = %d, want 2", got)
	}
	if got := limited(); got != 2 {
		t.Fatalf("rate limiter waits = %d, want one per attempt (2)", got)
	}
}

// The proxy answers overload with 503 or 429 plus Retry-After; in proxy mode
// the client must keep waiting past the fixed retry count for as long as the
// caller's deadline allows.
func TestProxyModeHonoursRetryAfterBeyondFixedRetries(t *testing.T) {
	t.Parallel()
	statuses := make([]int, maxRetries+1)
	for i := range statuses {
		statuses[i] = http.StatusServiceUnavailable
		if i%2 == 1 {
			statuses[i] = http.StatusTooManyRequests
		}
	}
	server, gets := newBackpressureServer(t, "1", statuses...)
	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out apiResponse[SeriesExtendedRecord]
	if err := c.doGet(ctx, "/series/81189/extended", &out); err != nil {
		t.Fatalf("expected success after %d attempts, got %v", len(statuses)+1, err)
	}
	if got := int(gets.Load()); got != len(statuses)+1 {
		t.Fatalf("GET attempts = %d, want %d", got, len(statuses)+1)
	}
}

func TestProxyRetryFailsFastWhenRetryAfterPassesDeadline(t *testing.T) {
	t.Parallel()
	server, gets := newBackpressureServer(t, "30", http.StatusServiceUnavailable)
	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	var out apiResponse[SeriesExtendedRecord]
	if err := c.doGet(ctx, "/series/81189/extended", &out); err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("client slept instead of failing fast: %v", elapsed)
	}
	if got := gets.Load(); got != 1 {
		t.Fatalf("GET attempts = %d, want 1", got)
	}
}

// Direct mode keeps the fixed retry cap even when TVDB sends Retry-After.
func TestDirectModeStillCapsRetries(t *testing.T) {
	t.Parallel()
	statuses := make([]int, maxRetries+2)
	for i := range statuses {
		statuses[i] = http.StatusTooManyRequests
	}
	server, gets := newBackpressureServer(t, "1", statuses...)
	c := NewClient(1000)
	c.SetBaseURL(server.URL)
	limited := meteredLimiter(c)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out apiResponse[SeriesExtendedRecord]
	if err := c.doGet(ctx, "/series/81189/extended", &out); err == nil {
		t.Fatal("expected failure")
	}
	if got := int(gets.Load()); got != maxRetries+1 {
		t.Fatalf("GET attempts = %d, want %d", got, maxRetries+1)
	}
	if got := limited(); got != maxRetries+1 {
		t.Fatalf("rate limiter waits = %d, want one per attempt (%d)", got, maxRetries+1)
	}
}

func TestRetryAfterDelay(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		value  string
		want   time.Duration
		wantOK bool
	}{
		{"", 0, false},
		{"1", time.Second, true},
		{" 7 ", 7 * time.Second, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"soon", 0, false},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, false},
		{"99999999999999999", maxProxyRetryAfter, true},
		{now.Add(48 * time.Hour).Format(http.TimeFormat), maxProxyRetryAfter, true},
	}
	for _, tt := range tests {
		got, ok := retryAfterDelay(tt.value, now)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("retryAfterDelay(%q) = (%v, %v), want (%v, %v)", tt.value, got, ok, tt.want, tt.wantOK)
		}
	}
}

// Without a caller deadline, backpressure waits stop at the client's budget
// instead of retrying for as long as the proxy keeps answering 503.
func TestProxyModeBackpressureBudgetWithoutDeadline(t *testing.T) {
	t.Parallel()
	statuses := make([]int, 50)
	for i := range statuses {
		statuses[i] = http.StatusServiceUnavailable
	}
	server, gets := newBackpressureServer(t, "1", statuses...)
	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}
	c.proxyBackpressureBudget = 2500 * time.Millisecond

	start := time.Now()
	var out apiResponse[SeriesExtendedRecord]
	if err := c.doGet(context.Background(), "/series/81189/extended", &out); err == nil {
		t.Fatal("doGet succeeded, want an error once the backpressure budget is spent")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("gave up after %v, want within the 2.5s budget", elapsed)
	}
	if got := gets.Load(); got < 2 || got > 3 {
		t.Fatalf("GET attempts = %d, want 2 or 3 within the budget", got)
	}
}
