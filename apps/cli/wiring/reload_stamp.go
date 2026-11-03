package wiring

import (
	"os"
	"path/filepath"
	"time"

	"tofu/apps/cli/output"
	"tofu/internal/konst"
	"tofu/internal/models/llm/catalog"
)

const (
	ReloadStampSuffix = ".reload"
	ReloadFound       = "models reload found "
	reloadSucceeded   = "succeeded"
	reloadFailed      = "failed"
)

func CatalogStale(now time.Time) bool {
	catalog, err := models.CatalogDir()
	if err != nil {
		return false
	}
	stamp := catalog + ReloadStampSuffix
	outcome, readErr := os.ReadFile(stamp)
	info, statErr := os.Stat(stamp)
	if readErr != nil || statErr != nil {
		return true
	}
	age := now.Sub(info.ModTime())
	switch string(outcome) {
	case reloadSucceeded:
		return age >= konst.CatalogStaleHours*time.Hour
	case reloadFailed:
		return age >= konst.CatalogRetryHours*time.Hour
	}
	return true
}

func StampReload(exit int) {
	catalog, err := models.CatalogDir()
	if err != nil || os.MkdirAll(filepath.Dir(catalog), 0o755) != nil {
		return
	}
	outcome := reloadFailed
	if exit == output.ExitOK {
		outcome = reloadSucceeded
	}
	_ = os.WriteFile(catalog+ReloadStampSuffix, []byte(outcome), 0o644)
}
