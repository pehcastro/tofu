package startup

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"tofu/internal/turn"
)

const packageJSONForNodeProbe = `{"name":"tofu-startup-bench","version":"0.0.0"}`

type Stats struct {
	Samples []time.Duration
	Median  time.Duration
	Worst   time.Duration
}

func stats(samples []time.Duration) Stats {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Stats{Samples: sorted, Median: sorted[len(sorted)/2], Worst: sorted[len(sorted)-1]}
}

type Rounds struct {
	Shell     Stats
	GoProbe   Stats
	NodeProbe Stats
	NodeCold  Stats
	NodeWarm  Stats
}

func Measure(runs int) (Rounds, error) {
	root, err := repoRoot()
	if err != nil {
		return Rounds{}, err
	}
	nodeDir, err := nodeProjectDir()
	if err != nil {
		return Rounds{}, err
	}
	defer func() { _ = os.RemoveAll(nodeDir) }()
	baselineDir, err := os.MkdirTemp("", "tofu-startup-shell-*")
	if err != nil {
		return Rounds{}, err
	}
	defer func() { _ = os.RemoveAll(baselineDir) }()

	var shellSamples, goDeltas, nodeDeltas, coldSamples, warmSamples []time.Duration
	for i := 0; i < runs; i++ {
		before, err := timeBashTool(baselineDir)
		if err != nil {
			return Rounds{}, err
		}
		goTotal, err := timeBashTool(root)
		if err != nil {
			return Rounds{}, err
		}
		after, err := timeBashTool(baselineDir)
		if err != nil {
			return Rounds{}, err
		}
		nodeTotal, err := timeBashTool(nodeDir)
		if err != nil {
			return Rounds{}, err
		}
		cold, warm, err := timeToolchainCacheHit(nodeDir)
		if err != nil {
			return Rounds{}, err
		}

		shellSamples = append(shellSamples, before, after)
		goDeltas = append(goDeltas, subtractFloor(goTotal, before))
		nodeDeltas = append(nodeDeltas, subtractFloor(nodeTotal, after))
		coldSamples = append(coldSamples, cold)
		warmSamples = append(warmSamples, warm)
	}

	return Rounds{
		Shell:     stats(shellSamples),
		GoProbe:   stats(goDeltas),
		NodeProbe: stats(nodeDeltas),
		NodeCold:  stats(coldSamples),
		NodeWarm:  stats(warmSamples),
	}, nil
}

func timeToolchainCacheHit(dir string) (cold, warm time.Duration, err error) {
	shell, err := turn.ResolveRunShell("")
	if err != nil {
		return 0, 0, err
	}
	coldStart := time.Now()
	turn.EnvironmentFromShell(dir, coldStart, shell)
	cold = time.Since(coldStart)

	warmStart := time.Now()
	turn.EnvironmentFromShell(dir, warmStart, shell)
	warm = time.Since(warmStart)
	return cold, warm, nil
}

func subtractFloor(total, baseline time.Duration) time.Duration {
	if total <= baseline {
		return 0
	}
	return total - baseline
}

func timeBashTool(root string) (time.Duration, error) {
	started := time.Now()
	if _, err := turn.NewBashTool(root); err != nil {
		return 0, err
	}
	return time.Since(started), nil
}

func nodeProjectDir() (string, error) {
	dir, err := os.MkdirTemp("", "tofu-startup-node-*")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJSONForNodeProbe), 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("startup: runtime.Caller could not resolve this file's own path")
	}
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
}
