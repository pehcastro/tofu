package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type View int

const (
	ViewNone View = iota
	ViewList
	ViewHistory
)

type Reply struct {
	Note       string
	View       View
	Job        string
	CheckGoals bool
}

const (
	cronWord   = "/cron"
	loopWord   = "/loop"
	goalWord   = "/goal"
	cronUsage  = `/cron "<schedule>" <prompt> [--until "<cmd>"] [--expires 48h], /cron list, /cron history <id>, /cron pause|resume|delete <id>, /cron edit <id> [--schedule "<schedule>"] [--prompt "<text>"] [--expires 48h] [--until "<cmd>"] --reason "<why>"`
	loopUsage  = "/loop <interval> <prompt>, such as /loop 10m check the build"
	goalUsage  = `/goal <prompt> --until "<cmd>", such as /goal make the test pass --until "go test ./..."`
	timeLayout = "Jan 2 15:04:05"
)

func IsCommand(line string) bool {
	word, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	return word == cronWord || word == loopWord || word == goalWord
}

type order struct {
	args  []string
	flags map[string]string
}

func parseOrder(line string) (order, error) {
	var words []string
	var word strings.Builder
	quoted, started := false, false
	for _, letter := range line {
		switch {
		case letter == '"':
			quoted, started = !quoted, true
		case unicode.IsSpace(letter) && !quoted:
			if started {
				words, started = append(words, word.String()), false
				word.Reset()
			}
		default:
			word.WriteRune(letter)
			started = true
		}
	}
	if quoted {
		return order{}, errors.New("a double quote is not closed")
	}
	if started {
		words = append(words, word.String())
	}
	parsed := order{flags: map[string]string{}}
	for index := 0; index < len(words); index++ {
		name, isFlag := strings.CutPrefix(words[index], "--")
		if !isFlag {
			parsed.args = append(parsed.args, words[index])
			continue
		}
		switch name {
		case "until", "expires", "reason", "schedule", "prompt":
		default:
			return order{}, fmt.Errorf("--%s is not a cron flag", name)
		}
		if index++; index >= len(words) {
			return order{}, fmt.Errorf("--%s wants a value", name)
		}
		parsed.flags[name] = words[index]
	}
	return parsed, nil
}

func ExpiresIn(text string, now time.Time) (time.Time, error) {
	if text == "" {
		return time.Time{}, nil
	}
	after, err := parseDuration(text)
	return now.Add(after), err
}

func (b *Book) Command(line string, now time.Time) (Reply, error) {
	parsed, err := parseOrder(line)
	if err != nil {
		return Reply{}, err
	}
	expires, err := ExpiresIn(parsed.flags["expires"], now)
	if err != nil {
		return Reply{}, err
	}
	word, args := parsed.args[0], parsed.args[1:]
	spec := Spec{Expires: expires, Until: parsed.flags["until"]}
	switch word {
	case loopWord:
		if len(args) < 2 {
			return Reply{}, errors.New("usage: " + loopUsage)
		}
		spec.Schedule, spec.Prompt = everyPrefix+args[0], strings.Join(args[1:], " ")
		return b.createReply(spec, "made by /loop", now)
	case goalWord:
		if len(args) == 0 || spec.Until == "" {
			return Reply{}, errors.New("usage: " + goalUsage)
		}
		spec.Schedule, spec.Prompt = string(KindAfterTurn), strings.Join(args, " ")
		reply, err := b.createReply(spec, "made by /goal", now)
		reply.CheckGoals = err == nil
		return reply, err
	}
	verb, id := "list", ""
	if len(args) > 0 {
		verb = args[0]
	}
	if len(args) > 1 {
		id = args[1]
	}
	paused, resumed := true, false
	switch {
	case verb == "list":
		return Reply{View: ViewList}, nil
	case id == "" && (verb == "history" || verb == "pause" || verb == "resume" || verb == "delete" || verb == "edit"):
		return Reply{}, errors.New(verb + " names a job: " + cronUsage)
	case verb == "history":
		_, err := b.Job(id)
		return Reply{View: ViewHistory, Job: id}, err
	case verb == "delete":
		return Reply{Note: "deleted cron " + id}, b.Delete(id)
	case verb == "pause":
		return b.updateReply(id, Change{Paused: &paused}, parsed.flags["reason"], "paused by the person", now)
	case verb == "resume":
		return b.updateReply(id, Change{Paused: &resumed}, parsed.flags["reason"], "resumed by the person", now)
	case verb == "edit":
		change := Change{Schedule: parsed.flags["schedule"], Prompt: parsed.flags["prompt"], Until: spec.Until, Expires: expires}
		return b.updateReply(id, change, parsed.flags["reason"], "", now)
	case len(args) < 2:
		return Reply{}, errors.New("usage: " + cronUsage)
	}
	spec.Schedule, spec.Prompt = args[0], strings.Join(args[1:], " ")
	return b.createReply(spec, "made by /cron", now)
}

func (b *Book) createReply(spec Spec, reason string, now time.Time) (Reply, error) {
	job, err := b.Create(spec, Person, reason, now)
	if err != nil {
		return Reply{}, err
	}
	return Reply{Note: "made " + job.Line(now)}, nil
}

func (b *Book) updateReply(id string, change Change, reason, otherwise string, now time.Time) (Reply, error) {
	job, err := b.Update(id, change, Person, edited(reason, otherwise), now)
	if err != nil {
		return Reply{}, err
	}
	return Reply{Note: "changed " + job.Line(now)}, nil
}

func span(left time.Duration) string {
	left = left.Round(time.Second)
	switch {
	case left < time.Minute:
		return strconv.Itoa(int(left.Seconds())) + "s"
	case left < time.Hour:
		return strconv.Itoa(int(left.Minutes())) + "m"
	}
	return strconv.Itoa(int(left.Hours())) + "h" + strconv.Itoa(int(left.Minutes())%60) + "m"
}

func (j Job) Line(now time.Time) string {
	spec := j.Spec()
	parts := []string{j.Noun() + " " + j.ID, spec.Schedule, "v" + strconv.Itoa(j.Version())}
	switch {
	case j.Ended != "":
		parts = append(parts, "ended: "+j.Ended)
	case spec.Paused:
		parts = append(parts, "paused")
	case !j.Next.IsZero():
		parts = append(parts, "next "+j.Next.Format(timeLayout)+" (in "+span(max(j.Next.Sub(now), 0))+")")
	}
	if j.Ended == "" {
		parts = append(parts, "expires "+spec.Expires.Format(timeLayout))
	}
	parts = append(parts, "fired "+strconv.Itoa(j.Fires))
	if j.LastResult != "" {
		parts = append(parts, "last: "+j.LastResult)
	}
	return strings.Join(parts, " · ")
}

func (j Job) History() []string {
	lines := make([]string, 0, 2*len(j.Versions))
	for _, version := range j.Versions {
		spec := version.Spec
		detail := []string{spec.Schedule, "expires " + spec.Expires.Format(timeLayout)}
		if spec.Until != "" {
			detail = append(detail, "until "+spec.Until)
		}
		if spec.Paused {
			detail = append(detail, "paused")
		}
		lines = append(lines,
			"v"+strconv.Itoa(version.N)+" · "+version.At.Format(timeLayout)+" · by "+string(version.By)+" · "+version.Reason,
			"   "+strings.Join(append(detail, firstLine(spec.Prompt)), " · "))
	}
	return lines
}
