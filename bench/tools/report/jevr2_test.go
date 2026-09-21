package report

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tofutools "tofu/internal/turn/tools"
)

func TestSearchOverTheLargestTreeOnThisMachine(t *testing.T) {
	if os.Getenv("TOFU_TOOLS_LARGE_TREES") == "" {
		t.Skip("this reads 14 thousand files off .local/sources and takes over a minute: set TOFU_TOOLS_LARGE_TREES=1")
	}
	root := filepath.Join(tofuRoot, ".local", "sources", "jev-r2")
	if _, err := os.Stat(root); err != nil {
		t.Skip("this measurement needs .local/sources/jev-r2 and it is not on this machine")
	}
	searchTool, err := tofutools.NewSearch(root)
	if err != nil {
		t.Fatalf("building search: %v", err)
	}
	for _, pattern := range []string{"TODO|FIXME|XXX|HACK", "TODO|FIXME|XXX|HACK", "func NewClient", "func NewClient"} {
		args, err := json.Marshal(map[string]string{"pattern": pattern})
		if err != nil {
			t.Fatalf("encoding the call: %v", err)
		}
		started := time.Now()
		result, err := searchTool.Run(context.Background(), json.RawMessage(args))
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("search over jev-r2: %v", err)
		}
		counts := strings.Split(result.Content, "\n")[1]
		t.Logf("search %s over jev-r2: %s, %d bytes returned, %s", pattern, elapsed.Round(time.Millisecond), len(result.Content), counts)
	}
}
