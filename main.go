package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/Silo-Server/silo-plugin-tvdb/metadata"
	"github.com/Silo-Server/silo-plugin-tvdb/models"
	"github.com/Silo-Server/silo-plugin-tvdb/provider"
)

// version is set at build time via -ldflags "-X main.version=...".
var version string

const tvdbArtworksBase = "https://artworks.thetvdb.com/"

func tvdbCanonicalPath(imageURL string) string {
	if imageURL == "" {
		return ""
	}
	path := strings.TrimPrefix(imageURL, tvdbArtworksBase)
	return "tvdb://" + path
}

func resolveOneTVDBPath(barePath, variant string) string {
	if barePath == "" {
		return ""
	}
	fullURL := tvdbArtworksBase + barePath
	if variant == "card" {
		return tvdbThumbnailURL(fullURL)
	}
	return fullURL
}

func tvdbThumbnailURL(imageURL string) string {
	dotIdx := strings.LastIndex(imageURL, ".")
	if dotIdx <= 0 {
		return imageURL
	}
	return imageURL[:dotIdx] + "_t" + imageURL[dotIdx:]
}

type runtimeServer struct {
	pluginv1.UnimplementedRuntimeServer

	manifest *pluginv1.PluginManifest
	provider *provider.Provider
}

type metadataServer struct {
	pluginv1.UnimplementedMetadataProviderServer
	pluginv1.UnimplementedImageResolverServer
	runtime *runtimeServer
}

//go:embed manifest.json
var manifestJSON []byte

func (s *runtimeServer) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *runtimeServer) Configure(_ context.Context, _ *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *runtimeServer) providerForRequest() (*provider.Provider, error) {
	return s.provider, nil
}

func (s *metadataServer) Search(ctx context.Context, req *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.Search(ctx, metadata.SearchQuery{
		Title:       req.GetQuery(),
		Year:        int(req.GetYear()),
		ContentType: req.GetItemType(),
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
		Language:    req.GetLanguage(),
	})
	if err != nil {
		return nil, err
	}

	response := &pluginv1.SearchMetadataResponse{
		Results: make([]*pluginv1.ProviderSearchResult, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(result.ProviderIDs)
		if err != nil {
			return nil, err
		}
		response.Results = append(response.Results, &pluginv1.ProviderSearchResult{
			ProviderId:       result.ProviderIDs["tvdb"],
			ItemType:         req.GetItemType(),
			Title:            result.Name,
			Year:             int32(result.Year),
			Overview:         result.Overview,
			ProviderIds:      providerIDs,
			ImageUrl:         tvdbCanonicalPath(result.ImageURL),
			OriginalTitle:    result.OriginalTitle,
			TitleAliases:     aliasesToProto(result.TitleAliases),
			TitleLanguage:    result.TitleLanguage,
			TitleIsFallback:  result.TitleIsFallback,
			OriginalLanguage: result.OriginalLanguage,
		})
	}
	return response, nil
}

func (s *metadataServer) GetMetadata(ctx context.Context, req *pluginv1.GetMetadataRequest) (*pluginv1.GetMetadataResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	result, err := p.GetMetadata(ctx, metadataRequestFromProto(req, "tvdb"))
	if err != nil || result == nil {
		return nil, err
	}

	item, err := metadataItemFromResult(result, req.GetItemType())
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetMetadataResponse{Item: item}, nil
}

func (s *metadataServer) GetPersonDetail(ctx context.Context, req *pluginv1.GetPersonDetailRequest) (*pluginv1.GetPersonDetailResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	result, err := p.GetPersonDetail(ctx, personDetailRequestFromProto(req))
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &pluginv1.GetPersonDetailResponse{}, nil
	}

	person, err := personDetailRecordFromResult(result)
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetPersonDetailResponse{Person: person}, nil
}

func (s *metadataServer) GetSeasons(ctx context.Context, req *pluginv1.GetSeasonsRequest) (*pluginv1.GetSeasonsResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.GetSeasons(ctx, seasonsRequestFromProto(req, "tvdb"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetSeasonsResponse{
		Seasons: make([]*pluginv1.SeasonRecord, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(map[string]string{"tvdb": result.ContentID})
		if err != nil {
			return nil, err
		}
		response.Seasons = append(response.Seasons, &pluginv1.SeasonRecord{
			ProviderId:   result.ContentID,
			ProviderIds:  providerIDs,
			SeasonNumber: int32(result.SeasonNumber),
			Title:        result.Title,
			Overview:     result.Overview,
			AirDate:      result.AirDate,
			PosterPath:   tvdbCanonicalPath(result.PosterPath),
		})
	}
	return response, nil
}

func (s *metadataServer) GetEpisodes(ctx context.Context, req *pluginv1.GetEpisodesRequest) (*pluginv1.GetEpisodesResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.GetEpisodes(ctx, episodesRequestFromProto(req, "tvdb"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetEpisodesResponse{
		Episodes: make([]*pluginv1.EpisodeRecord, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(result.ProviderIDs)
		if err != nil {
			return nil, err
		}
		response.Episodes = append(response.Episodes, &pluginv1.EpisodeRecord{
			ProviderId:    result.ContentID,
			SeasonNumber:  int32(result.SeasonNumber),
			EpisodeNumber: int32(result.EpisodeNumber),
			Title:         result.Title,
			Overview:      result.Overview,
			AirDate:       result.AirDate,
			Runtime:       int32(result.Runtime),
			StillPath:     tvdbCanonicalPath(result.StillPath),
			ProviderIds:   providerIDs,
			Ratings:       ratingsStruct(result.Ratings),
		})
	}
	return response, nil
}

func (s *metadataServer) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	images, err := p.GetImages(ctx, imageRequestFromProto(req, "tvdb"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetImagesResponse{}
	for _, img := range images {
		kind := ""
		switch img.Type {
		case metadata.ImagePoster:
			kind = "poster"
		case metadata.ImageBackdrop:
			kind = "backdrop"
		case metadata.ImageLogo:
			kind = "logo"
		}
		response.Images = append(response.Images, &pluginv1.ImageRecord{
			Kind:     kind,
			Url:      tvdbCanonicalPath(img.URL),
			Language: img.Language,
			Width:    int32(img.Width),
			Height:   int32(img.Height),
			Metadata: imageRecordMetadata(img),
		})
	}
	return response, nil
}

func imageRecordMetadata(img metadata.RemoteImage) *structpb.Struct {
	fields := make(map[string]interface{}, 2)
	if img.Rating > 0 {
		fields["rating"] = img.Rating
	}
	if img.IncludesText != nil {
		fields["includes_text"] = *img.IncludesText
	}
	if len(fields) == 0 {
		return nil
	}
	result, _ := structpb.NewStruct(fields)
	return result
}

func (s *metadataServer) ResolveImageURL(_ context.Context, req *pluginv1.ResolveImageURLRequest) (*pluginv1.ResolveImageURLResponse, error) {
	url := resolveOneTVDBPath(req.GetPath(), req.GetVariant())
	return &pluginv1.ResolveImageURLResponse{Url: url}, nil
}

func (s *metadataServer) ResolveImageURLs(_ context.Context, req *pluginv1.ResolveImageURLsRequest) (*pluginv1.ResolveImageURLsResponse, error) {
	urls := make(map[string]string, len(req.GetPaths()))
	for _, path := range req.GetPaths() {
		urls[path] = resolveOneTVDBPath(path, req.GetVariant())
	}
	return &pluginv1.ResolveImageURLsResponse{Urls: urls}, nil
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}

	rs := &runtimeServer{
		manifest: manifest,
		provider: provider.NewProvider(),
	}

	ms := &metadataServer{runtime: rs}
	runtime.Serve(runtime.ServeConfig{
		Servers: runtime.CapabilityServers{
			Runtime:          rs,
			MetadataProvider: ms,
			ImageResolver:    ms,
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}

	if version != "" {
		manifest.Version = version
	}

	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])

	return manifest, nil
}

func metadataItemFromResult(result *metadata.MetadataResult, itemType string) (*pluginv1.MetadataItem, error) {
	providerIDs, err := stringStruct(result.ProviderIDs)
	if err != nil {
		return nil, err
	}

	return &pluginv1.MetadataItem{
		ProviderId:           result.ProviderIDs["tvdb"],
		ItemType:             itemType,
		Title:                result.Title,
		OriginalTitle:        result.OriginalTitle,
		SortTitle:            result.SortTitle,
		Year:                 int32(result.Year),
		Overview:             result.Overview,
		Tagline:              result.Tagline,
		Runtime:              int32(result.Runtime),
		Genres:               append([]string(nil), result.Genres...),
		Studios:              append([]string(nil), result.Studios...),
		Networks:             append([]string(nil), result.Networks...),
		Countries:            append([]string(nil), result.Countries...),
		OriginalLanguage:     result.OriginalLanguage,
		ContentRating:        result.ContentRating,
		ProviderIds:          providerIDs,
		Ratings:              ratingsStruct(result.Ratings),
		PosterPath:           tvdbCanonicalPath(result.PosterPath),
		PosterThumbhash:      result.PosterThumbhash,
		BackdropPath:         tvdbCanonicalPath(result.BackdropPath),
		BackdropThumbhash:    result.BackdropThumbhash,
		LogoPath:             tvdbCanonicalPath(result.LogoPath),
		SeasonCount:          int32(result.SeasonCount),
		FirstAirDate:         result.FirstAirDate,
		LastAirDate:          result.LastAirDate,
		AirTime:              result.AirTime,
		Status:               result.ShowStatus,
		People:               peopleToRecords(result.People),
		TitleAliases:         aliasesToProto(result.TitleAliases),
		TitleAliasesComplete: result.TitleAliasesComplete,
		TitleLanguage:        result.TitleLanguage,
		TitleIsFallback:      result.TitleIsFallback,
	}, nil
}

func aliasesToProto(aliases []metadata.TitleAlias) []*pluginv1.TitleAlias {
	out := make([]*pluginv1.TitleAlias, 0, len(aliases))
	for _, alias := range aliases {
		if strings.TrimSpace(alias.Title) == "" {
			continue
		}
		out = append(out, &pluginv1.TitleAlias{Title: alias.Title, Language: alias.Language, Kind: alias.Kind})
	}
	return out
}

func personDetailRecordFromResult(result *metadata.PersonDetailResult) (*pluginv1.PersonDetailRecord, error) {
	providerIDs, err := stringStruct(result.ProviderIDs)
	if err != nil {
		return nil, err
	}

	return &pluginv1.PersonDetailRecord{
		Name:           result.Name,
		SortName:       result.SortName,
		Bio:            result.Bio,
		BirthDate:      result.BirthDate,
		DeathDate:      result.DeathDate,
		Birthplace:     result.Birthplace,
		Homepage:       result.Homepage,
		PhotoPath:      tvdbCanonicalPath(result.PhotoPath),
		PhotoThumbhash: result.PhotoThumbhash,
		ProviderIds:    providerIDs,
	}, nil
}

func peopleToRecords(people []models.ItemPerson) []*pluginv1.PersonRecord {
	if len(people) == 0 {
		return nil
	}

	records := make([]*pluginv1.PersonRecord, 0, len(people))
	for _, person := range people {
		records = append(records, &pluginv1.PersonRecord{
			Name:           person.Name,
			Kind:           person.Kind.String(),
			Character:      person.Character,
			SortOrder:      int32(person.SortOrder),
			TmdbId:         person.TmdbID,
			TvdbId:         person.TvdbID,
			ImdbId:         person.ImdbID,
			PlexGuid:       person.PlexGUID,
			PhotoPath:      person.PhotoPath,
			PhotoThumbhash: person.PhotoThumbhash,
		})
	}
	return records
}

func stringMapFromStruct(value *structpb.Struct) map[string]string {
	result := make(map[string]string)
	if value == nil {
		return result
	}
	for key, raw := range value.AsMap() {
		text, ok := raw.(string)
		if ok && text != "" {
			result[key] = text
		}
	}
	return result
}

func providerIDsFromProto(value *structpb.Struct, capabilityID string, fallbackID string) map[string]string {
	result := stringMapFromStruct(value)
	if fallbackID != "" && result[capabilityID] == "" {
		result[capabilityID] = fallbackID
	}
	return result
}

func metadataRequestFromProto(req *pluginv1.GetMetadataRequest, capabilityID string) metadata.MetadataRequest {
	return metadata.MetadataRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetProviderId()),
		ContentType: req.GetItemType(),
		Language:    req.GetLanguage(),
		FilePath:    req.GetFilePath(),
	}
}

func personDetailRequestFromProto(req *pluginv1.GetPersonDetailRequest) metadata.PersonDetailRequest {
	return metadata.PersonDetailRequest{
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
		Language:    req.GetLanguage(),
	}
}

func seasonsRequestFromProto(req *pluginv1.GetSeasonsRequest, capabilityID string) metadata.SeasonsRequest {
	return metadata.SeasonsRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetSeriesProviderId()),
		ContentType: "series",
		Language:    req.GetLanguage(),
	}
}

func episodesRequestFromProto(req *pluginv1.GetEpisodesRequest, capabilityID string) metadata.EpisodesRequest {
	return metadata.EpisodesRequest{
		ProviderIDs:  providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetSeriesProviderId()),
		SeasonNumber: int(req.GetSeasonNumber()),
		Language:     req.GetLanguage(),
	}
}

func imageRequestFromProto(req *pluginv1.GetImagesRequest, capabilityID string) metadata.ImageRequest {
	return metadata.ImageRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetProviderId()),
		ContentType: req.GetItemType(),
		Language:    req.GetLanguage(),
	}
}

func stringStruct(value map[string]string) (*structpb.Struct, error) {
	if len(value) == 0 {
		return nil, nil
	}

	converted := make(map[string]any, len(value))
	for key, entry := range value {
		if entry == "" {
			continue
		}
		converted[key] = entry
	}
	if len(converted) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(converted)
}

func structFromMap(value map[string]any) *structpb.Struct {
	if len(value) == 0 {
		return nil
	}
	result, err := structpb.NewStruct(value)
	if err != nil {
		panic(err)
	}
	return result
}

func ratingsStruct(ratings metadata.Ratings) *structpb.Struct {
	return structFromMap(ratingsMap(ratings))
}

func ratingsMap(ratings metadata.Ratings) map[string]any {
	result := make(map[string]any)
	if ratings.IMDB != 0 {
		result["imdb"] = ratings.IMDB
	}
	if ratings.TMDB != 0 {
		result["tmdb"] = ratings.TMDB
	}
	if ratings.RTCritic != 0 {
		result["rt_critic"] = ratings.RTCritic
	}
	if ratings.RTAudience != 0 {
		result["rt_audience"] = ratings.RTAudience
	}
	return result
}
