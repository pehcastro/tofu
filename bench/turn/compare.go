package turn

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Headline struct {
	EntryCount int
	Parsed     int
	Skipped    int
	AsRecorded ReplayArm
	Layered    ReplayArm
}

func HeadlineOf(r ReplayReport) Headline {
	return Headline{
		EntryCount: r.Corpus.EntryCount,
		Parsed:     len(r.Corpus.Turns),
		Skipped:    len(r.Corpus.Skipped),
		AsRecorded: r.AsRecordedTotal,
		Layered:    r.LayeredTotal,
	}
}

var corpusLine = regexp.MustCompile("(\\d+) entries read under `[^`]+`: (\\d+) parsed as a recorded turn, (\\d+) skipped\\.")

var asRecordedRow = regexp.MustCompile(`\| as recorded \| (\d+) \| (\d+) \| - \| - \| - \| (\d+) \| - \| (\d+) \| 0 \| (\d+) \|`)

var layeredRow = regexp.MustCompile(`\| with the call memo and the repair rule \| (\d+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \|`)

func ParseHeadline(body string) (Headline, error) {
	corpus := corpusLine.FindStringSubmatch(body)
	if corpus == nil {
		return Headline{}, fmt.Errorf("bench/turn: no line matching \"N entries read under ...\" in the report body")
	}
	asRecorded := asRecordedRow.FindStringSubmatch(body)
	if asRecorded == nil {
		return Headline{}, fmt.Errorf("bench/turn: no \"as recorded\" totals row in the report body")
	}
	layered := layeredRow.FindStringSubmatch(body)
	if layered == nil {
		return Headline{}, fmt.Errorf("bench/turn: no \"with the call memo and the repair rule\" totals row in the report body")
	}
	return Headline{
		EntryCount: atoi(corpus[1]),
		Parsed:     atoi(corpus[2]),
		Skipped:    atoi(corpus[3]),
		AsRecorded: ReplayArm{
			Steps:         atoi(asRecorded[1]),
			Calls:         atoi(asRecorded[2]),
			Refusals:      atoi(asRecorded[3]),
			BytesReturned: atoi64(asRecorded[4]),
			WallClockMS:   atof(asRecorded[5]),
		},
		Layered: ReplayArm{
			Steps:          atoi(layered[1]),
			Calls:          atoi(layered[2]),
			CacheHits:      atoi(layered[3]),
			Retries:        atoi(layered[4]),
			Repaired:       atoi(layered[5]),
			Refusals:       atoi(layered[6]),
			Undecided:      atoi(layered[7]),
			BytesReturned:  atoi64(layered[8]),
			AnswersChanged: atoi(layered[9]),
			WallClockMS:    atof(layered[10]),
		},
	}, nil
}

func atoi(s string) int     { n, _ := strconv.Atoi(s); return n }
func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
func atof(s string) float64 { n, _ := strconv.ParseFloat(s, 64); return n }

func CompareHeadlines(file, disk Headline) error {
	var moved []string
	report := func(figure string, fileVal, diskVal int) {
		if fileVal != diskVal {
			moved = append(moved, fmt.Sprintf("%s: the report says %d, disk computes %d", figure, fileVal, diskVal))
		}
	}
	report64 := func(figure string, fileVal, diskVal int64) {
		if fileVal != diskVal {
			moved = append(moved, fmt.Sprintf("%s: the report says %d, disk computes %d", figure, fileVal, diskVal))
		}
	}
	reportMS := func(figure string, fileVal, diskVal float64) {
		if fileVal != diskVal {
			moved = append(moved, fmt.Sprintf("%s: the report says %.0f ms, disk computes %.0f ms", figure, fileVal, diskVal))
		}
	}

	report("entries read", file.EntryCount, disk.EntryCount)
	report("entries parsed as a recorded turn", file.Parsed, disk.Parsed)
	report("entries skipped", file.Skipped, disk.Skipped)
	report("as recorded steps", file.AsRecorded.Steps, disk.AsRecorded.Steps)
	report("as recorded calls", file.AsRecorded.Calls, disk.AsRecorded.Calls)
	report("as recorded refused", file.AsRecorded.Refusals, disk.AsRecorded.Refusals)
	report64("as recorded bytes returned", file.AsRecorded.BytesReturned, disk.AsRecorded.BytesReturned)
	reportMS("as recorded wall clock", file.AsRecorded.WallClockMS, disk.AsRecorded.WallClockMS)
	report("layered steps", file.Layered.Steps, disk.Layered.Steps)
	report("layered calls", file.Layered.Calls, disk.Layered.Calls)
	report("layered cache hits", file.Layered.CacheHits, disk.Layered.CacheHits)
	report("layered retries removed", file.Layered.Retries, disk.Layered.Retries)
	report("layered repaired", file.Layered.Repaired, disk.Layered.Repaired)
	report("layered refused", file.Layered.Refusals, disk.Layered.Refusals)
	report("layered undecided", file.Layered.Undecided, disk.Layered.Undecided)
	report64("layered bytes returned", file.Layered.BytesReturned, disk.Layered.BytesReturned)
	report("layered answers changed", file.Layered.AnswersChanged, disk.Layered.AnswersChanged)
	reportMS("layered wall clock", file.Layered.WallClockMS, disk.Layered.WallClockMS)

	if len(moved) == 0 {
		return nil
	}
	return fmt.Errorf("%d figure(s) no longer follow from the recorded material:\n%s", len(moved), strings.Join(moved, "\n"))
}
