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

	"tofu/internal/konst"
	platformfs "tofu/internal/platform/fs"
)

type By string

const (
	Person By = "person"
	Agent  By = "agent"
)

const (
	idPrefix    = "c"
	expiredWord = "expired "
	whileAway   = "the person is away, so nobody answers in this turn: keep working, and where a question has a recommended option that can be undone, " +
		"take it and say in your reply what you took and why. anything that pushes, deletes, publishes or spends waits for the person"
)

var (
	ErrNoReason    = errors.New("a change to a cron job needs a reason")
	errAgentCheck  = errors.New("a check command runs with no gate, so only the person names one: ask the person to set it")
	errGoalCheck   = errors.New("a job that fires after each turn needs a check command, or it fires until a cap stops it")
	errUnchanged   = errors.New("the change leaves the job as it was")
	errAwayAlready = errors.New("the session is already set that way")
	errEmptyPrompt = errors.New("a cron job needs a prompt")
)

type Spec struct {
	Schedule string    `json:"schedule"`
	Prompt   string    `json:"prompt"`
	Expires  time.Time `json:"expires"`
	Until    string    `json:"until,omitempty"`
	Paused   bool      `json:"paused,omitempty"`
}

type Away struct {
	On     bool      `json:"on"`
	At     time.Time `json:"at"`
	By     By        `json:"by"`
	Reason string    `json:"reason"`
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
	LastFired  time.Time `json:"last_fired,omitzero"`
	LastResult string    `json:"last_result,omitempty"`
	Unchanged  int       `json:"unchanged,omitempty"`
	Ended      string    `json:"ended,omitempty"`
	Handed     time.Time `json:"-"`
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
	Check   Checker
	Changed chan<- struct{}
	mu      sync.Mutex
	path    string
	made    int
	jobs    []*Job
	away    Away
}

type kept struct {
	Made int    `json:"made"`
	Jobs []*Job `json:"jobs"`
	Away Away   `json:"away,omitzero"`
}

func (b *Book) Load(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path, b.made, b.jobs, b.away = path, 0, nil, Away{}
	defer b.changed()
	if path == "" {
		return nil
	}
	read, err := readKept(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		b.path = ""
		return err
	}
	b.made, b.jobs, b.away = read.Made, read.Jobs, read.Away
	return nil
}

func readKept(path string) (kept, error) {
	var body []byte
	err := platformfs.Settled(func() (err error) {
		body, err = os.ReadFile(path)
		return err
	})
	if err != nil {
		return kept{}, err
	}
	var read kept
	err = json.Unmarshal(body, &read)
	for _, job := range read.Jobs {
		if err == nil && len(job.Versions) == 0 {
			err = errors.New(job.ID + " has no version")
		}
	}
	if err != nil {
		return kept{}, fmt.Errorf("cron jobs in %s are unreadable, so none is kept and the file is left as it is: %w", path, err)
	}
	return read, nil
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
	if _, err := readKept(b.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if b.made == 0 && b.away == (Away{}) {
		return nil
	}
	body, err := json.MarshalIndent(kept{Made: b.made, Jobs: b.jobs, Away: b.away}, "", "  ")
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
	return platformfs.Settled(func() error { return os.Rename(temporary, b.path) })
}

func (b *Book) changed() {
	select {
	case b.Changed <- struct{}{}:
	default:
	}
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
	case live >= konst.CronJobsMax:
		return Job{}, fmt.Errorf("%d cron jobs are open, the most a session holds: delete one first", konst.CronJobsMax)
	}
	if spec.Expires.IsZero() {
		spec.Expires = now.Add(konst.CronExpiryHours * time.Hour)
	}
	schedule, err := valid(spec, now)
	if err != nil {
		return Job{}, err
	}
	spec.Schedule = schedule.Text
	b.made++
	job := &Job{ID: idPrefix + strconv.Itoa(b.made), Versions: []Version{{N: 1, At: now, By: by, Reason: reason, Spec: spec}}, Next: schedule.Next(now)}
	b.jobs = append(b.jobs, job)
	b.changed()
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
	stretch := personExpiry(*job).Add(konst.CronExpiryHours * time.Hour)
	expired := strings.HasPrefix(job.Ended, expiredWord)
	switch {
	case job.Ended != "" && !expired:
		return Job{}, fmt.Errorf("%s ended (%s): delete it and make a new one", id, job.Ended)
	case reason == "":
		return Job{}, ErrNoReason
	case after == before:
		return Job{}, errUnchanged
	case by == Agent && after.Until != before.Until:
		return Job{}, errAgentCheck
	case by == Agent && after.Expires.After(stretch):
		return Job{}, fmt.Errorf("an agent stretches an expiry at most %dh past the person's, to %s: ask the person for more (rule cron_edits)", konst.CronExpiryHours, stretch.Format(time.DateTime))
	case !after.Expires.After(now):
		return Job{}, fmt.Errorf("%s expired %s: give it a later expiry to renew it", id, after.Expires.Format(time.DateTime))
	}
	schedule, err := valid(after, now)
	if err != nil {
		return Job{}, err
	}
	after.Schedule = schedule.Text
	job.Versions = append(job.Versions, Version{N: len(job.Versions) + 1, At: now, By: by, Reason: reason, Spec: after})
	if expired || after.Schedule != before.Schedule || before.Paused && !after.Paused {
		job.Next = schedule.Next(now)
	}
	job.Ended = ""
	b.changed()
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
	b.changed()
	return b.save()
}

func (b *Book) End(reason string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ended []string
	for _, job := range b.jobs {
		if job.Ended == "" {
			job.Ended, job.waiting = reason, false
			ended = append(ended, job.Noun()+" "+job.ID)
		}
	}
	if len(ended) == 0 {
		return nil, nil
	}
	b.changed()
	return ended, b.save()
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
			job.Ended = expiredWord + spec.Expires.Format(time.DateTime)
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
		fires = append(fires, fire(one, b.away.On, now))
	}
	if len(fires) == 0 {
		return nil
	}
	b.changed()
	if err := b.save(); err != nil {
		fires = append(fires, Fire{Line: "cron jobs were not written: " + err.Error()})
	}
	return fires
}

func (b *Book) check(ctx context.Context, command string) (bool, string, error) {
	if b.Check == nil {
		return false, "", errors.New("no shell is wired to run it")
	}
	ctx, cancel := context.WithTimeout(ctx, konst.CronCheckTimeoutMillis*time.Millisecond)
	defer cancel()
	exit, output, err := b.Check(ctx, command)
	if err != nil {
		return false, "", err
	}
	tail := strings.TrimSpace(output)
	if len(tail) > konst.CronCheckTailBytes {
		tail = "..." + tail[len(tail)-konst.CronCheckTailBytes:]
	}
	return exit == 0, "`" + command + "` exited " + strconv.Itoa(exit) + "\n" + tail, nil
}

func fire(one pending, away bool, now time.Time) Fire {
	job, spec := one.job, one.spec
	said, _, _ := strings.Cut(one.checked, "\n")
	schedule, _ := Parse(spec.Schedule, now)
	job.Next = schedule.Next(now)
	switch {
	case one.failed != nil:
		job.Ended = "the check could not run: " + one.failed.Error()
	case one.passed:
		job.Ended = "check passed: " + said
	case job.Fires >= konst.CronFiresMax:
		job.Ended = "fired " + strconv.Itoa(konst.CronFiresMax) + " times, the most one job may"
	}
	if job.Ended != "" {
		job.waiting = false
		return Fire{ID: job.ID, Line: job.endedLine()}
	}
	job.Fires++
	job.LastFired = now
	head := job.Noun() + " " + job.ID
	prompt := head + " fired (" + spec.Schedule + ", v" + strconv.Itoa(job.Version()) + konst.CronPromptMark + spec.Prompt
	line := head + " fired · " + spec.Schedule + " · " + firstLine(spec.Prompt)
	if one.checked != "" {
		prompt += "\n\nits check is not met yet: " + one.checked
		line += " · not met: " + said
	}
	if away {
		prompt += "\n\n" + whileAway
		line += " · " + awayWord
	}
	return Fire{ID: job.ID, Prompt: prompt, Line: line}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func (b *Book) Away() Away {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.away
}

func (b *Book) SetAway(on bool, by By, reason string, now time.Time) (Away, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case reason == "":
		return b.away, ErrNoReason
	case on == b.away.On:
		return b.away, errAwayAlready
	}
	b.away = Away{On: on, At: now, By: by, Reason: reason}
	b.changed()
	return b.away, b.save()
}

func (a Away) Line() string {
	state := "back"
	if a.On {
		state = "away"
	}
	return "the person is " + state + ", set by " + string(a.By) + ": " + a.Reason
}

func (b *Book) Hand(id string, at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if job, err := b.find(id); err == nil && job.Handed.IsZero() {
		job.Handed = at
	}
}

func (b *Book) Finished(id, result string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	job, err := b.find(id)
	if err != nil {
		return ""
	}
	result = firstLine(result)
	job.waiting, job.Handed = false, time.Time{}
	if result == job.LastResult {
		job.Unchanged++
	} else {
		job.Unchanged = 0
	}
	job.LastResult = result
	b.changed()
	switch {
	case job.Unchanged >= konst.CronUnchangedStop:
		job.Ended = strconv.Itoa(konst.CronUnchangedStop) + " fires in a row changed nothing"
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
