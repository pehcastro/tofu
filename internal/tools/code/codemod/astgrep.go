package codemod

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
)

type astGrepMatch struct {
	File        string  `json:"file"`
	Text        string  `json:"text"`
	Replacement *string `json:"replacement"`
	Range       struct {
		ByteOffset struct{ Start, End int } `json:"byteOffset"`
		Start      struct{ Line int }       `json:"start"`
	} `json:"range"`
	ReplacementOffsets *struct{ Start, End int } `json:"replacementOffsets"`
}

func astGrepSites(ctx context.Context, binary, root string, args arguments, paths []string) ([]fileSites, error) {
	command := exec.CommandContext(ctx, binary, append([]string{"run", "--pattern", args.Pattern, "--rewrite", args.Rewrite, "--lang", args.Lang, "--json=stream", "--color", "never"}, paths...)...)
	var stdout, stderr bytes.Buffer
	command.Dir, command.Stdout, command.Stderr, command.WaitDelay = root, &stdout, &stderr, konst.CodemodRunMillis*time.Millisecond
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && stdout.Len() == 0 && strings.TrimSpace(stderr.String()) == "":
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("ast-grep run failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var found []fileSites
	index := map[string]int{}
	lines := bufio.NewScanner(&stdout)
	lines.Buffer(nil, stdout.Len()+1)
	for lines.Scan() {
		var match astGrepMatch
		if err := json.Unmarshal(lines.Bytes(), &match); err != nil {
			return nil, fmt.Errorf("ast-grep printed a line that is not a match: %w", err)
		}
		if match.Replacement == nil {
			return nil, fmt.Errorf("ast-grep gave no replacement for the match at %s:%d", match.File, match.Range.Start.Line+1)
		}
		offsets := match.Range.ByteOffset
		if match.ReplacementOffsets != nil {
			offsets = *match.ReplacementOffsets
		}
		path := match.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		at, seen := index[path]
		if !seen {
			at, index[path] = len(found), len(found)
			found = append(found, fileSites{path: path})
		}
		found[at].sites = append(found[at].sites, site{start: offsets.Start, end: offsets.End, line: match.Range.Start.Line + 1, before: match.Text, after: *match.Replacement})
	}
	for _, file := range found {
		slices.SortFunc(file.sites, func(a, b site) int { return a.start - b.start })
	}
	return found, lines.Err()
}
