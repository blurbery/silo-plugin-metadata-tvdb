package provider

// ---------------------------------------------------------------------------
// Generic API envelope
// ---------------------------------------------------------------------------

// apiResponse is the common envelope for all TVDB v4 API responses.
type apiResponse[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
	Links  *links `json:"links,omitempty"`
}

type links struct {
	Prev       *string `json:"prev"`
	Self       string  `json:"self"`
	Next       *string `json:"next"`
	TotalItems int     `json:"total_items"`
	PageSize   int     `json:"page_size"`
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

// loginResponse is returned by POST /login.
type loginResponse struct {
	Status string     `json:"status"`
	Data   loginToken `json:"data"`
}

type loginToken struct {
	Token string `json:"token"`
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// SearchResult represents a single result from GET /search.
type SearchResult struct {
	ObjectID        string     `json:"objectID"`
	Country         string     `json:"country"`
	Director        string     `json:"director"`
	ExtendedTitle   string     `json:"extended_title"`
	Genres          []string   `json:"genres"`
	Studios         []string   `json:"studios"`
	ID              string     `json:"id"`
	ImageURL        string     `json:"image_url"`
	Name            string     `json:"name"`
	FirstAirTime    string     `json:"first_air_time"`
	Overview        string     `json:"overview"`
	PrimaryLanguage string     `json:"primary_language"`
	PrimaryType     string     `json:"primary_type"`
	Status          string     `json:"status"`
	Type            string     `json:"type"`
	TVDBID          string     `json:"tvdb_id"`
	Year            string     `json:"year"`
	Slug            string     `json:"slug"`
	Aliases         []string   `json:"aliases"`
	RemoteIDs       []RemoteID `json:"remote_ids"`
}

// RemoteIDResult is returned by GET /search/remoteid/{id}.
type RemoteIDResult struct {
	Series *SeriesBaseRecord `json:"series,omitempty"`
	Movie  *MovieBaseRecord  `json:"movie,omitempty"`
	People *PeopleBaseRecord `json:"people,omitempty"`
}

// ---------------------------------------------------------------------------
// Series
// ---------------------------------------------------------------------------

// SeriesBaseRecord is the base series record.
type SeriesBaseRecord struct {
	ID                int          `json:"id"`
	Name              string       `json:"name"`
	Slug              string       `json:"slug"`
	Image             string       `json:"image"`
	FirstAired        string       `json:"firstAired"`
	LastAired         string       `json:"lastAired"`
	NextAired         string       `json:"nextAired"`
	Score             float64      `json:"score"`
	Status            StatusRecord `json:"status"`
	OriginalCountry   string       `json:"originalCountry"`
	OriginalLanguage  string       `json:"originalLanguage"`
	Overview          string       `json:"overview"`
	Year              string       `json:"year"`
	AverageRuntime    int          `json:"averageRuntime"`
	DefaultSeasonType int          `json:"defaultSeasonType"`
	LastUpdated       string       `json:"lastUpdated"`
}

// SeriesExtendedRecord is returned by GET /series/{id}/extended.
type SeriesExtendedRecord struct {
	SeriesBaseRecord
	Aliases         []Alias            `json:"aliases"`
	Artworks        []ArtworkRecord    `json:"artworks"`
	Characters      []Character        `json:"characters"`
	Companies       []CompanyRecord    `json:"companies"`
	ContentRatings  []ContentRating    `json:"contentRatings"`
	Genres          []GenreRecord      `json:"genres"`
	RemoteIDs       []RemoteID         `json:"remoteIds"`
	Seasons         []SeasonBaseRecord `json:"seasons"`
	SeasonTypes     []SeasonType       `json:"seasonTypes"`
	Tags            []TagOption        `json:"tags"`
	Trailers        []Trailer          `json:"trailers"`
	Translations    *TranslationData   `json:"translations"`
	LatestNetwork   *NetworkRecord     `json:"latestNetwork"`
	OriginalNetwork *NetworkRecord     `json:"originalNetwork"`
	AirsTime        string             `json:"airsTime"`
}

// ---------------------------------------------------------------------------
// Movies
// ---------------------------------------------------------------------------

// MovieBaseRecord is the base movie record.
type MovieBaseRecord struct {
	ID               int          `json:"id"`
	Name             string       `json:"name"`
	Slug             string       `json:"slug"`
	Image            string       `json:"image"`
	Score            float64      `json:"score"`
	Status           StatusRecord `json:"status"`
	OriginalCountry  string       `json:"originalCountry"`
	OriginalLanguage string       `json:"originalLanguage"`
	Year             string       `json:"year"`
	Runtime          int          `json:"runtime"`
	LastUpdated      string       `json:"lastUpdated"`
}

// MovieExtendedRecord is returned by GET /movies/{id}/extended.
type MovieExtendedRecord struct {
	MovieBaseRecord
	Aliases             []Alias             `json:"aliases"`
	Artworks            []ArtworkRecord     `json:"artworks"`
	Characters          []Character         `json:"characters"`
	Companies           MovieCompanies      `json:"companies"`
	ContentRatings      []ContentRating     `json:"contentRatings"`
	FirstRelease        *ReleaseRecord      `json:"first_release"`
	Genres              []GenreRecord       `json:"genres"`
	ProductionCountries []ProductionCountry `json:"production_countries"`
	Releases            []ReleaseRecord     `json:"releases"`
	RemoteIDs           []RemoteID          `json:"remoteIds"`
	SpokenLanguages     []string            `json:"spoken_languages"`
	Studios             []StudioRecord      `json:"studios"`
	Trailers            []Trailer           `json:"trailers"`
	Translations        *TranslationData    `json:"translations"`
}

// MovieCompanies groups companies by role for movies.
type MovieCompanies struct {
	Studio         []CompanyRecord `json:"studio"`
	Network        []CompanyRecord `json:"network"`
	Production     []CompanyRecord `json:"production"`
	Distributor    []CompanyRecord `json:"distributor"`
	SpecialEffects []CompanyRecord `json:"special_effects"`
}

// ---------------------------------------------------------------------------
// Seasons
// ---------------------------------------------------------------------------

// SeasonBaseRecord is a season summary within a series.
type SeasonBaseRecord struct {
	ID       int        `json:"id"`
	SeriesID int        `json:"seriesId"`
	Type     SeasonType `json:"type"`
	Number   int        `json:"number"`
	Image    string     `json:"image"`
	Year     string     `json:"year"`
}

// SeasonExtendedRecord is returned by GET /seasons/{id}/extended.
type SeasonExtendedRecord struct {
	SeasonBaseRecord
	Artwork      []ArtworkRecord     `json:"artwork"`
	Episodes     []EpisodeBaseRecord `json:"episodes"`
	Trailers     []Trailer           `json:"trailers"`
	Translations *TranslationData    `json:"translations"`
}

// ---------------------------------------------------------------------------
// Episodes
// ---------------------------------------------------------------------------

// SeriesEpisodesData is the data object returned by the bulk episodes endpoint
// GET /series/{id}/episodes/{season-type}[/{lang}]. It carries the series record
// plus a page of that series' episodes (already translated when a language is
// requested).
type SeriesEpisodesData struct {
	Series   SeriesBaseRecord    `json:"series"`
	Episodes []EpisodeBaseRecord `json:"episodes"`
}

// EpisodeBaseRecord is an episode from a season extended response.
type EpisodeBaseRecord struct {
	ID                int     `json:"id"`
	SeriesID          int     `json:"seriesId"`
	Name              string  `json:"name"`
	Aired             string  `json:"aired"`
	Runtime           int     `json:"runtime"`
	Overview          string  `json:"overview"`
	Image             string  `json:"image"`
	ImageType         int     `json:"imageType"`
	IsMovie           int     `json:"isMovie"`
	Number            int     `json:"number"`
	SeasonNumber      int     `json:"seasonNumber"`
	AbsoluteNumber    int     `json:"absoluteNumber"`
	FinaleType        *string `json:"finaleType"`
	LastUpdated       string  `json:"lastUpdated"`
	Year              string  `json:"year"`
	AirsBeforeSeason  int     `json:"airsBeforeSeason"`
	AirsBeforeEpisode int     `json:"airsBeforeEpisode"`
}

// ---------------------------------------------------------------------------
// People
// ---------------------------------------------------------------------------

// PeopleBaseRecord is a person record.
type PeopleBaseRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// PeopleExtendedRecord is returned by GET /people/{id}/extended.
type PeopleExtendedRecord struct {
	PeopleBaseRecord
	Birth       string      `json:"birth"`
	BirthPlace  string      `json:"birthPlace"`
	Biographies []Biography `json:"biographies"`
	Death       string      `json:"death"`
	Image       string      `json:"image"`
	RemoteIDs   []RemoteID  `json:"remoteIds"`
}

// Character represents a cast/crew member in series/movie extended responses.
type Character struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PeopleID     int    `json:"peopleId"`
	SeriesID     *int   `json:"seriesId"`
	MovieID      *int   `json:"movieId"`
	EpisodeID    *int   `json:"episodeId"`
	Type         int    `json:"type"`
	Image        string `json:"image"`
	Sort         int    `json:"sort"`
	IsFeatured   bool   `json:"isFeatured"`
	URL          string `json:"url"`
	PeopleType   string `json:"peopleType"`
	PersonName   string `json:"personName"`
	PersonImgURL string `json:"personImgURL"`
}

// ---------------------------------------------------------------------------
// Shared sub-types
// ---------------------------------------------------------------------------

// StatusRecord represents the status of an entity.
type StatusRecord struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	RecordType  string `json:"recordType"`
	KeepUpdated bool   `json:"keepUpdated"`
}

// Alias is an alternative name for an entity.
type Alias struct {
	Language string `json:"language"`
	Name     string `json:"name"`
}

// Biography is a localized biography for a person.
type Biography struct {
	Biography string `json:"biography"`
	Language  string `json:"language"`
}

// ArtworkRecord is an image associated with an entity.
type ArtworkRecord struct {
	ID           int    `json:"id"`
	Image        string `json:"image"`
	Thumbnail    string `json:"thumbnail"`
	Language     string `json:"language"`
	Type         int    `json:"type"`
	Score        int    `json:"score"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	IncludesText *bool  `json:"includesText"`
}

// ContentRating is a content/age rating for a given country.
type ContentRating struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Country     string  `json:"country"`
	Description string  `json:"description"`
	ContentType string  `json:"contentType"`
	Order       int     `json:"order"`
	Fullname    *string `json:"fullname"`
}

// GenreRecord is a genre classification.
type GenreRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// RemoteID is a cross-reference to an external provider.
type RemoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

// SeasonType classifies the type of a season.
type SeasonType struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	AlternateName *string `json:"alternateName"`
}

// CompanyRecord is a production company, network, or studio.
type CompanyRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// NetworkRecord is a broadcast or streaming network.
type NetworkRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// StudioRecord is a production studio.
type StudioRecord struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// TagOption is a user-contributed tag on an entity.
type TagOption struct {
	ID      int    `json:"id"`
	Tag     int    `json:"tag"`
	TagName string `json:"tagName"`
	Name    string `json:"name"`
}

// Trailer is a video trailer for an entity.
type Trailer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Language string `json:"language"`
	Runtime  int    `json:"runtime"`
}

// ReleaseRecord is a release date for a movie in a given country.
type ReleaseRecord struct {
	Country string `json:"country"`
	Date    string `json:"date"`
	Detail  string `json:"detail"`
}

// ProductionCountry is a country where a movie was produced.
type ProductionCountry struct {
	ID      int    `json:"id"`
	Country string `json:"country"`
	Name    string `json:"name"`
}

// ---------------------------------------------------------------------------
// Translations
// ---------------------------------------------------------------------------

// TranslationData holds name and overview translations.
type TranslationData struct {
	NameTranslations     []Translation `json:"nameTranslations"`
	OverviewTranslations []Translation `json:"overviewTranslations"`
}

// Translation is a single translation record.
type Translation struct {
	Language  string `json:"language"`
	Name      string `json:"name"`
	Overview  string `json:"overview"`
	Tagline   string `json:"tagline"`
	IsAlias   bool   `json:"isAlias"`
	IsPrimary bool   `json:"isPrimary"`
}

// TranslationRecord is returned by the per-language translation endpoints
// (/series/{id}/translations/{language}, /movies/{id}/translations/{language}, etc.).
type TranslationRecord struct {
	Name     string `json:"name"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline"`
	Language string `json:"language"`
}

// ---------------------------------------------------------------------------
// API error response
// ---------------------------------------------------------------------------

type apiError struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}
