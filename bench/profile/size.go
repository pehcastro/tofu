package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

type packageBytes struct {
	Package string `json:"package"`
	Bytes   int64  `json:"bytes"`
}

type sizeRun struct {
	Label         string         `json:"label"`
	Build         string         `json:"build"`
	StrippedBytes int64          `json:"stripped_bytes"`
	FullBytes     int64          `json:"full_bytes"`
	SymbolBytes   int64          `json:"symbol_bytes"`
	Packages      int            `json:"packages"`
	Top           []packageBytes `json:"top"`
	Machine       string         `json:"machine"`
	Date          string         `json:"date"`
}

var linkerTables = []string{"go:", "type:", "runtime.pclntab", "runtime.rodata", "runtime.findfunctab", "runtime.etypes"}

func packageOf(symbol string) string {
	if generic := strings.IndexByte(symbol, '['); generic >= 0 {
		symbol = symbol[:generic]
	}
	slash := strings.LastIndexByte(symbol, '/') + 1
	dot := strings.IndexByte(symbol[slash:], '.')
	if dot < 0 {
		return symbol
	}
	return symbol[:slash+dot]
}

func symbolBytesByPackage(binary string) (map[string]int64, error) {
	listing, err := exec.Command("go", "tool", "nm", "-size", binary).Output()
	if err != nil {
		return nil, fmt.Errorf("go tool nm: %w", err)
	}
	byPackage := map[string]int64{}
	for line := range strings.Lines(string(listing)) {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.ContainsAny(fields[2], "TtRrDd") {
			continue
		}
		name := strings.Join(fields[3:], " ")
		if slices.ContainsFunc(linkerTables, func(prefix string) bool { return strings.HasPrefix(name, prefix) }) {
			continue
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		byPackage[packageOf(name)] += size
	}
	return byPackage, nil
}

func sizes(args []string) error {
	flags := flag.NewFlagSet("size", flag.ExitOnError)
	label := flags.String("label", "", "the run's name, and its file under runs/ as size-<label>.json")
	stripped := flags.String("stripped", "", "the binary built with -trimpath -ldflags=-s -w")
	full := flags.String("full", "", "the same tree built with -trimpath and its symbols kept, read by go tool nm")
	build := flags.String("build", "", "what was built: the commit and anything changed on top of it")
	top := flags.Int("top", 15, "how many packages the table lists")
	_ = flags.Parse(args)
	if *label == "" || *stripped == "" || *full == "" || *build == "" {
		return errors.New("size needs -label, -stripped, -full and -build")
	}
	strippedInfo, err := os.Stat(*stripped)
	if err != nil {
		return err
	}
	fullInfo, err := os.Stat(*full)
	if err != nil {
		return err
	}
	byPackage, err := symbolBytesByPackage(*full)
	if err != nil {
		return err
	}
	r := sizeRun{Label: *label, Build: *build, StrippedBytes: strippedInfo.Size(), FullBytes: fullInfo.Size(), Packages: len(byPackage), Machine: machine(), Date: time.Now().Format("2006-01-02 15:04")}
	for name, size := range byPackage {
		r.SymbolBytes += size
		r.Top = append(r.Top, packageBytes{name, size})
	}
	slices.SortFunc(r.Top, func(a, b packageBytes) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), strings.Compare(a.Package, b.Package))
	})
	r.Top = r.Top[:min(*top, len(r.Top))]
	body, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(here(), "runs", "size-"+*label+".json"), append(body, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Print(r.table())
	return nil
}

func (r sizeRun) table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s, stripped %.2f MB (%d bytes), with symbols %.2f MB, %.2f MB of symbols in %d packages, %s, %s\n\n",
		r.Label, r.Build, float64(r.StrippedBytes)/mebibyte, r.StrippedBytes, float64(r.FullBytes)/mebibyte, float64(r.SymbolBytes)/mebibyte, r.Packages, r.Machine, r.Date)
	b.WriteString("| rank | package | KB | of the stripped file |\n|---|---|---|---|\n")
	for i, p := range r.Top {
		fmt.Fprintf(&b, "| %d | %s | %.0f | %.1f%% |\n", i+1, p.Package, float64(p.Bytes)/1024, 100*float64(p.Bytes)/float64(r.StrippedBytes))
	}
	return b.String()
}

func machine() string {
	return fmt.Sprintf("%s/%s, %d logical CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
}
