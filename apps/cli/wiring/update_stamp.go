package wiring

import (
	"os"
	"path/filepath"
	"time"

	"tofu/apps/cli/output"
	"tofu/internal/konst"
	models "tofu/internal/models/llm/catalog"
)

const (
	UpdateStampSuffix = ".update"
	UpdateFound       = "models update found "
	updateSucceeded   = "succeeded"
	updateFailed      = "failed"
)

func CatalogStale(now time.Time) bool {
	catalog, err := models.CatalogDir()
	if err != nil {
		return false
	}
	stamp := catalog + UpdateStampSuffix
	outcome, readErr := os.ReadFile(stamp)
	info, statErr := os.Stat(stamp)
	if readErr != nil || statErr != nil {
		return true
	}
	age := now.Sub(info.ModTime())
	switch string(outcome) {
	case updateSucceeded:
		return age >= konst.CatalogStaleHours*time.Hour
	case updateFailed:
		return age >= konst.CatalogRetryHours*time.Hour
	}
	return true
}

func StampUpdate(exit int) {
	catalog, err := models.CatalogDir()
	if err != nil || os.MkdirAll(filepath.Dir(catalog), 0o755) != nil {
		return
	}
	outcome := updateFailed
	if exit == output.ExitOK {
		outcome = updateSucceeded
	}
	_ = os.WriteFile(catalog+UpdateStampSuffix, []byte(outcome), 0o644)
}
