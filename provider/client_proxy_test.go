package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeMetadataProxy serves the TVDB v4 surface under /v1/tvdb/4 the way the
// Silo metadata proxy does: it answers /login itself with a fixed token and
// returns raw TVDB JSON for everything else. Like the real proxy it ignores the
// Authorization header, but it records every request and header so tests can
// assert on routing.
type fakeMetadataProxy struct {
	*httptest.Server
	logins atomic.Int32

	mu       sync.Mutex
	requests []string
	auth     []string
}

func (f *fakeMetadataProxy) seen() (requests, auth []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...), append([]string(nil), f.auth...)
}

func newFakeMetadataProxy(t *testing.T) *fakeMetadataProxy {
	t.Helper()
	f := &fakeMetadataProxy{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tvdb/4/login":
			f.logins.Add(1)
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"silo-metadata-proxy"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tvdb/4/series/81189/extended":
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":81189,"name":"Breaking Bad"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tvdb/4/series/81189/episodes/official":
			_, _ = w.Write([]byte(`{"status":"success","data":{"series":{"id":81189},"episodes":[{"id":349232,"seasonNumber":1,"number":1}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tvdb/4/series/999999999/extended":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":"failure","message":"NotFoundException: series not found","data":null}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func TestProxyModeRoutesLoginAndRequestsThroughProxyPrefix(t *testing.T) {
	server := newFakeMetadataProxy(t)

	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL + "/"); err != nil {
		t.Fatalf("SetProxyURL: %v", err)
	}
	if !c.ProxyMode() {
		t.Fatal("expected proxy mode")
	}
	series, err := c.GetSeriesExtended(context.Background(), 81189)
	if err != nil {
		t.Fatalf("GetSeriesExtended: %v", err)
	}
	if series.ID != 81189 || series.Name != "Breaking Bad" {
		t.Fatalf("decoded series = %d %q", series.ID, series.Name)
	}
	want := []string{
		"POST /v1/tvdb/4/login",
		"GET /v1/tvdb/4/series/81189/extended?meta=translations",
	}
	requests, auth := server.seen()
	if strings.Join(requests, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests = %q, want %q", requests, want)
	}
	if auth[1] != "Bearer silo-metadata-proxy" {
		t.Fatalf("Authorization = %q, want the token the proxy's /login returned", auth[1])
	}
	if got := server.logins.Load(); got != 1 {
		t.Fatalf("logins = %d, want 1", got)
	}
}

func TestProxyModePassesThroughTVDBNotFound(t *testing.T) {
	server := newFakeMetadataProxy(t)

	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}
	_, err := c.GetSeriesExtended(context.Background(), 999999999)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404: NotFoundException") {
		t.Fatalf("err = %v, want TVDB's 404 message", err)
	}
}

func TestProxyModeCanBeDisabledAgain(t *testing.T) {
	server := newFakeMetadataProxy(t)

	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.GetSeriesExtended(ctx, 81189); err != nil {
		t.Fatalf("GetSeriesExtended: %v", err)
	}
	if _, err := c.getAllSeriesEpisodes(ctx, 81189, officialSeasonType, ""); err != nil {
		t.Fatalf("getAllSeriesEpisodes: %v", err)
	}
	if c.getToken() == "" || len(c.seriesExtendedCache) == 0 || len(c.episodesCache) == 0 {
		t.Fatalf("setup did not populate the session: token=%q series=%d episodes=%d",
			c.getToken(), len(c.seriesExtendedCache), len(c.episodesCache))
	}

	if err := c.SetProxyURL(""); err != nil {
		t.Fatal(err)
	}
	if base, proxy := c.transport(); proxy || base != defaultBaseURL {
		t.Fatalf("direct mode not restored: proxy=%v base=%q", proxy, base)
	}
	if c.getToken() != "" {
		t.Fatalf("token = %q, want it cleared when leaving proxy mode", c.getToken())
	}
	if len(c.seriesExtendedCache) != 0 || len(c.episodesCache) != 0 {
		t.Fatalf("caches not cleared: series=%d episodes=%d", len(c.seriesExtendedCache), len(c.episodesCache))
	}
}

func TestSetProxyURLKeepsSessionWhenUnchanged(t *testing.T) {
	server := newFakeMetadataProxy(t)

	c := NewClient(1000)
	if err := c.SetProxyURL(server.URL); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.GetSeriesExtended(ctx, 81189); err != nil {
		t.Fatalf("GetSeriesExtended: %v", err)
	}
	// Configure may run again with an unchanged setting.
	if err := c.SetProxyURL(server.URL + "/"); err != nil {
		t.Fatal(err)
	}
	if c.getToken() == "" || len(c.seriesExtendedCache) == 0 {
		t.Fatal("re-applying the same proxy URL dropped the session")
	}
	if _, err := c.GetSeriesExtended(ctx, 81189); err != nil {
		t.Fatalf("GetSeriesExtended: %v", err)
	}
	if got := server.logins.Load(); got != 1 {
		t.Fatalf("logins = %d, want 1", got)
	}
}

func TestSetProxyURLRejectsGarbage(t *testing.T) {
	c := NewClient(1000)
	for _, bad := range []string{"metadata.siloserver.org", "ftp://x", "https://", "https://h/?x=1", "https://h/#f"} {
		if err := c.SetProxyURL(bad); err == nil {
			t.Errorf("SetProxyURL(%q) accepted", bad)
		}
	}
	if base, proxy := c.transport(); proxy || base != defaultBaseURL {
		t.Fatalf("rejected URL changed the transport: proxy=%v base=%q", proxy, base)
	}
}

// Configure can switch the transport while a library scan is running. Run with
// -race to check that requests and the switch do not share unguarded state.
func TestSetProxyURLIsSafeDuringRequests(t *testing.T) {
	first := newFakeMetadataProxy(t)
	second := newFakeMetadataProxy(t)

	c := NewClient(1000)
	if err := c.SetProxyURL(first.URL); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				// doGet bypasses the memo cache so every call reads the transport.
				var resp apiResponse[SeriesExtendedRecord]
				if err := c.doGet(ctx, "/series/81189/extended?meta=translations", &resp); err != nil {
					t.Errorf("doGet: %v", err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	for i := 0; ; i++ {
		select {
		case <-done:
			return
		default:
		}
		target := first.URL
		if i%2 == 0 {
			target = second.URL
		}
		if err := c.SetProxyURL(target); err != nil {
			t.Fatal(err)
		}
	}
}

// Saving proxy settings while a request is in flight clears the token. The
// retry must log in to and call the new upstream, not keep calling the old one.
func TestTransportSwitchDuringRequestRetriesAgainstNewUpstream(t *testing.T) {
	t.Parallel()
	c := NewClient(1000)
	var newHits atomic.Int32
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == proxyPathPrefix+"/login" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"next-token"}}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer next-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		newHits.Add(1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":81189,"name":"Breaking Bad"}}`))
	}))
	t.Cleanup(next.Close)
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"old-token"}}`))
			return
		}
		// The operator saves new proxy settings while this request is in flight.
		if err := c.SetProxyURL(next.URL); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(old.Close)
	if err := c.SetProxyURL(old.URL); err != nil {
		t.Fatal(err)
	}

	var out apiResponse[SeriesExtendedRecord]
	if err := c.doGet(context.Background(), "/series/81189/extended", &out); err != nil {
		t.Fatalf("doGet: %v", err)
	}
	if out.Data.ID != 81189 || newHits.Load() != 1 {
		t.Fatalf("decoded id %d with %d hits on the new upstream, want 81189 and 1", out.Data.ID, newHits.Load())
	}
}
