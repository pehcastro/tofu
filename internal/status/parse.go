package status

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Refusal string

func (r Refusal) Error() string { return string(r) }

const (
	Query     Refusal = "the report is the feature detection query"
	NoState   Refusal = "the report names no state this build knows"
	OverLimit Refusal = "the report breaks a size limit"
	BadText   Refusal = "a title or msg is not base64 of printable utf-8"
	BadID     Refusal = "the id is not a path of valid segments"
)

func Parse(body string) (Record, error) {
	if len(introducer)+len(body)+len(terminator) > maxSequence {
		return Record{}, OverLimit
	}
	if body == "?" {
		return Record{}, Query
	}
	values := map[string]string{}
	for _, pair := range strings.Split(body, ":") {
		key, value, found := strings.Cut(pair, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found || key == "" || strings.TrimLeft(key, "abcdefghijklmnopqrstuvwxyz") != "" {
			continue
		}
		if len(key) > maxKey {
			return Record{}, OverLimit
		}
		if key == "id" || strings.Trim(value, valueBytes) == "" {
			values[key] = value
		}
	}
	record := Record{State: State(values["state"])}
	switch record.State {
	case Idle, Working, Done, Blocked, Errored, Clear:
	default:
		return Record{}, NoState
	}
	if id, named := values["id"]; named {
		if !validID(id) {
			return Record{}, BadID
		}
		record.ID = id
	}
	app := values["app"]
	if len(app) > maxApp {
		return Record{}, OverLimit
	}
	if app != "" && segmentOf(app) == app {
		record.App = app
	}
	switch kind := Kind(values["kind"]); kind {
	case Permission, Question, Auth:
		if record.State == Blocked {
			record.Kind = kind
		}
	}
	if progress, err := strconv.Atoi(values["progress"]); err == nil && strings.Trim(values["progress"], "0123456789") == "" && progress <= maxProgress && (record.State == Working || record.State == Blocked) {
		record.Progress = &progress
	}
	var err error
	if record.Title, err = decoded(values["title"], maxTitleCoded, maxTitle); err != nil {
		return Record{}, err
	}
	if record.Msg, err = decoded(values["msg"], maxMsgEncoded, maxMsg); err != nil {
		return Record{}, err
	}
	return record, nil
}

func validID(id string) bool {
	segments := strings.Split(id, "/")
	if len(id) > maxID || len(segments) > maxDepth {
		return false
	}
	for _, segment := range segments {
		if segment == "" || segmentOf(segment) != segment {
			return false
		}
	}
	return true
}

func decoded(encoded string, encodedLimit, limit int) (string, error) {
	if len(encoded) > encodedLimit {
		return "", OverLimit
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(encoded, "="))
	switch {
	case err != nil:
		return "", BadText
	case len(raw) > limit:
		return "", OverLimit
	case !utf8.Valid(raw) || strings.ContainsFunc(string(raw), control):
		return "", BadText
	}
	return string(raw), nil
}
