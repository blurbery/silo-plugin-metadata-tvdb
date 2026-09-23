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

// artworkFixture builds a TVDB artwork JSON object for image test responses.
// A nil includesText leaves the key out entirely, matching a provider that
// does not report text presence; extra merges in optional keys such as
// "thumbnail" or "language".
func artworkFixture(id, artType int, url string, width, height, score int, includesText any, extra map[string]any) map[string]any {
	artwork := map[string]any{
		"id":     id,
		"type":   artType,
		"image":  url,
		"width":  width,
		"height": height,
		"score":  score,
	}
	if includesText != nil {
		artwork["includesText"] = includesText
	}
	for key, value := range extra {
		artwork[key] = value
	}
	return artwork
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
						artworkFixture(1, 2, "https://artworks.example/poster-original.jpg", 2000, 3000, 10, true, map[string]any{
							"thumbnail": "https://artworks.example/poster-thumb.jpg",
						}),
						artworkFixture(2, 3, "https://artworks.example/background-original.jpg", 3840, 2160, 8, false, map[string]any{
							"thumbnail": "",
						}),
						artworkFixture(3, 22, "https://artworks.example/logo-original.png", 1000, 400, 7, nil, nil),
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
	if len(images) != 3 {
		t.Fatalf("len(images) = %d, want 3", len(images))
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
	if got[metadata.ImageLogo].URL != "https://artworks.example/logo-original.png" {
		t.Fatalf("logo URL = %q", got[metadata.ImageLogo].URL)
	}
	if got[metadata.ImageLogo].IncludesText != nil {
		t.Fatalf("logo IncludesText = %v, want nil for an omitted provider value", got[metadata.ImageLogo].IncludesText)
	}
}

func TestGetImagesReturnsExactSeasonGallery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id": 99,
					"artworks": []map[string]any{{
						"type": 2, "image": "https://artworks.example/show-poster.jpg",
					}},
					"seasons": []map[string]any{
						{"id": 700, "number": 0, "type": map[string]any{"id": 1, "name": "Official"}},
						{"id": 699, "number": 0, "type": map[string]any{"id": 1, "name": "Official"}},
						{"id": 701, "number": 1, "type": map[string]any{"id": 1, "name": "Official"}},
						{"id": 799, "number": 0, "type": map[string]any{"id": 2, "name": "DVD"}},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/seasons/699/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":     699,
					"number": 0,
					"image":  "https://artworks.example/specials-primary.jpg",
					"artwork": []map[string]any{
						{"id": 1, "type": 7, "image": "https://artworks.example/specials-one.jpg", "language": "eng", "score": 9, "width": 2000, "height": 3000},
						{"id": 2, "type": 14, "image": "https://artworks.example/specials-two.jpg", "language": "fra", "score": 8, "width": 2000, "height": 3000},
						{"id": 3, "type": 7, "image": "https://artworks.example/specials-landscape.jpg", "score": 10, "width": 3840, "height": 2160},
						{"id": 4, "type": 7, "image": "https://artworks.example/specials-unknown-size.jpg", "score": 10, "width": 0, "height": 0},
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
	specials := 0
	images, err := NewProviderWithClient(client).GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs:  map[string]string{"tvdb": "99"},
		ContentType:  "series",
		SeasonNumber: &specials,
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 3 {
		t.Fatalf("images = %#v, want two season artworks plus primary", images)
	}
	gotURLs := make(map[string]bool, len(images))
	for _, image := range images {
		gotURLs[image.URL] = true
		if image.Type != metadata.ImagePoster {
			t.Fatalf("image type = %v, want season poster", image.Type)
		}
		if image.SeasonNumber == nil || *image.SeasonNumber != 0 {
			t.Fatalf("image SeasonNumber = %v, want present Specials value 0", image.SeasonNumber)
		}
		if strings.Contains(image.URL, "show-poster") {
			t.Fatalf("show poster leaked into exact season gallery: %#v", image)
		}
		if image.URL == "https://artworks.example/specials-primary.jpg" && image.Rating != 0 {
			t.Fatalf("primary fallback rating = %v, want unknown score 0", image.Rating)
		}
	}
	for _, want := range []string{
		"https://artworks.example/specials-one.jpg",
		"https://artworks.example/specials-unknown-size.jpg",
		"https://artworks.example/specials-primary.jpg",
	} {
		if !gotURLs[want] {
			t.Errorf("season gallery missing %q: %#v", want, images)
		}
	}
	for _, rejected := range []string{
		"https://artworks.example/specials-two.jpg",
		"https://artworks.example/specials-landscape.jpg",
	} {
		if gotURLs[rejected] {
			t.Errorf("season gallery included rejected artwork %q: %#v", rejected, images)
		}
	}
}

func TestGetImagesDoesNotAppendFilteredSeasonPrimary(t *testing.T) {
	t.Parallel()

	const filteredPrimary = "https://artworks.example/not-a-season-poster.jpg"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "data": map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id": 99,
					"seasons": []map[string]any{
						{"id": 701, "number": 1, "type": map[string]any{"id": 1}},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/seasons/701/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id": 701, "number": 1, "image": filteredPrimary,
					"artwork": []map[string]any{
						{"id": 1, "type": 14, "image": filteredPrimary, "width": 2000, "height": 3000},
						{"id": 2, "type": 7, "image": "https://artworks.example/accepted.jpg", "width": 2000, "height": 3000},
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
	seasonNumber := 1
	images, err := NewProviderWithClient(client).GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tvdb": "99"}, ContentType: "series", SeasonNumber: &seasonNumber,
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 1 || images[0].URL != "https://artworks.example/accepted.jpg" {
		t.Fatalf("images = %#v, want only the accepted season poster", images)
	}
}

func TestSeriesExtendedCacheSharedBySeasonsAndGallery(t *testing.T) {
	t.Parallel()

	var seriesCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "data": map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			seriesCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id": 99,
					"seasons": []map[string]any{
						{"id": 701, "number": 1, "image": "https://artworks.example/season.jpg", "type": map[string]any{"id": 1}},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/seasons/701/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id": 701, "number": 1, "image": "https://artworks.example/season.jpg",
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	provider := NewProviderWithClient(client)
	seasons, err := provider.GetSeasons(context.Background(), metadata.SeasonsRequest{
		ProviderIDs: map[string]string{"tvdb": "99"},
	})
	if err != nil {
		t.Fatalf("GetSeasons() error = %v", err)
	}
	if len(seasons) != 1 {
		t.Fatalf("seasons = %#v, want one official season", seasons)
	}

	seasonNumber := 1
	images, err := provider.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"tvdb": "99"}, ContentType: "series", SeasonNumber: &seasonNumber,
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if len(images) != 1 || images[0].URL != "https://artworks.example/season.jpg" || images[0].Rating != 0 {
		t.Fatalf("images = %#v, want the zero-rated canonical season poster", images)
	}
	if got := seriesCalls.Load(); got != 1 {
		t.Fatalf("series extended calls = %d, want 1 shared by seasons and gallery", got)
	}
}

func TestGetSeasonsPicksPosterInRequestedLanguage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "data": map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/99/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id": 99, "originalLanguage": "eng",
					"seasons": []map[string]any{
						{"id": 701, "number": 1, "image": "https://artworks.example/s1-hun.jpg", "type": map[string]any{"id": 1}},
						{"id": 702, "number": 2, "image": "https://artworks.example/s2-primary.jpg", "type": map[string]any{"id": 1}},
						{"id": 703, "number": 3, "image": "https://artworks.example/s3.jpg", "type": map[string]any{"id": 1}},
					},
					"artworks": []map[string]any{
						{"id": 1, "type": 7, "seasonId": 701, "image": "https://artworks.example/s1-hun.jpg", "language": "hun", "score": 1, "includesText": true, "width": 400, "height": 578},
						{"id": 2, "type": 7, "seasonId": 701, "image": "https://artworks.example/s1-eng-low.jpg", "language": "eng", "score": 5, "width": 400, "height": 578},
						{"id": 3, "type": 7, "seasonId": 701, "image": "https://artworks.example/s1-eng.jpg", "language": "eng", "score": 100000, "includesText": true, "width": 400, "height": 578},
						{"id": 4, "type": 7, "seasonId": 702, "image": "https://artworks.example/s2-primary.jpg", "language": "eng", "score": 10, "width": 400, "height": 578},
						{"id": 5, "type": 7, "seasonId": 702, "image": "https://artworks.example/s2-eng.jpg", "language": "eng", "score": 100000, "width": 400, "height": 578},
						{"id": 7, "type": 6, "seasonId": 703, "image": "https://artworks.example/s3-banner.jpg", "language": "eng", "score": 100000, "width": 758, "height": 140},
						{"id": 6, "type": 2, "image": "https://artworks.example/series-poster.jpg", "language": "eng", "score": 100000, "width": 680, "height": 1000},
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
	seasons, err := NewProviderWithClient(client).GetSeasons(context.Background(), metadata.SeasonsRequest{
		ProviderIDs: map[string]string{"tvdb": "99"}, Language: "en",
	})
	if err != nil {
		t.Fatalf("GetSeasons() error = %v", err)
	}

	want := map[int]string{
		1: "https://artworks.example/s1-eng.jpg",
		2: "https://artworks.example/s2-primary.jpg",
		3: "https://artworks.example/s3.jpg",
	}
	if len(seasons) != len(want) {
		t.Fatalf("seasons = %#v, want %d seasons", seasons, len(want))
	}
	for _, season := range seasons {
		if season.PosterPath != want[season.SeasonNumber] {
			t.Errorf("season %d poster = %q, want %q", season.SeasonNumber, season.PosterPath, want[season.SeasonNumber])
		}
	}
}

func TestSeasonPosterPath(t *testing.T) {
	t.Parallel()

	textless, hasText := false, true
	poster := func(url, language string, score int) ArtworkRecord {
		return ArtworkRecord{Type: seasonPosterArtworkTypeID, Image: url, Language: language, Score: score, Width: 400, Height: 578}
	}
	textlessPoster := func(url, language string, score int) ArtworkRecord {
		artwork := poster(url, language, score)
		artwork.IncludesText = &textless
		return artwork
	}
	landscape := poster("wide", "eng", 100000)
	landscape.Width, landscape.Height = 1000, 562

	tests := []struct {
		name     string
		artworks []ArtworkRecord
		primary  string
		language string
		want     string
	}{
		{
			name:     "requested language beats English",
			artworks: []ArtworkRecord{poster("en", "eng", 100000), poster("de", "deu", 0)},
			primary:  "en",
			language: "de-DE",
			want:     "de",
		},
		{
			name:     "English is the fallback language",
			artworks: []ArtworkRecord{poster("hu", "hun", 100000), poster("en", "eng", 0)},
			primary:  "hu",
			language: "fr",
			want:     "en",
		},
		{
			name:     "no requested language prefers English",
			artworks: []ArtworkRecord{poster("ru", "rus", 100000), poster("en", "eng", 0)},
			primary:  "ru",
			want:     "en",
		},
		{
			name:     "primary wins within its tier",
			artworks: []ArtworkRecord{poster("en-top", "eng", 100000), poster("en-primary", "eng", 1)},
			primary:  "en-primary",
			language: "en",
			want:     "en-primary",
		},
		{
			name:     "highest score wins within a tier",
			artworks: []ArtworkRecord{poster("de-low", "deu", 1), poster("de-high", "deu", 9), poster("de-mid", "deu", 5)},
			language: "de",
			want:     "de-high",
		},
		{
			name:     "equal scores keep API order",
			artworks: []ArtworkRecord{poster("first", "eng", 7), poster("second", "eng", 7)},
			language: "en",
			want:     "first",
		},
		{
			name:     "other text beats textless art",
			artworks: []ArtworkRecord{textlessPoster("clean", "eng", 100000), poster("ja", "jpn", 0)},
			primary:  "clean",
			language: "en",
			want:     "ja",
		},
		{
			name:     "untagged art is textless",
			artworks: []ArtworkRecord{poster("untagged", "", 100000), poster("es", "spa", 0)},
			language: "en",
			want:     "es",
		},
		{
			name:     "textless art is used when nothing has text",
			artworks: []ArtworkRecord{textlessPoster("clean-low", "", 1), textlessPoster("clean-high", "", 2)},
			language: "en",
			want:     "clean-high",
		},
		{
			name:     "requested language as a three-letter code",
			artworks: []ArtworkRecord{poster("en", "eng", 100000), poster("de", "deu", 0)},
			language: "deu",
			want:     "de",
		},
		{
			name: "text flag without a language ranks as other text",
			artworks: []ArtworkRecord{
				textlessPoster("clean", "eng", 100000),
				{Type: seasonPosterArtworkTypeID, Image: "untagged-text", Score: 1, IncludesText: &hasText},
			},
			language: "en",
			want:     "untagged-text",
		},
		{
			name:     "any season poster beats an unlisted primary",
			artworks: []ArtworkRecord{poster("de", "deu", 0)},
			primary:  "unlisted",
			language: "en",
			want:     "de",
		},
		{
			name:    "unlisted primary is kept when the season has no poster",
			primary: "unlisted",
			want:    "unlisted",
		},
		{
			name:     "landscape art is skipped",
			artworks: []ArtworkRecord{landscape, poster("de", "deu", 0)},
			primary:  "wide",
			language: "en",
			want:     "de",
		},
		{
			name: "banner primary is skipped",
			artworks: []ArtworkRecord{
				{Type: 6, Image: "banner", Language: "eng", Score: 100000, Width: 758, Height: 140},
				poster("de", "deu", 0),
			},
			primary:  "banner",
			language: "en",
			want:     "de",
		},
		{
			name: "banner primary is not used as a poster",
			artworks: []ArtworkRecord{
				{Type: 6, Image: "banner", Language: "eng", Score: 100000, Width: 758, Height: 140},
			},
			primary:  "banner",
			language: "en",
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := seasonPosterPath(tt.artworks, tt.primary, tt.language); got != tt.want {
				t.Fatalf("seasonPosterPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsurePrimaryImagePreservesProviderScores(t *testing.T) {
	t.Parallel()

	images := []metadata.RemoteImage{
		{URL: "https://artworks.example/primary.jpg", Type: metadata.ImagePoster, Rating: 7},
		{URL: "https://artworks.example/top-voted.jpg", Type: metadata.ImagePoster, Rating: 9},
	}
	got := ensurePrimaryImage(images, metadata.ImagePoster, "https://artworks.example/primary.jpg", "", false)
	if len(got) != 2 {
		t.Fatalf("images = %#v, want no duplicate primary", got)
	}
	if got[0].Rating != 7 || got[1].Rating != 9 {
		t.Fatalf("ratings = [%v, %v], want provider scores [7, 9]", got[0].Rating, got[1].Rating)
	}

	got = ensurePrimaryImage(got, metadata.ImagePoster, "https://artworks.example/missing-primary.jpg", "", false)
	if len(got) != 3 || got[2].Rating != 0 {
		t.Fatalf("missing primary = %#v, want appended poster with unknown score 0", got)
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
						artworkFixture(1, 2, "https://artworks.example/poster-primary.jpg", 2000, 3000, 10, true, map[string]any{
							"language": "eng",
						}),
						artworkFixture(2, 2, "https://artworks.example/poster-textless.jpg", 2000, 3000, 11, false, map[string]any{
							"language": "",
						}),
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
						artworkFixture(2, 2, "https://artworks.example/poster-alt.jpg", 2000, 3000, 11, nil, map[string]any{
							"language": "",
						}),
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
