package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Silo-Server/silo-plugin-tvdb/metadata"
)

func TestProviderSearchByTitleIncludesRemoteIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"token": "test-token",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/search":
			if r.URL.Query().Get("query") != "10 Tokyo Warriors" {
				t.Fatalf("query = %q, want 10 Tokyo Warriors", r.URL.Query().Get("query"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": []map[string]any{{
					"name":             "倒凶十将伝",
					"aliases":          []string{"10 Tokyo Warriors"},
					"primary_language": "jpn",
					"year":             "1999",
					"tvdb_id":          "420105",
					"overview":         "Ten warriors defend Tokyo.",
					"remote_ids": []map[string]any{
						{"type": 12, "id": "201992", "sourceName": "TheMovieDB.com"},
						{"type": 2, "id": "tt18076310", "sourceName": "IMDB"},
					},
				}},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	results, err := p.Search(context.Background(), metadata.SearchQuery{
		Title:       "10 Tokyo Warriors",
		ContentType: "series",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	ids := results[0].ProviderIDs
	if ids["tvdb"] != "420105" || ids["tmdb"] != "201992" || ids["imdb"] != "tt18076310" {
		t.Fatalf("provider ids = %+v, want tvdb/tmdb/imdb", ids)
	}
	if results[0].Name != "倒凶十将伝" || results[0].OriginalLanguage != "ja" || !results[0].TitleIsFallback {
		t.Fatalf("title metadata = (%q, %q, %v)", results[0].Name, results[0].OriginalLanguage, results[0].TitleIsFallback)
	}
	if len(results[0].TitleAliases) != 1 || results[0].TitleAliases[0].Title != "10 Tokyo Warriors" {
		t.Fatalf("aliases = %#v, want English search alias", results[0].TitleAliases)
	}
}

func TestGetSeriesMetadataIncludesSourceNameRemoteIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/100/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":       100,
					"name":     "Series",
					"overview": "Series overview",
					"remoteIds": []map[string]any{
						{"type": 0, "id": "201992", "sourceName": "TheMovieDB.com"},
						{"type": 0, "id": "tt18076310", "sourceName": "IMDb"},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tvdb": "100"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if result.ProviderIDs["tvdb"] != "100" || result.ProviderIDs["tmdb"] != "201992" || result.ProviderIDs["imdb"] != "tt18076310" {
		t.Fatalf("provider ids = %+v, want tvdb/tmdb/imdb", result.ProviderIDs)
	}
}

func TestFillRemoteIDsUsesTypeAndSourceNameWithoutOverwrite(t *testing.T) {
	t.Parallel()

	ids := map[string]string{
		"imdb": "nm-existing",
		"tmdb": "existing-tmdb",
	}
	fillRemoteIDs(ids, []RemoteID{
		{Type: 0, ID: "tt-source", SourceName: "IMDb"},
		{Type: 0, ID: "source-tmdb", SourceName: "The Movie Database"},
		{Type: 2, ID: "tt-type", SourceName: ""},
		{Type: 12, ID: "type-tmdb", SourceName: ""},
	})

	if ids["imdb"] != "nm-existing" {
		t.Fatalf("imdb overwritten: got %q", ids["imdb"])
	}
	if ids["tmdb"] != "existing-tmdb" {
		t.Fatalf("tmdb overwritten: got %q", ids["tmdb"])
	}

	ids = map[string]string{}
	fillRemoteIDs(ids, []RemoteID{
		{Type: 0, ID: "30773-the-yogi-bear-show", SourceName: "TheMovieDB.com"},
		{Type: 0, ID: "not-an-imdb-id", SourceName: "imdb.com"},
		{Type: 0, ID: "tt123", SourceName: "imdb.com"},
		{Type: 0, ID: "nm1234567", SourceName: "imdb.com"},
		{Type: 0, ID: "201992", SourceName: "TheMovieDB.com"},
		{Type: 0, ID: "TT18076310", SourceName: "imdb.com"},
	})

	if ids["tmdb"] != "201992" || ids["imdb"] != "tt18076310" {
		t.Fatalf("provider ids = %+v, want source-name tmdb/imdb", ids)
	}
}

func TestGetImagesReturnsArtworkImageURLs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"token": "test-token",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":   99,
					"name": "Series",
					"artworks": []map[string]any{
						{
							"id":           1,
							"type":         2,
							"image":        "https://artworks.example/poster-original.jpg",
							"thumbnail":    "https://artworks.example/poster-thumb.jpg",
							"width":        2000,
							"height":       3000,
							"score":        10,
							"includesText": true,
						},
						{
							"id":           2,
							"type":         3,
							"image":        "https://artworks.example/background-original.jpg",
							"thumbnail":    "",
							"width":        3840,
							"height":       2160,
							"score":        8,
							"includesText": false,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tvdb": "99"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(images))
	}

	got := map[metadata.ImageType]metadata.RemoteImage{}
	for _, img := range images {
		got[img.Type] = img
	}

	if got[metadata.ImagePoster].URL != "https://artworks.example/poster-original.jpg" {
		t.Fatalf("poster URL = %q", got[metadata.ImagePoster].URL)
	}
	if got[metadata.ImagePoster].IncludesText == nil || !*got[metadata.ImagePoster].IncludesText {
		t.Fatalf("poster IncludesText = %v, want true", got[metadata.ImagePoster].IncludesText)
	}
	if got[metadata.ImageBackdrop].URL != "https://artworks.example/background-original.jpg" {
		t.Fatalf("backdrop URL = %q", got[metadata.ImageBackdrop].URL)
	}
	if got[metadata.ImageBackdrop].IncludesText == nil || *got[metadata.ImageBackdrop].IncludesText {
		t.Fatalf("backdrop IncludesText = %v, want false", got[metadata.ImageBackdrop].IncludesText)
	}
}

func TestGetImagesPrefersTVDBPrimaryPoster(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"token": "test-token",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":    99,
					"name":  "Series",
					"image": "https://artworks.example/poster-primary.jpg",
					"artworks": []map[string]any{
						{
							"id":           1,
							"type":         2,
							"image":        "https://artworks.example/poster-primary.jpg",
							"language":     "eng",
							"width":        2000,
							"height":       3000,
							"score":        10,
							"includesText": true,
						},
						{
							"id":           2,
							"type":         2,
							"image":        "https://artworks.example/poster-textless.jpg",
							"language":     "",
							"width":        2000,
							"height":       3000,
							"score":        11,
							"includesText": false,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tvdb": "99"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}

	var primary, textless *metadata.RemoteImage
	for i := range images {
		switch images[i].URL {
		case "https://artworks.example/poster-primary.jpg":
			primary = &images[i]
		case "https://artworks.example/poster-textless.jpg":
			textless = &images[i]
		}
	}

	if primary == nil {
		t.Fatal("primary poster missing from GetImages() result")
	}
	if textless == nil {
		t.Fatal("alternate poster missing from GetImages() result")
	}
	if primary.Language != "en" {
		t.Fatalf("primary language = %q, want en", primary.Language)
	}
	if primary.IncludesText == nil || !*primary.IncludesText {
		t.Fatalf("primary IncludesText = %v, want true", primary.IncludesText)
	}
	if textless.IncludesText == nil || *textless.IncludesText {
		t.Fatalf("textless IncludesText = %v, want false", textless.IncludesText)
	}
	if primary.Rating <= textless.Rating {
		t.Fatalf("primary rating = %v, textless rating = %v; want primary > textless", primary.Rating, textless.Rating)
	}
}

func TestGetImagesAddsPrimaryPosterWhenArtworkListMissesIt(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"token": "test-token",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":    99,
					"name":  "Series",
					"image": "https://artworks.example/poster-primary.jpg",
					"artworks": []map[string]any{
						{
							"id":       2,
							"type":     2,
							"image":    "https://artworks.example/poster-alt.jpg",
							"language": "",
							"width":    2000,
							"height":   3000,
							"score":    11,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tvdb": "99"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}

	var primary, alt *metadata.RemoteImage
	for i := range images {
		switch images[i].URL {
		case "https://artworks.example/poster-primary.jpg":
			primary = &images[i]
		case "https://artworks.example/poster-alt.jpg":
			alt = &images[i]
		}
	}

	if primary == nil {
		t.Fatal("primary poster was not appended to GetImages() result")
	}
	if alt == nil {
		t.Fatal("alternate poster missing from GetImages() result")
	}
	if primary.Rating <= alt.Rating {
		t.Fatalf("primary rating = %v, alt rating = %v; want primary > alt", primary.Rating, alt.Rating)
	}
}

// ---------------------------------------------------------------------------
// Translation tests
// ---------------------------------------------------------------------------

// newTranslationTestServer creates a test server that serves a Japanese series
// with embedded translations and per-entity translation endpoints.
func newTranslationTestServer(t *testing.T, translationCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"token": "test-token"},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/series/100/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":               100,
					"name":             "Original Japanese Title",
					"overview":         "Original Japanese overview",
					"originalLanguage": "jpn",
					"image":            "https://example.com/poster.jpg",
					"translations": map[string]any{
						"nameTranslations": []map[string]any{
							{"language": "jpn", "name": "Original Japanese Title"},
							{"language": "eng", "name": "English Series Title"},
						},
						"overviewTranslations": []map[string]any{
							{"language": "jpn", "overview": "Original Japanese overview"},
							{"language": "eng", "overview": "English series overview"},
						},
					},
					"seasons": []map[string]any{
						{"id": 200, "seriesId": 100, "number": 1, "type": map[string]any{"id": 1, "name": "Aired Order"}},
					},
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/series/100/translations/eng":
			if translationCalls != nil {
				translationCalls.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"name":     "English Series Title",
					"overview": "English series overview",
					"language": "eng",
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/series/100/episodes/official":
			// Bulk base (original-language) episode list for the whole series.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "jpn"},
					"episodes": []map[string]any{
						{"id": 301, "name": "Japanese Ep 1", "overview": "JP overview 1", "number": 1, "seasonNumber": 1},
						{"id": 302, "name": "Japanese Ep 2", "overview": "JP overview 2", "number": 2, "seasonNumber": 1},
						{"id": 303, "name": "Japanese Ep 3", "overview": "JP overview 3", "number": 3, "seasonNumber": 1},
					},
				},
				"links": map[string]any{"next": nil},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/series/100/episodes/official/eng":
			// Bulk translated episode list — one call for the whole series.
			if translationCalls != nil {
				translationCalls.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "jpn"},
					"episodes": []map[string]any{
						{"id": 301, "name": "English Ep 1", "overview": "EN overview 1", "number": 1, "seasonNumber": 1},
						{"id": 302, "name": "English Ep 2", "overview": "EN overview 2", "number": 2, "seasonNumber": 1},
						{"id": 303, "name": "English Ep 3", "overview": "EN overview 3", "number": 3, "seasonNumber": 1},
					},
				},
				"links": map[string]any{"next": nil},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/seasons/200/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":       200,
					"seriesId": 100,
					"number":   1,
					"type":     map[string]any{"id": 1, "name": "Aired Order"},
					"episodes": []map[string]any{
						{"id": 301, "name": "Japanese Ep 1", "overview": "JP overview 1", "number": 1, "seasonNumber": 1},
						{"id": 302, "name": "Japanese Ep 2", "overview": "JP overview 2", "number": 2, "seasonNumber": 1},
						{"id": 303, "name": "Japanese Ep 3", "overview": "JP overview 3", "number": 3, "seasonNumber": 1},
					},
				},
			})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/episodes/") && strings.HasSuffix(r.URL.Path, "/translations/eng"):
			if translationCalls != nil {
				translationCalls.Add(1)
			}
			// Extract episode ID from path.
			parts := strings.Split(r.URL.Path, "/")
			epID := parts[2]
			names := map[string]string{"301": "English Ep 1", "302": "English Ep 2", "303": "English Ep 3"}
			overviews := map[string]string{"301": "EN overview 1", "302": "EN overview 2", "303": "EN overview 3"}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"name":     names[epID],
					"overview": overviews[epID],
					"language": "eng",
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/seasons/200/translations/eng":
			if translationCalls != nil {
				translationCalls.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"name":     "Season 1",
					"overview": "English season overview",
					"language": "eng",
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
}

func TestGetSeriesMetadata_TranslatesNonNativeLanguage(t *testing.T) {
	t.Parallel()

	server := newTranslationTestServer(t, nil)
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tvdb": "100"},
		ContentType: "series",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if result.Title != "English Series Title" {
		t.Fatalf("Title = %q, want %q", result.Title, "English Series Title")
	}
	if result.Overview != "English series overview" {
		t.Fatalf("Overview = %q, want %q", result.Overview, "English series overview")
	}
	if result.TitleLanguage != "en" || result.TitleIsFallback || result.OriginalLanguage != "ja" || result.OriginalTitle != "Original Japanese Title" {
		t.Fatalf("title language metadata = title:%q fallback:%v original_language:%q original_title:%q", result.TitleLanguage, result.TitleIsFallback, result.OriginalLanguage, result.OriginalTitle)
	}
	if !result.TitleAliasesComplete {
		t.Fatal("full TVDB extended response must mark title aliases complete")
	}
	foundOriginal, foundEnglish := false, false
	for _, alias := range result.TitleAliases {
		foundOriginal = foundOriginal || alias.Title == "Original Japanese Title" && alias.Language == "ja" && alias.Kind == "original"
		foundEnglish = foundEnglish || alias.Title == "English Series Title" && alias.Language == "en" && alias.Kind == "localized"
	}
	if !foundOriginal || !foundEnglish {
		t.Fatalf("title aliases = %#v, want Japanese original and English localized aliases", result.TitleAliases)
	}
}

func TestGetSeriesMetadata_SkipsTranslationWhenLanguageMatchesOriginal(t *testing.T) {
	t.Parallel()

	var translationCalls atomic.Int32
	server := newTranslationTestServer(t, &translationCalls)
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tvdb": "100"},
		ContentType: "series",
		Language:    "ja", // matches originalLanguage "jpn"
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	// Should use original data without fetching translations.
	if result.Title != "Original Japanese Title" {
		t.Fatalf("Title = %q, want %q", result.Title, "Original Japanese Title")
	}
	if result.TitleLanguage != "ja" || result.TitleIsFallback || result.OriginalLanguage != "ja" {
		t.Fatalf("native title metadata = (%q, %v, %q)", result.TitleLanguage, result.TitleIsFallback, result.OriginalLanguage)
	}
	if translationCalls.Load() != 0 {
		t.Fatalf("translation endpoint called %d times, want 0", translationCalls.Load())
	}
}

func TestGetSeriesMetadata_UsesEmbeddedTranslationsWithoutDedicatedEndpoint(t *testing.T) {
	t.Parallel()

	var translationCalls atomic.Int32
	server := newTranslationTestServer(t, &translationCalls)
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tvdb": "100"},
		ContentType: "series",
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	// Should get English data from embedded translations.
	if result.Title != "English Series Title" {
		t.Fatalf("Title = %q, want %q", result.Title, "English Series Title")
	}
	if result.Overview != "English series overview" {
		t.Fatalf("Overview = %q, want %q", result.Overview, "English series overview")
	}
	// Should NOT call the dedicated translation endpoint since embedded data was sufficient.
	if translationCalls.Load() != 0 {
		t.Fatalf("dedicated translation endpoint called %d times, want 0 (embedded was sufficient)", translationCalls.Load())
	}
}

func TestGetEpisodes_TranslatesViaBulkEndpoint(t *testing.T) {
	t.Parallel()

	var translationCalls atomic.Int32
	server := newTranslationTestServer(t, &translationCalls)
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"tvdb": "100"},
		SeasonNumber: 1,
		Language:     "en",
	})
	if err != nil {
		t.Fatalf("GetEpisodes() error = %v", err)
	}
	if len(episodes) != 3 {
		t.Fatalf("len(episodes) = %d, want 3", len(episodes))
	}

	// Verify all episodes were translated.
	for i, ep := range episodes {
		wantTitle := []string{"English Ep 1", "English Ep 2", "English Ep 3"}[i]
		wantOverview := []string{"EN overview 1", "EN overview 2", "EN overview 3"}[i]
		if ep.Title != wantTitle {
			t.Errorf("episodes[%d].Title = %q, want %q", i, ep.Title, wantTitle)
		}
		if ep.Overview != wantOverview {
			t.Errorf("episodes[%d].Overview = %q, want %q", i, ep.Overview, wantOverview)
		}
	}

	// The bulk translated endpoint must be hit exactly once for the whole
	// season — not once per episode (the old N+1).
	if got := translationCalls.Load(); got != 1 {
		t.Fatalf("bulk translation calls = %d, want 1 (no per-episode N+1)", got)
	}
}

func TestGetEpisodes_SkipsTranslationWhenLanguageMatchesOriginal(t *testing.T) {
	t.Parallel()

	var translationCalls atomic.Int32
	server := newTranslationTestServer(t, &translationCalls)
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"tvdb": "100"},
		SeasonNumber: 1,
		Language:     "ja", // matches originalLanguage "jpn"
	})
	if err != nil {
		t.Fatalf("GetEpisodes() error = %v", err)
	}
	if len(episodes) != 3 {
		t.Fatalf("len(episodes) = %d, want 3", len(episodes))
	}
	// Should use original data.
	if episodes[0].Title != "Japanese Ep 1" {
		t.Fatalf("episodes[0].Title = %q, want %q", episodes[0].Title, "Japanese Ep 1")
	}
	if translationCalls.Load() != 0 {
		t.Fatalf("translation endpoint called %d times, want 0", translationCalls.Load())
	}
}

func TestGetEpisodes_PartialTranslationFailureKeepsOriginalData(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"token": "test-token"},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/series/100/episodes/official":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "jpn"},
					"episodes": []map[string]any{
						{"id": 301, "name": "JP Ep 1", "overview": "JP ov 1", "number": 1, "seasonNumber": 1},
						{"id": 302, "name": "JP Ep 2", "overview": "JP ov 2", "number": 2, "seasonNumber": 1},
					},
				},
				"links": map[string]any{"next": nil},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/series/100/episodes/official/eng":
			// Translated bulk list: ep 301 is translated; ep 302 has no
			// translation (empty name/overview), so its original must be kept.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "jpn"},
					"episodes": []map[string]any{
						{"id": 301, "name": "English Ep 1", "overview": "EN ov 1", "number": 1, "seasonNumber": 1},
						{"id": 302, "name": "", "overview": "", "number": 2, "seasonNumber": 1},
					},
				},
				"links": map[string]any{"next": nil},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"tvdb": "100"},
		SeasonNumber: 1,
		Language:     "en",
	})
	if err != nil {
		t.Fatalf("GetEpisodes() error = %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("len(episodes) = %d, want 2", len(episodes))
	}

	// Episode 1 should be translated.
	if episodes[0].Title != "English Ep 1" {
		t.Errorf("episodes[0].Title = %q, want %q", episodes[0].Title, "English Ep 1")
	}
	// Episode 2 should keep original data after translation failure.
	if episodes[1].Title != "JP Ep 2" {
		t.Errorf("episodes[1].Title = %q, want %q (original kept after failure)", episodes[1].Title, "JP Ep 2")
	}
	if episodes[1].Overview != "JP ov 2" {
		t.Errorf("episodes[1].Overview = %q, want %q (original kept after failure)", episodes[1].Overview, "JP ov 2")
	}
}

// TestGetEpisodes_CachesAcrossSeasons verifies the bulk endpoint is fetched once
// for a multi-season series even when the server requests each season
// separately — the cache prevents a full-series re-fetch per season.
func TestGetEpisodes_CachesAcrossSeasons(t *testing.T) {
	t.Parallel()

	var baseCalls, transCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "data": map[string]any{"token": "test-token"},
			})

		case r.URL.Path == "/series/100/episodes/official":
			baseCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "jpn"},
					"episodes": []map[string]any{
						{"id": 301, "name": "JP S1E1", "number": 1, "seasonNumber": 1},
						{"id": 401, "name": "JP S2E1", "number": 1, "seasonNumber": 2},
					},
				},
				"links": map[string]any{"next": nil},
			})

		case r.URL.Path == "/series/100/episodes/official/eng":
			transCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "jpn"},
					"episodes": []map[string]any{
						{"id": 301, "name": "EN S1E1", "number": 1, "seasonNumber": 1},
						{"id": 401, "name": "EN S2E1", "number": 1, "seasonNumber": 2},
					},
				},
				"links": map[string]any{"next": nil},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	for _, season := range []int{1, 2} {
		eps, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
			ProviderIDs:  map[string]string{"tvdb": "100"},
			SeasonNumber: season,
			Language:     "en",
		})
		if err != nil {
			t.Fatalf("GetEpisodes(season=%d) error = %v", season, err)
		}
		if len(eps) != 1 {
			t.Fatalf("season %d: len(episodes) = %d, want 1", season, len(eps))
		}
		wantTitle := map[int]string{1: "EN S1E1", 2: "EN S2E1"}[season]
		if eps[0].Title != wantTitle {
			t.Errorf("season %d: Title = %q, want %q", season, eps[0].Title, wantTitle)
		}
	}

	// Despite two GetEpisodes calls, each bulk endpoint is fetched exactly once.
	if got := baseCalls.Load(); got != 1 {
		t.Errorf("base bulk calls = %d, want 1 (cache should serve season 2)", got)
	}
	if got := transCalls.Load(); got != 1 {
		t.Errorf("translated bulk calls = %d, want 1 (cache should serve season 2)", got)
	}
}

// TestGetEpisodes_Paginates verifies a series whose episodes span multiple pages
// is fully assembled across pages.
func TestGetEpisodes_Paginates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "data": map[string]any{"token": "test-token"},
			})

		case r.URL.Path == "/series/100/episodes/official":
			page := r.URL.Query().Get("page")
			if page == "0" {
				next := "page1"
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "success",
					"data": map[string]any{
						"series": map[string]any{"id": 100, "originalLanguage": "eng"},
						"episodes": []map[string]any{
							{"id": 301, "name": "Ep 1", "number": 1, "seasonNumber": 1},
						},
					},
					"links": map[string]any{"next": next},
				})
				return
			}
			// page 1 (final).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series": map[string]any{"id": 100, "originalLanguage": "eng"},
					"episodes": []map[string]any{
						{"id": 302, "name": "Ep 2", "number": 2, "seasonNumber": 1},
					},
				},
				"links": map[string]any{"next": nil},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	// Language "en" matches originalLanguage "eng" → base list only, two pages.
	eps, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"tvdb": "100"},
		SeasonNumber: 1,
		Language:     "en",
	})
	if err != nil {
		t.Fatalf("GetEpisodes() error = %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("len(episodes) = %d, want 2 (both pages assembled)", len(eps))
	}
	if eps[0].Title != "Ep 1" || eps[1].Title != "Ep 2" {
		t.Errorf("titles = [%q, %q], want [Ep 1, Ep 2]", eps[0].Title, eps[1].Title)
	}
}

func TestGetSeriesMetadataCarriesShowStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/100/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":       100,
					"name":     "Series",
					"overview": "Series overview",
					"status": map[string]any{
						"id":   1,
						"name": "Continuing",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	p := NewProviderWithClient(client)

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"tvdb": "100"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if result.ShowStatus != "Continuing" {
		t.Fatalf("ShowStatus = %q, want %q", result.ShowStatus, "Continuing")
	}
}
