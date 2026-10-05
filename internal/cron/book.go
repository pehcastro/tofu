package cron

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type By string

const (
	Person By = "person"
	Agent  By = "agent"
)

const idPrefix = "c"

var (
	ErrNoReason    = errors.New("a change to a cron job needs a reason")
	errAgentCheck  = errors.New("a check command runs with no gate, so only the person names one: ask the person to set it")
	errGoalCheck   = errors.New("a job that fires after each turn needs a check command, or it fires until a cap stops it")
	errUnchanged   = errors.New("the change leaves the job as it was")
	errEmptyPrompt = errors.New("a cron job needs a prompt")
)

type Spec struct {
	Schedule string    `json:"schedule"`
	Prompt   string    `json:"prompt"`
	Expires  time.Time `json:"expires"`
	Until    string    `json:"until,omitempty"`
	Paused   bool      `json:"paused,omitempty"`
}

type Version struct {
	N      int       `json:"n"`
	At     time.Time `json:"at"`
	By     By        `json:"by"`
	Reason string    `json:"reason"`
	Spec   Spec      `json:"spec"`
}

type Job struct {
	ID         string    `json:"id"`
	Versions   []Version `json:"versions"`
	Next       time.Time `json:"next,omitzero"`
	Fires      int       `json:"fires"`
	LastResult string    `json:"last_result,omitempty"`
	Unchanged  int       `json:"unchanged,omitempty"`
	Ended      string    `json:"ended,omitempty"`
	waiting    bool
}

func (j Job) Spec() Spec { return j.Versions[len(j.Versions)-1].Spec }

func (j Job) Version() int { return len(j.Versions) }

func (j Job) Live() bool { return j.Ended == "" && !j.Spec().Paused }

func (j Job) Noun() string {
	if j.Spec().Schedule == string(KindAfterTurn) {
		return "goal"
	}
	return "cron"
}

func (j Job) endedLine() string { return j.Noun() + " " + j.ID + " ended: " + j.Ended }

type Checker func(ctx context.Context, command string) (exit int, output string, err error)

type Fire struct {
	ID     string
	Prompt string
	Line   string
}

type Moment int

const (
	Tick Moment = iota
	TurnEnded
)

type Change struct {
	Schedule string
	Prompt   string
	Expires  time.Time
	Until    string
	Paused   *bool
}

type Book struct {
	Check Checker
	mu    sync.Mutex
	path  string
	made  int
	jobs  []*Job
}

type kept struct {
	Made int    `json:"made"`
	Jobs []*Job `json:"jobs"`
}

func (b *Book) Load(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path, b.made, b.jobs = path, 0, nil
	if path == "" {
		return nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var read kept
	if err == nil {
		err = json.Unmarshal(body, &read)
	}
	for _, job := range read.Jobs {
		if err == nil && len(job.Versions) == 0 {
			err = errors.New(job.ID + " has no version")
		}
	}
	if err != nil {
		b.path = ""
		return fmt.Errorf("cron jobs in %s are unreadable, so none is kept and the file is left as it is: %w", path, err)
	}
	b.made, b.jobs = read.Made, read.Jobs
	return nil
}

func (b *Book) Keep(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path = path
	return b.save()
}

func (b *Book) save() error {
	if b.path == "" {
		return nil
	}
	body, err := json.MarshalIndent(kept{Made: b.made, Jobs: b.jobs}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	temporary := b.path + ".tmp"
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, b.path)
}

func (b *Book) find(id string) (*Job, error) {
	at := slices.IndexFunc(b.jobs, func(job *Job) bool { return job.ID == id })
	if at < 0 {
		return nil, fmt.Errorf("there is no cron job %q", id)
	}
	return b.jobs[at], nil
}

func valid(spec Spec, now time.Time) (Schedule, error) {
	schedule, err := Parse(spec.Schedule, now)
	switch {
	case err != nil:
		return Schedule{}, err
	case strings.TrimSpace(spec.Prompt) == "":
		return Schedule{}, errEmptyPrompt
	case !spec.Expires.After(now):
		return Schedule{}, fmt.Errorf("the expiry %s is already past", spec.Expires.Format(time.DateTime))
	case schedule.Kind == KindAfterTurn && spec.Until == "":
		return Schedule{}, errGoalCheck
	}
	return schedule, nil
}

func (b *Book) Create(spec Spec, by By, reason string, now time.Time) (Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	live := 0
	for _, job := range b.jobs {
		if job.Ended == "" {
			live++
		}
	}
	switch {
	case reason == "":
		return Job{}, ErrNoReason
	case by == Agent && spec.Until != "":
		return Job{}, errAgentCheck
	case live >= JobsMax:
		return Job{}, fmt.Errorf("%d cron jobs are open, the most a session holds: delete one first", JobsMax)
	}
	if spec.Expires.IsZero() {
		spec.Expires = now.Add(ExpiryHours * time.Hour)
	}
	schedule, err := valid(spec, now)
	if err != nil {
		return Job{}, err
	}
	spec.Schedule = schedule.Text
	b.made++
	job := &Job{ID: idPrefix + strconv.Itoa(b.made), Versions: []Version{{N: 1, At: now, By: by, Reason: reason, Spec: spec}}, Next: schedule.Next(now)}
	b.jobs = append(b.jobs, job)
	return *job, b.save()
}

func (b *Book) Update(id string, change Change, by By, reason string, now time.Time) (Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	job, err := b.find(id)
	if err != nil {
		return Job{}, err
	}
	before := job.Spec()
	after := before
	after.Schedule = edited(change.Schedule, after.Schedule)
	after.Prompt = edited(change.Prompt, after.Prompt)
	after.Until = edited(change.Until, after.Until)
	if !change.Expires.IsZero() {
		after.Expires = change.Expires
	}
	if change.Paused != nil {
		after.Paused = *change.Paused
	}
	stretch := personExpiry(*job).Add(ExpiryHours * time.Hour)
	switch {
	case job.Ended != "":
		return Job{}, fmt.Errorf("%s ended (%s): delete it and make a new one", id, job.Ended)
	case reason == "":
		return Job{}, ErrNoReason
	case after == before:
		return Job{}, errUnchanged
	case by == Agent && after.Until != before.Until:
		return Job{}, errAgentCheck
	case by == Agent && after.Expires.After(stretch):
		return Job{}, fmt.Errorf("an agent stretches an expiry at most %dh past the person's, to %s: ask the person for more (rule cron_edits)", ExpiryHours, stretch.Format(time.DateTime))
	}
	schedule, err := valid(after, now)
	if err != nil {
		return Job{}, err
	}
	after.Schedule = schedule.Text
	job.Versions = append(job.Versions, Version{N: len(job.Versions) + 1, At: now, By: by, Reason: reason, Spec: after})
	if after.Schedule != before.Schedule || before.Paused && !after.Paused {
		job.Next = schedule.Next(now)
	}
	return *job, b.save()
}

func edited(changed, was string) string {
	if strings.TrimSpace(changed) == "" {
		return was
	}
	return changed
}

func personExpiry(job Job) time.Time {
	for index := len(job.Versions) - 1; index >= 0; index-- {
		if job.Versions[index].By == Person {
			return job.Versions[index].Spec.Expires
		}
	}
	return job.Versions[0].Spec.Expires
}

func (b *Book) Delete(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := b.find(id); err != nil {
		return err
	}
	b.jobs = slices.DeleteFunc(b.jobs, func(job *Job) bool { return job.ID == id })
	return b.save()
}

func (b *Book) Jobs() []Job {
	b.mu.Lock()
	defer b.mu.Unlock()
	jobs := make([]Job, len(b.jobs))
	for index, job := range b.jobs {
		jobs[index] = *job
	}
	return jobs
}

func (b *Book) Job(id string) (Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	job, err := b.find(id)
	if err != nil {
		return Job{}, err
	}
	return *job, nil
}

func (b *Book) Live() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	live := 0
	for _, job := range b.jobs {
		if job.Live() {
			live++
		}
	}
	return live
}

type pending struct {
	job     *Job
	spec    Spec
	passed  bool
	checked string
	failed  error
}

func (b *Book) Due(ctx context.Context, now time.Time, moment Moment) []Fire {
	b.mu.Lock()
	var fires []Fire
	var due []pending
	for _, job := range b.jobs {
		spec := job.Spec()
		afterTurn := spec.Schedule == string(KindAfterTurn)
		switch {
		case !job.Live() || job.waiting:
		case now.After(spec.Expires):
			job.Ended = "expired " + spec.Expires.Format(time.DateTime)
			fires = append(fires, Fire{ID: job.ID, Line: job.endedLine()})
		case moment == TurnEnded && afterTurn, moment == Tick && !afterTurn && !job.Next.IsZero() && !now.Before(job.Next):
			job.waiting = true
			due = append(due, pending{job: job, spec: spec})
		}
	}
	b.mu.Unlock()
	for index, one := range due {
		if one.spec.Until != "" {
			due[index].passed, due[index].checked, due[index].failed = b.check(ctx, one.spec.Until)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, one := range due {
		fires = append(fires, fire(one, now))
	}
	if err := b.save(); err != nil {
		fires = append(fires, Fire{Line: "cron jobs were not written: " + err.Error()})
	}
	return fires
}

func (b *Book) check(ctx context.Context, command string) (bool, string, error) {
	if b.Check == nil {
		return false, "", errors.New("no shell is wired to run it")
	}
	ctx, cancel := context.WithTimeout(ctx, CheckTimeoutMillis*time.Millisecond)
	defer cancel()
	exit, output, err := b.Check(ctx, command)
	if err != nil {
		return false, "", err
	}
	tail := strings.TrimSpace(output)
	if len(tail) > CheckTailBytes {
		tail = "..." + tail[len(tail)-CheckTailBytes:]
	}
	return exit == 0, "`" + command + "` exited " + strconv.Itoa(exit) + "\n" + tail, nil
}

func fire(one pending, now time.Time) Fire {
	job, spec := one.job, one.spec
	said, _, _ := strings.Cut(one.checked, "\n")
	schedule, _ := Parse(spec.Schedule, now)
	job.Next = schedule.Next(now)
	switch {
	case one.failed != nil:
		job.Ended = "the check could not run: " + one.failed.Error()
	case one.passed:
		job.Ended = "check passed: " + said
	case job.Fires >= FiresMax:
		job.Ended = "fired " + strconv.Itoa(FiresMax) + " times, the most one job may"
	}
	if job.Ended != "" {
		job.waiting = false
		return Fire{ID: job.ID, Line: job.endedLine()}
	}
	job.Fires++
	head := job.Noun() + " " + job.ID
	prompt := head + " fired (" + spec.Schedule + ", v" + strconv.Itoa(job.Version()) + "), not typed by the person:\n" + spec.Prompt
	line := head + " fired · " + spec.Schedule + " · " + firstLine(spec.Prompt)
	if one.checked != "" {
		prompt += "\n\nits check is not met yet: " + one.checked
		line += " · not met: " + said
	}
	return Fire{ID: job.ID, Prompt: prompt, Line: line}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func (b *Book) Finished(id, result string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	job, err := b.find(id)
	if err != nil {
		return ""
	}
	result = firstLine(result)
	job.waiting = false
	if result == job.LastResult {
		job.Unchanged++
	} else {
		job.Unchanged = 0
	}
	job.LastResult = result
	switch {
	case job.Unchanged >= UnchangedStop:
		job.Ended = strconv.Itoa(UnchangedStop) + " fires in a row changed nothing"
	case job.Next.IsZero() && job.Spec().Schedule != string(KindAfterTurn):
		job.Ended = "fired once"
	}
	note := ""
	if job.Ended != "" {
		note = job.endedLine()
	}
	if err := b.save(); err != nil {
		note = strings.TrimSpace(note + "; cron jobs were not written: " + err.Error())
	}
	return note
}
