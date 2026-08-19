package metadata

import (
	"strings"

	"github.com/Silo-Server/silo-plugin-tvdb/models"
)

// SearchQuery is passed to SearchProvider.Search().
type SearchQuery struct {
	Title       string
	Year        int
	ContentType string
	ProviderIDs map[string]string
	Language    string // ISO 639-1 code from library preference
}

// SearchResult is returned from SearchProvider.Search().
type SearchResult struct {
	Name             string
	OriginalTitle    string
	TitleAliases     []TitleAlias
	TitleLanguage    string
	TitleIsFallback  bool
	OriginalLanguage string
	Year             int
	ProviderIDs      map[string]string
	ImageURL         string
	Overview         string
	Provider         string
}

// TitleAlias is a provider-confirmed title for the same work.
type TitleAlias struct {
	Title    string
	Language string
	Kind     string
}

// MetadataRequest is passed to MetadataProvider.GetMetadata().
type MetadataRequest struct {
	ProviderIDs map[string]string
	ContentType string
	Language    string
	FilePath    string
}

// MetadataResult carries structured metadata from a single provider.
type MetadataResult struct {
	HasMetadata          bool
	ProviderIDs          map[string]string
	Title                string
	OriginalTitle        string
	SortTitle            string
	Overview             string
	Tagline              string
	Year                 int
	Runtime              int
	Genres               []string
	Studios              []string
	Networks             []string
	Countries            []string
	OriginalLanguage     string
	TitleAliases         []TitleAlias
	TitleAliasesComplete bool
	TitleLanguage        string
	TitleIsFallback      bool
	ContentRating        string
	Ratings              Ratings
	People               []models.ItemPerson
	PosterPath           string
	PosterThumbhash      string
	BackdropPath         string
	BackdropThumbhash    string
	LogoPath             string
	SeasonCount          int
	FirstAirDate         string
	LastAirDate          string
	AirTime              string
	// ShowStatus is the TVDB series lifecycle status verbatim ("Continuing",
	// "Ended", "Upcoming"); the host normalizes spellings.
	ShowStatus string
}

// PersonDetailRequest is passed to person detail lookups.
type PersonDetailRequest struct {
	ProviderIDs map[string]string
	Language    string
}

// PersonDetailResult carries person-level metadata from a provider.
type PersonDetailResult struct {
	Name           string
	SortName       string
	Bio            string
	BirthDate      string
	DeathDate      string
	Birthplace     string
	Homepage       string
	PhotoPath      string
	PhotoThumbhash string
	ProviderIDs    map[string]string
}

// Ratings holds ratings from multiple sources.
type Ratings struct {
	IMDB       float64
	TMDB       float64
	RTCritic   float64
	RTAudience float64
}

// ImageRequest is passed to ImageProvider.GetImages().
type ImageRequest struct {
	ProviderIDs map[string]string
	ContentType string
	Language    string
}

// RemoteImage describes an available image from a provider.
type RemoteImage struct {
	URL          string
	Type         ImageType
	Language     string
	Width        int
	Height       int
	Rating       float64
	IncludesText *bool
}

// ImageType classifies image purpose.
type ImageType int

const (
	ImagePoster ImageType = iota
	ImageBackdrop
	ImageLogo
	ImageStill
)

// SeasonsRequest is passed to EpisodeProvider.GetSeasons().
type SeasonsRequest struct {
	ProviderIDs map[string]string
	ContentType string
	Language    string
}

// EpisodesRequest is passed to EpisodeProvider.GetEpisodes().
type EpisodesRequest struct {
	ProviderIDs  map[string]string
	SeasonNumber int
	Language     string
}

// SeasonResult carries season data from a provider.
type SeasonResult struct {
	ContentID    string
	SeasonNumber int
	Title        string
	Overview     string
	AirDate      string
	PosterPath   string
	Episodes     []EpisodeResult
}

// EpisodeResult carries episode data from a provider.
type EpisodeResult struct {
	ContentID     string
	ProviderIDs   map[string]string
	SeasonNumber  int
	EpisodeNumber int
	Title         string
	Overview      string
	AirDate       string
	Runtime       int
	Ratings       Ratings
	StillPath     string
}

// NormalizeOriginalLanguage lowercases and trims whitespace from a language code.
func NormalizeOriginalLanguage(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
