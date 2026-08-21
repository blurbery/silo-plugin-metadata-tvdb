package main

import (
	"context"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-tvdb/metadata"
	"github.com/Silo-Server/silo-plugin-tvdb/provider"
)

func TestResolveImageURL(t *testing.T) {
	ms := &metadataServer{}
	tests := []struct {
		name, path, variant, want string
	}{
		{"poster card", "banners/posters/81189-10.jpg", "card", "https://artworks.thetvdb.com/banners/posters/81189-10_t.jpg"},
		{"poster featured", "banners/posters/81189-10.jpg", "featured", "https://artworks.thetvdb.com/banners/posters/81189-10.jpg"},
		{"poster original", "banners/posters/81189-10.jpg", "original", "https://artworks.thetvdb.com/banners/posters/81189-10.jpg"},
		{"empty variant", "banners/posters/81189-10.jpg", "", "https://artworks.thetvdb.com/banners/posters/81189-10.jpg"},
		{"backdrop card", "banners/fanart/81189-5.jpg", "card", "https://artworks.thetvdb.com/banners/fanart/81189-5_t.jpg"},
		{"empty path", "", "card", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := ms.ResolveImageURL(context.Background(), &pluginv1.ResolveImageURLRequest{
				Path: tt.path, Variant: tt.variant,
			})
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if resp.GetUrl() != tt.want {
				t.Fatalf("got %q, want %q", resp.GetUrl(), tt.want)
			}
		})
	}
}

func TestImageRecordMetadataIncludesArtworkTextSignal(t *testing.T) {
	includesText := false
	md := imageRecordMetadata(metadata.RemoteImage{
		Rating:       8.5,
		IncludesText: &includesText,
	})
	if md == nil {
		t.Fatal("imageRecordMetadata() = nil")
	}
	if got := md.GetFields()["rating"].GetNumberValue(); got != 8.5 {
		t.Fatalf("rating = %v, want 8.5", got)
	}
	includesTextField, ok := md.GetFields()["includes_text"]
	if !ok {
		t.Fatal("includes_text metadata is missing")
	}
	if includesTextField.GetBoolValue() {
		t.Fatal("includes_text = true, want false")
	}
}

func TestImageRequestFromProtoPreservesSpecialsPresence(t *testing.T) {
	specials := int32(0)
	got := imageRequestFromProto(&pluginv1.GetImagesRequest{
		ProviderId:   "99",
		ItemType:     "series",
		Language:     "fr",
		SeasonNumber: &specials,
	}, "tvdb")
	if got.SeasonNumber == nil || *got.SeasonNumber != 0 {
		t.Fatalf("SeasonNumber = %v, want present Specials value 0", got.SeasonNumber)
	}
	if got.ProviderIDs["tvdb"] != "99" || got.ContentType != "series" || got.Language != "fr" {
		t.Fatalf("image request = %#v", got)
	}
	if absent := imageRequestFromProto(&pluginv1.GetImagesRequest{}, "tvdb"); absent.SeasonNumber != nil {
		t.Fatalf("absent season number mapped as %v", absent.SeasonNumber)
	}
}

func TestRuntimeServerConfigure_NoOp(t *testing.T) {
	server := &runtimeServer{provider: provider.NewProvider()}

	_, err := server.Configure(context.Background(), &pluginv1.ConfigureRequest{})
	if err != nil {
		t.Fatalf("Configure() returned error: %v", err)
	}

	p, err := server.providerForRequest()
	if err != nil {
		t.Fatalf("providerForRequest() returned error: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider to be available")
	}
}

func TestPersonDetailRecordFromResult_CanonicalizesPhotoPath(t *testing.T) {
	record, err := personDetailRecordFromResult(&metadata.PersonDetailResult{
		Name:           "Sigourney Weaver",
		SortName:       "Weaver, Sigourney",
		Bio:            "English biography",
		BirthDate:      "1949-10-08",
		Birthplace:     "New York City, New York, USA",
		PhotoPath:      "https://artworks.thetvdb.com/banners/persons/321.jpg",
		PhotoThumbhash: "thumbhash-123",
		ProviderIDs: map[string]string{
			"tvdb": "321",
			"imdb": "nm0000244",
		},
	})
	if err != nil {
		t.Fatalf("personDetailRecordFromResult() error = %v", err)
	}
	if record.GetPhotoPath() != "tvdb://banners/persons/321.jpg" {
		t.Fatalf("record.PhotoPath = %q, want tvdb canonical path", record.GetPhotoPath())
	}
	if record.GetPhotoThumbhash() != "thumbhash-123" {
		t.Fatalf("record.PhotoThumbhash = %q, want thumbhash-123", record.GetPhotoThumbhash())
	}
	if record.GetProviderIds().AsMap()["tvdb"] != "321" {
		t.Fatalf("record.ProviderIds[tvdb] = %#v", record.GetProviderIds().AsMap()["tvdb"])
	}
}
