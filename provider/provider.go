package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/Silo-Server/silo-plugin-tvdb/metadata"
	"github.com/Silo-Server/silo-plugin-tvdb/models"
)

const maxCast = 20

// officialSeasonType is TVDB's "Aired Order" season-type slug (Type.ID == 1),
// used by the bulk episodes endpoint to mirror the prior season filter.
const officialSeasonType = "official"

// Provider implements SearchProvider, MetadataProvider, ImageProvider,
// and EpisodeProvider for the TVDB v4 API.
type Provider struct {
	client *Client
}

// NewProvider creates a TVDB provider using the built-in project API key.
func NewProvider() *Provider {
	return &Provider{client: NewClient(10)}
}

// NewProviderWithClient creates a TVDB provider with a pre-configured client.
func NewProviderWithClient(c *Client) *Provider {
	return &Provider{client: c}
}

func (p *Provider) Slug() string       { return "tvdb" }
func (p *Provider) Name() string       { return "TheTVDB" }
func (p *Provider) ForTypes() []string { return []string{"movie", "series"} }

// ---------------------------------------------------------------------------
// SearchProvider
// ---------------------------------------------------------------------------

func (p *Provider) Search(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	// Direct TVDB ID.
	if tvdbID := query.ProviderIDs["tvdb"]; tvdbID != "" {
		id, err := strconv.Atoi(tvdbID)
		if err != nil {
			return nil, fmt.Errorf("tvdb: invalid TVDB ID %q: %w", tvdbID, err)
		}
		return p.searchByID(ctx, id, query.ContentType, query.Language)
	}

	// IMDb ID lookup.
	if imdbID := query.ProviderIDs["imdb"]; imdbID != "" {
		id, err := p.findByRemoteID(ctx, imdbID, query.ContentType)
		if err != nil || id == 0 {
			return nil, err
		}
		return p.searchByID(ctx, id, query.ContentType, query.Language)
	}

	// TMDB ID lookup.
	if tmdbID := query.ProviderIDs["tmdb"]; tmdbID != "" {
		id, err := p.findByRemoteID(ctx, tmdbID, query.ContentType)
		if err != nil || id == 0 {
			return nil, err
		}
		return p.searchByID(ctx, id, query.ContentType, query.Language)
	}

	// Title search.
	if query.Title != "" {
		return p.searchByTitle(ctx, query)
	}

	return nil, nil
}

func (p *Provider) searchByID(ctx context.Context, id int, contentType, language string) ([]metadata.SearchResult, error) {
	switch contentType {
	case "movie":
		movie, err := p.getMovieMetadata(ctx, id, language)
		if err != nil {
			return nil, err
		}
		return []metadata.SearchResult{searchResultFromMetadata(movie, p.Slug())}, nil
	case "series":
		series, err := p.getSeriesMetadata(ctx, id, language)
		if err != nil {
			return nil, err
		}
		return []metadata.SearchResult{searchResultFromMetadata(series, p.Slug())}, nil
	}
	return nil, nil
}

func (p *Provider) searchByTitle(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	results, err := p.client.Search(ctx, query.Title, query.ContentType)
	if err != nil {
		return nil, err
	}

	var out []metadata.SearchResult
	for _, r := range results {
		ids := map[string]string{"tvdb": r.TVDBID}
		fillRemoteIDs(ids, r.RemoteIDs)
		out = append(out, metadata.SearchResult{
			Name:             r.Name,
			OriginalTitle:    r.Name,
			TitleAliases:     stringAliases(r.Aliases),
			TitleLanguage:    toLang1(r.PrimaryLanguage),
			TitleIsFallback:  strings.TrimSpace(query.Language) != "" && !languageMatches(query.Language, r.PrimaryLanguage),
			OriginalLanguage: toLang1(r.PrimaryLanguage),
			Year:             extractYear(r.Year),
			ProviderIDs:      ids,
			ImageURL:         r.ImageURL,
			Overview:         r.Overview,
			Provider:         p.Slug(),
		})
	}
	return out, nil
}

func searchResultFromMetadata(result *metadata.MetadataResult, provider string) metadata.SearchResult {
	return metadata.SearchResult{
		Name:             result.Title,
		OriginalTitle:    result.OriginalTitle,
		TitleAliases:     append([]metadata.TitleAlias(nil), result.TitleAliases...),
		TitleLanguage:    result.TitleLanguage,
		TitleIsFallback:  result.TitleIsFallback,
		OriginalLanguage: result.OriginalLanguage,
		Year:             result.Year,
		ProviderIDs:      result.ProviderIDs,
		ImageURL:         result.PosterPath,
		Overview:         result.Overview,
		Provider:         provider,
	}
}

func stringAliases(values []string) []metadata.TitleAlias {
	aliases := make([]metadata.TitleAlias, 0, len(values))
	for _, value := range values {
		aliases = appendTitleAlias(aliases, value, "", "alternate")
	}
	return aliases
}

func extendedAliases(localized, original, originalLanguage string, values []Alias, translations *TranslationData) []metadata.TitleAlias {
	aliases := make([]metadata.TitleAlias, 0, len(values)+1)
	if !strings.EqualFold(strings.TrimSpace(localized), strings.TrimSpace(original)) {
		aliases = appendTitleAlias(aliases, original, toLang1(originalLanguage), "original")
	}
	for _, value := range values {
		aliases = appendTitleAlias(aliases, value.Name, toLang1(value.Language), "alternate")
	}
	if translations != nil {
		for _, translation := range translations.NameTranslations {
			kind := "localized"
			if translation.IsAlias {
				kind = "alternate"
			}
			aliases = appendTitleAlias(aliases, translation.Name, toLang1(translation.Language), kind)
		}
	}
	return aliases
}

func appendTitleAlias(aliases []metadata.TitleAlias, title, language, kind string) []metadata.TitleAlias {
	title = strings.TrimSpace(title)
	if title == "" {
		return aliases
	}
	for _, alias := range aliases {
		if strings.EqualFold(strings.TrimSpace(alias.Title), title) {
			return aliases
		}
	}
	return append(aliases, metadata.TitleAlias{Title: title, Language: language, Kind: kind})
}

func titleIsFallback(requested, originalLanguage, title, original string) bool {
	return strings.TrimSpace(requested) != "" && !languageMatches(requested, originalLanguage) &&
		strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(original))
}

func resolvedTitleLanguage(requested, originalLanguage, title, original string) string {
	if titleIsFallback(requested, originalLanguage, title, original) {
		return toLang1(originalLanguage)
	}
	if requested != "" {
		return toLang1(requested)
	}
	return toLang1(originalLanguage)
}

func (p *Provider) findByRemoteID(ctx context.Context, remoteID, mediaType string) (int, error) {
	results, err := p.client.SearchByRemoteID(ctx, remoteID)
	if err != nil {
		return 0, err
	}
	for _, r := range results {
		switch mediaType {
		case "series":
			if r.Series != nil {
				return r.Series.ID, nil
			}
		case "movie":
			if r.Movie != nil {
				return r.Movie.ID, nil
			}
		}
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// MetadataProvider
// ---------------------------------------------------------------------------

func (p *Provider) GetMetadata(ctx context.Context, req metadata.MetadataRequest) (*metadata.MetadataResult, error) {
	tvdbID := req.ProviderIDs["tvdb"]
	if tvdbID == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb: invalid TVDB ID %q: %w", tvdbID, err)
	}
	switch req.ContentType {
	case "movie":
		return p.getMovieMetadata(ctx, id, req.Language)
	case "series":
		return p.getSeriesMetadata(ctx, id, req.Language)
	}
	return nil, nil
}

func (p *Provider) GetPersonDetail(ctx context.Context, req metadata.PersonDetailRequest) (*metadata.PersonDetailResult, error) {
	var (
		id  int
		err error
	)

	switch {
	case req.ProviderIDs["tvdb"] != "":
		id, err = strconv.Atoi(req.ProviderIDs["tvdb"])
		if err != nil {
			return nil, fmt.Errorf("tvdb: invalid TVDB person ID %q: %w", req.ProviderIDs["tvdb"], err)
		}
	case req.ProviderIDs["imdb"] != "":
		id, err = p.findPersonByRemoteID(ctx, req.ProviderIDs["imdb"])
	case req.ProviderIDs["tmdb"] != "":
		id, err = p.findPersonByRemoteID(ctx, req.ProviderIDs["tmdb"])
	default:
		return nil, nil
	}
	if err != nil || id == 0 {
		return nil, err
	}

	person, err := p.client.GetPersonExtended(ctx, id)
	if err != nil {
		return nil, err
	}

	providerIDs := map[string]string{"tvdb": strconv.Itoa(person.ID)}
	fillPersonRemoteIDs(providerIDs, person.RemoteIDs)

	return &metadata.PersonDetailResult{
		Name:        person.Name,
		Bio:         findBiography(person.Biographies, req.Language),
		BirthDate:   normalizeDate(person.Birth),
		DeathDate:   normalizeDate(person.Death),
		Birthplace:  person.BirthPlace,
		PhotoPath:   person.Image,
		ProviderIDs: providerIDs,
	}, nil
}

func (p *Provider) getMovieMetadata(ctx context.Context, id int, lang string) (*metadata.MetadataResult, error) {
	movie, err := p.client.GetMovieExtended(ctx, id)
	if err != nil {
		return nil, err
	}

	title := movie.Name
	overview := findTranslationOverview(movie.Translations, lang)
	if needsTranslation(lang, movie.OriginalLanguage) {
		// Try embedded translations first (movie endpoint includes ?meta=translations).
		if name := findTranslationName(movie.Translations, lang); name != "" {
			title = name
		}
		// Fall back to dedicated translation endpoint for any missing fields.
		if title == movie.Name || overview == "" {
			tr, err := p.client.GetMovieTranslation(ctx, id, toLang3(lang))
			if err != nil {
				slog.Warn("tvdb: movie translation fetch failed", "movie_id", id, "lang", lang, "error", err)
			} else if tr != nil {
				if tr.Overview != "" && overview == "" {
					overview = tr.Overview
				}
				if tr.Name != "" && title == movie.Name {
					title = tr.Name
				}
			}
		}
	}

	result := &metadata.MetadataResult{
		HasMetadata:          true,
		Title:                title,
		OriginalTitle:        movie.Name,
		OriginalLanguage:     toLang1(movie.OriginalLanguage),
		TitleAliases:         extendedAliases(title, movie.Name, movie.OriginalLanguage, movie.Aliases, movie.Translations),
		TitleAliasesComplete: true,
		TitleLanguage:        resolvedTitleLanguage(lang, movie.OriginalLanguage, title, movie.Name),
		TitleIsFallback:      titleIsFallback(lang, movie.OriginalLanguage, title, movie.Name),
		Overview:             overview,
		Runtime:              movie.Runtime,
		Year:                 extractYear(movie.Year),
		ContentRating:        findContentRating(movie.ContentRatings),
		ProviderIDs:          map[string]string{"tvdb": strconv.Itoa(movie.ID)},
	}

	if movie.FirstRelease != nil && movie.FirstRelease.Date != "" && result.Year == 0 {
		result.Year = extractYear(movie.FirstRelease.Date)
	}

	fillRemoteIDs(result.ProviderIDs, movie.RemoteIDs)

	for _, g := range movie.Genres {
		result.Genres = append(result.Genres, g.Name)
	}
	for _, s := range movie.Studios {
		result.Studios = append(result.Studios, s.Name)
	}
	if movie.OriginalCountry != "" {
		result.Countries = []string{movie.OriginalCountry}
	}

	// Carry the top-level image so it's available even when the artworks
	// sub-response is empty (common for obscure or new titles).
	result.PosterPath = movie.Image

	result.People = convertCharacters(movie.Characters)

	return result, nil
}

func (p *Provider) getSeriesMetadata(ctx context.Context, id int, lang string) (*metadata.MetadataResult, error) {
	series, err := p.client.GetSeriesExtended(ctx, id)
	if err != nil {
		return nil, err
	}

	officialCount := 0
	for _, s := range series.Seasons {
		if s.Type.ID == 1 && s.Number > 0 {
			officialCount++
		}
	}

	title := series.Name
	overview := series.Overview
	if needsTranslation(lang, series.OriginalLanguage) {
		// Try embedded translations first (series endpoint now includes ?meta=translations).
		if name := findTranslationName(series.Translations, lang); name != "" {
			title = name
		}
		if ov := findTranslationOverview(series.Translations, lang); ov != "" {
			overview = ov
		}
		// Fall back to dedicated translation endpoint for any missing fields.
		if title == series.Name || overview == series.Overview {
			tr, err := p.client.GetSeriesTranslation(ctx, id, toLang3(lang))
			if err != nil {
				slog.Warn("tvdb: series translation fetch failed", "series_id", id, "lang", lang, "error", err)
			} else if tr != nil {
				if tr.Name != "" && title == series.Name {
					title = tr.Name
				}
				if tr.Overview != "" && overview == series.Overview {
					overview = tr.Overview
				}
			}
		}
	}

	result := &metadata.MetadataResult{
		HasMetadata:          true,
		Title:                title,
		OriginalTitle:        series.Name,
		OriginalLanguage:     toLang1(series.OriginalLanguage),
		TitleAliases:         extendedAliases(title, series.Name, series.OriginalLanguage, series.Aliases, series.Translations),
		TitleAliasesComplete: true,
		TitleLanguage:        resolvedTitleLanguage(lang, series.OriginalLanguage, title, series.Name),
		TitleIsFallback:      titleIsFallback(lang, series.OriginalLanguage, title, series.Name),
		Overview:             overview,
		Year:                 extractYear(series.Year),
		ContentRating:        findContentRating(series.ContentRatings),
		SeasonCount:          officialCount,
		FirstAirDate:         series.FirstAired,
		LastAirDate:          series.LastAired,
		AirTime:              series.AirsTime,
		ShowStatus:           series.Status.Name,
		ProviderIDs:          map[string]string{"tvdb": strconv.Itoa(series.ID)},
	}

	fillRemoteIDs(result.ProviderIDs, series.RemoteIDs)

	if series.OriginalNetwork != nil {
		result.Networks = []string{series.OriginalNetwork.Name}
	}
	for _, g := range series.Genres {
		result.Genres = append(result.Genres, g.Name)
	}
	if series.OriginalCountry != "" {
		result.Countries = []string{series.OriginalCountry}
	}

	// Carry the top-level image so it's available even when the artworks
	// sub-response is empty (common for obscure or new titles).
	result.PosterPath = series.Image

	result.People = convertCharacters(series.Characters)

	return result, nil
}

// ---------------------------------------------------------------------------
// ImageProvider
// ---------------------------------------------------------------------------

func (p *Provider) GetImages(ctx context.Context, req metadata.ImageRequest) ([]metadata.RemoteImage, error) {
	tvdbID := req.ProviderIDs["tvdb"]
	if tvdbID == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb: invalid TVDB ID: %w", err)
	}

	var artworks []ArtworkRecord
	primaryPosterURL := ""
	switch req.ContentType {
	case "movie":
		movie, err := p.client.GetMovieExtended(ctx, id)
		if err != nil {
			return nil, err
		}
		artworks = movie.Artworks
		primaryPosterURL = movie.Image
	case "series":
		series, err := p.client.GetSeriesExtended(ctx, id)
		if err != nil {
			return nil, err
		}
		artworks = series.Artworks
		primaryPosterURL = series.Image
	}

	var out []metadata.RemoteImage
	for _, a := range artworks {
		imgType, ok := artworkTypeToImageType(a.Type)
		if !ok {
			continue
		}
		out = append(out, metadata.RemoteImage{
			URL:          a.Image,
			Type:         imgType,
			Language:     toLang1(a.Language),
			Width:        a.Width,
			Height:       a.Height,
			Rating:       float64(a.Score),
			IncludesText: a.IncludesText,
		})
	}
	return preferPrimaryImage(out, metadata.ImagePoster, primaryPosterURL, ""), nil
}

// ---------------------------------------------------------------------------
// EpisodeProvider
// ---------------------------------------------------------------------------

func (p *Provider) GetSeasons(ctx context.Context, req metadata.SeasonsRequest) ([]metadata.SeasonResult, error) {
	tvdbID := req.ProviderIDs["tvdb"]
	if tvdbID == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb: invalid TVDB ID: %w", err)
	}

	series, err := p.client.GetSeriesExtended(ctx, id)
	if err != nil {
		return nil, err
	}

	// Filter to official seasons.
	var officialSeasons []SeasonBaseRecord
	for _, s := range series.Seasons {
		if s.Type.ID == 1 {
			officialSeasons = append(officialSeasons, s)
		}
	}

	seasons := make([]metadata.SeasonResult, len(officialSeasons))
	for i, s := range officialSeasons {
		seasons[i] = metadata.SeasonResult{
			SeasonNumber: s.Number,
			PosterPath:   s.Image,
		}
	}

	if needsTranslation(req.Language, series.OriginalLanguage) {
		lang3 := toLang3(req.Language)
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(5)

		for i, s := range officialSeasons {
			g.Go(func() error {
				tr, err := p.client.GetSeasonTranslation(gctx, s.ID, lang3)
				if err != nil {
					if gctx.Err() != nil {
						return gctx.Err()
					}
					slog.Warn("tvdb: season translation fetch failed", "season_id", s.ID, "lang", lang3, "error", err)
					return nil
				}
				if tr != nil {
					seasons[i].Title = tr.Name
					seasons[i].Overview = tr.Overview
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return nil, err
		}
	}

	return seasons, nil
}

func (p *Provider) GetEpisodes(ctx context.Context, req metadata.EpisodesRequest) ([]metadata.EpisodeResult, error) {
	tvdbID := req.ProviderIDs["tvdb"]
	if tvdbID == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb: invalid TVDB ID: %w", err)
	}

	// Fetch the whole series' episodes in one bulk (paginated, cached) call
	// instead of one translation round-trip per episode. officialSeasonType
	// mirrors the previous "official / Aired Order" (Type.ID == 1) filter.
	base, err := p.client.getAllSeriesEpisodes(ctx, id, officialSeasonType, "")
	if err != nil {
		return nil, err
	}

	// When a non-native language is requested, pull the translated bulk list
	// once and index it by episode ID to overlay onto the base records.
	translated := map[int]EpisodeBaseRecord{}
	if needsTranslation(req.Language, base.series.OriginalLanguage) {
		lang3 := toLang3(req.Language)
		tr, err := p.client.getAllSeriesEpisodes(ctx, id, officialSeasonType, lang3)
		if err != nil {
			return nil, err
		}
		for _, ep := range tr.episodes {
			translated[ep.ID] = ep
		}
	}

	var episodes []metadata.EpisodeResult
	for _, ep := range base.episodes {
		if ep.SeasonNumber != req.SeasonNumber {
			continue
		}
		title, overview := ep.Name, ep.Overview
		// Overlay the translation only when present, so an episode without a
		// translation keeps its original-language title/overview.
		if tr, ok := translated[ep.ID]; ok {
			if tr.Name != "" {
				title = tr.Name
			}
			if tr.Overview != "" {
				overview = tr.Overview
			}
		}
		episodes = append(episodes, metadata.EpisodeResult{
			ProviderIDs:   map[string]string{"tvdb": strconv.Itoa(ep.ID)},
			SeasonNumber:  ep.SeasonNumber,
			EpisodeNumber: ep.Number,
			Title:         title,
			Overview:      overview,
			Runtime:       ep.Runtime,
			AirDate:       ep.Aired,
			StillPath:     ep.Image,
		})
	}

	return episodes, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractYear(yearStr string) int {
	if len(yearStr) < 4 {
		return 0
	}
	y, err := strconv.Atoi(yearStr[:4])
	if err != nil {
		return 0
	}
	return y
}

// findTranslationName selects a name translation matching the requested
// language from embedded translation data.
func findTranslationName(td *TranslationData, lang string) string {
	if td == nil {
		return ""
	}
	for _, t := range td.NameTranslations {
		if t.Name != "" && languageMatches(lang, t.Language) {
			return t.Name
		}
	}
	return ""
}

// findTranslationOverview selects an overview translation matching the
// requested language. Fallback order: requested → primary → first non-empty.
func findTranslationOverview(td *TranslationData, lang string) string {
	if td == nil {
		return ""
	}
	for _, t := range td.OverviewTranslations {
		if t.Overview != "" && languageMatches(lang, t.Language) {
			return t.Overview
		}
	}
	for _, t := range td.OverviewTranslations {
		if t.IsPrimary && t.Overview != "" {
			return t.Overview
		}
	}
	for _, t := range td.OverviewTranslations {
		if t.Overview != "" {
			return t.Overview
		}
	}
	return ""
}

func findContentRating(ratings []ContentRating) string {
	if len(ratings) == 0 {
		return ""
	}
	for _, r := range ratings {
		if r.Country == "usa" {
			return r.Name
		}
	}
	return ratings[0].Name
}

func convertCharacters(chars []Character) []models.ItemPerson {
	var people []models.ItemPerson
	castCount := 0
	for _, ch := range chars {
		var kind models.PersonKind
		switch ch.PeopleType {
		case "Actor":
			if castCount >= maxCast {
				continue
			}
			kind = models.PersonKindActor
			castCount++
		case "Guest Star":
			if castCount >= maxCast {
				continue
			}
			kind = models.PersonKindGuestStar
			castCount++
		case "Director":
			kind = models.PersonKindDirector
		case "Writer":
			kind = models.PersonKindWriter
		case "Producer":
			kind = models.PersonKindProducer
		default:
			continue
		}
		people = append(people, models.ItemPerson{
			Person: models.Person{
				Name:      ch.PersonName,
				TvdbID:    strconv.Itoa(ch.PeopleID),
				PhotoPath: ch.PersonImgURL,
			},
			Kind:      kind,
			Character: ch.Name,
			SortOrder: ch.Sort,
		})
	}
	return people
}

func fillRemoteIDs(ids map[string]string, remoteIDs []RemoteID) {
	fillRemoteIDsWithIMDbPrefix(ids, remoteIDs, "tt")
}

func fillPersonRemoteIDs(ids map[string]string, remoteIDs []RemoteID) {
	fillRemoteIDsWithIMDbPrefix(ids, remoteIDs, "nm")
}

func fillRemoteIDsWithIMDbPrefix(ids map[string]string, remoteIDs []RemoteID, imdbPrefix string) {
	for _, r := range remoteIDs {
		provider := remoteIDProvider(r)
		id, ok := canonicalRemoteProviderID(provider, r.ID, imdbPrefix)
		if !ok {
			continue
		}
		switch provider {
		case "imdb":
			if ids["imdb"] == "" {
				ids["imdb"] = id
			}
		case "tmdb":
			if ids["tmdb"] == "" {
				ids["tmdb"] = id
			}
		}
	}
}

func canonicalRemoteProviderID(provider, value, imdbPrefix string) (string, bool) {
	value = strings.TrimSpace(value)
	switch provider {
	case "tmdb":
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			return "", false
		}
		return strconv.Itoa(id), true
	case "imdb":
		value = strings.ToLower(value)
		if len(value) < 9 || len(value) > 12 || value[:2] != imdbPrefix {
			return "", false
		}
		digits := value[2:]
		if len(digits) < 7 || len(digits) > 10 {
			return "", false
		}
		id, err := strconv.ParseUint(digits, 10, 64)
		if err != nil || id == 0 {
			return "", false
		}
		return value, true
	default:
		return "", false
	}
}

func remoteIDProvider(r RemoteID) string {
	switch r.Type {
	case 2:
		return "imdb"
	case 12:
		return "tmdb"
	}

	switch normalizeRemoteIDSourceName(r.SourceName) {
	case "imdb", "imdbcom":
		return "imdb"
	case "tmdb", "themoviedb", "themoviedbcom", "themoviedatabase":
		return "tmdb"
	default:
		return ""
	}
}

func normalizeRemoteIDSourceName(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	replacer := strings.NewReplacer(" ", "", ".", "", "-", "", "_", "")
	return replacer.Replace(source)
}

func (p *Provider) findPersonByRemoteID(ctx context.Context, remoteID string) (int, error) {
	results, err := p.client.SearchByRemoteID(ctx, remoteID)
	if err != nil {
		return 0, err
	}
	for _, r := range results {
		if r.People != nil {
			return r.People.ID, nil
		}
	}
	return 0, nil
}

func artworkTypeToImageType(artType int) (metadata.ImageType, bool) {
	switch artType {
	case 2:
		return metadata.ImagePoster, true
	case 3:
		return metadata.ImageBackdrop, true
	case 22:
		return metadata.ImageLogo, true
	default:
		return 0, false
	}
}

func preferPrimaryImage(
	images []metadata.RemoteImage,
	imageType metadata.ImageType,
	primaryURL, language string,
) []metadata.RemoteImage {
	primaryURL = strings.TrimSpace(primaryURL)
	if primaryURL == "" {
		return images
	}

	bestRating := 0.0
	primaryIdx := -1
	for i, img := range images {
		if img.Type != imageType {
			continue
		}
		if img.Rating > bestRating {
			bestRating = img.Rating
		}
		if img.URL == primaryURL {
			primaryIdx = i
		}
	}

	if primaryIdx >= 0 {
		images[primaryIdx].Rating = bestRating + 1
		if images[primaryIdx].Language == "" && language != "" {
			images[primaryIdx].Language = language
		}
		return images
	}

	return append(images, metadata.RemoteImage{
		URL:      primaryURL,
		Type:     imageType,
		Language: language,
		Rating:   bestRating + 1,
	})
}

func findBiography(biographies []Biography, requestedLanguage string) string {
	if len(biographies) == 0 {
		return ""
	}
	if bio := findBiographyByLanguage(biographies, requestedLanguage); bio != "" {
		return bio
	}
	if bio := findBiographyByLanguage(biographies, "eng"); bio != "" {
		return bio
	}
	if bio := findBiographyByLanguage(biographies, "en"); bio != "" {
		return bio
	}
	return biographies[0].Biography
}

func findBiographyByLanguage(biographies []Biography, requestedLanguage string) string {
	for _, biography := range biographies {
		if biography.Biography == "" {
			continue
		}
		if languageMatches(requestedLanguage, biography.Language) {
			return biography.Biography
		}
	}
	return ""
}

func languageMatches(requested, candidate string) bool {
	req := normalizeLanguageTag(requested)
	got := normalizeLanguageTag(candidate)
	if req == "" || got == "" {
		return false
	}
	if req == got {
		return true
	}
	return toLang1(req) == toLang1(got)
}

func normalizeLanguageTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if idx := strings.IndexAny(value, "-_"); idx >= 0 {
		value = value[:idx]
	}
	return value
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 && value[4] == '-' && value[7] == '-' {
		return value[:10]
	}
	return value
}
