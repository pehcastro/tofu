package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type trackFiles struct {
	key, rows, origins string
}

func tracked(env Env, ev event) (string, trackFiles) {
	session := orDefault(ev.SessionID, "nosession")
	key := orDefault(ev.AgentID, session)
	return session, trackAt(env, key)
}

func trackAt(env Env, key string) trackFiles {
	return trackFiles{
		key:     key,
		rows:    filepath.Join(env.Temp, "boji-loc-"+key+".txt"),
		origins: filepath.Join(env.Temp, "boji-loc-"+key+"-orig.txt"),
	}
}

func locTrack(env Env, ev event) error {
	path := ev.ToolInput.FilePath
	if blank(path) || !foldSuffix(path, ".go") {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_, track := tracked(env, ev)
	seen := slices.ContainsFunc(fileLines(track.rows), func(line string) bool { return strings.HasPrefix(line, path+"\t") })
	written := 0
	for _, line := range fileLines(path) {
		if line != "" {
			written++
		}
	}
	if err := appendLine(track.rows, fmt.Sprintf("%s\t%d\tnative\t%s", path, written, ev.AgentID)); err != nil {
		return err
	}
	if seen || !strings.EqualFold(ev.ToolName, "Edit") {
		return nil
	}
	before, undone := undoEdit(string(data), ev.ToolInput.OldString, ev.ToolInput.NewString, ev.ToolInput.ReplaceAll)
	if !undone {
		return nil
	}
	return appendLine(track.origins, path+"\t"+contentHash([]byte(before)))
}

func undoEdit(text, old, replacement string, all bool) (string, bool) {
	if replacement == "" {
		return "", false
	}
	if all {
		return strings.ReplaceAll(text, replacement, old), true
	}
	at := strings.Index(text, replacement)
	if at < 0 {
		return "", false
	}
	return text[:at] + old + text[at+len(replacement):], true
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type trackRow struct {
	lines, tag, writer string
}

func minifyGate(env Env, ev event) (Answer, error) {
	if ev.StopHookActive {
		return Answer{}, nil
	}
	session, track := tracked(env, ev)
	if _, err := os.Stat(track.rows); err != nil {
		return Answer{}, nil
	}
	rows := map[string]trackRow{}
	for _, line := range fileLines(track.rows) {
		if parts := strings.Split(line, "\t"); len(parts) == 4 {
			rows[parts[0]] = trackRow{lines: parts[1], tag: parts[2], writer: parts[3]}
		}
	}
	origins := map[string]string{}
	for _, line := range fileLines(track.origins) {
		if parts := strings.Split(line, "\t"); len(parts) == 2 && origins[parts[0]] == "" {
			origins[parts[0]] = parts[1]
		}
	}
	_ = os.Remove(track.rows)
	_ = os.Remove(track.origins)

	subagent := strings.EqualFold(ev.HookEventName, "SubagentStop")
	rootPrefix := strings.TrimRight(env.Root, `\/`) + string(os.PathSeparator)
	for path, row := range rows {
		data, err := os.ReadFile(path)
		reverted := origins[path] != "" && contentHash(data) == origins[path]
		strayAtStop := !subagent && row.tag == "native" && !blank(row.writer)
		if err != nil || !foldPrefix(path, rootPrefix) || reverted || strayAtStop {
			delete(rows, path)
		}
	}
	if err := dropOwned(env, rows, origins, session, track.key, subagent); err != nil {
		return Answer{}, err
	}
	if len(rows) == 0 {
		return Answer{}, nil
	}
	paths := make([]string, 0, len(rows))
	for path := range rows {
		paths = append(paths, path)
	}
	slices.SortFunc(paths, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	total := 0
	var list []string
	for _, path := range paths {
		count, _ := strconv.Atoi(rows[path].lines)
		total += count
		list = append(list, fmt.Sprintf("  %6d  %s", count, path))
	}
	return refuse(2, fmt.Sprintf(`Before you finish: run the minify skill over every Go file you wrote.

%s
  ------
  %d lines total

Read each file whole, walk the checklist, apply the fixes, and state the count before and after per file. Less code that says the same thing. A line removed that took meaning with it is a failure, and so is a clever line that takes two readings.

A file that needed nothing says so.

This gate fires once. After the pass, finish and report as normal.`, strings.Join(list, "\n"), total)), nil
}

func dropOwned(env Env, rows map[string]trackRow, origins map[string]string, session, key string, subagent bool) error {
	if len(rows) == 0 {
		return nil
	}
	owning, err := ticketsAt(env).list(placeOpen, placeDoing, placeReview)
	var globs, doingGlobs []string
	for _, t := range owning {
		globs = append(globs, t.Owns...)
		if t.Place == placeDoing {
			doingGlobs = append(doingGlobs, t.Owns...)
		}
	}
	if len(globs) == 0 {
		return err
	}
	parent := trackAt(env, session)
	for path, row := range rows {
		rel := relativePath(env.Root, path)
		switch {
		case !subagent:
			if ownsMatch(rel, globs) {
				delete(rows, path)
			}
		case !ownsMatch(rel, globs):
			err = errors.Join(err, appendLine(parent.rows, fmt.Sprintf("%s\t%s\tforwarded\t%s", path, row.lines, key)))
			if origins[path] != "" {
				err = errors.Join(err, appendLine(parent.origins, path+"\t"+origins[path]))
			}
			delete(rows, path)
		case !ownsMatch(rel, doingGlobs):
			delete(rows, path)
		}
	}
	return err
}
