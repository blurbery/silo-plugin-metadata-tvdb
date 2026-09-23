package main

import "testing"

func TestManifestDeclaresMetadataProxySetting(t *testing.T) {
	m, err := loadManifest()
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	var found bool
	for _, s := range m.GetGlobalConfigSchema() {
		if s.GetKey() == "metadata_proxy" {
			found = true
			if s.GetAdminForm() == nil || len(s.GetAdminForm().GetFields()) != 2 {
				t.Fatalf("metadata_proxy admin form malformed: %v", s.GetAdminForm())
			}
		}
	}
	if !found {
		t.Fatal("manifest missing metadata_proxy global config schema")
	}
}
