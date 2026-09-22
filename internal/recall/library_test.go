package recall_test

import (
	"testing"

	"tofu/internal/recall"
)

func TestLoadConfigReadsTheShippedLibraryFile(t *testing.T) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.ElideAboveBytes != 4096 {
		t.Fatalf("elide_above_bytes = %d, want 4096 from data/elide.yaml", cfg.ElideAboveBytes)
	}
}

func TestParseConfigTracksTheDataRatherThanALiteral(t *testing.T) {
	fixture := []byte("elide_above_bytes: 999\nhead_bytes: 7\ntail_bytes: 3\n")

	cfg, err := recall.ParseConfig(fixture)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.ElideAboveBytes == 4096 {
		t.Fatal("changing the library data did not change the threshold: a literal is standing in for it")
	}
	if cfg.ElideAboveBytes != 999 || cfg.HeadBytes != 7 || cfg.TailBytes != 3 {
		t.Fatalf("config = %+v, want the fixture's own numbers", cfg)
	}
}

func TestParseConfigRejectsAMissingThreshold(t *testing.T) {
	if _, err := recall.ParseConfig([]byte("head_bytes: 7\n")); err == nil {
		t.Fatal("a library file with no elide_above_bytes was accepted")
	}
}
