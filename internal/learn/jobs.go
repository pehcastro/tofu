package learn

import (
	"cmp"
	"slices"
	"time"

	"tofu/internal/learn/words"
)

type JobRun struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Text    string    `json:"text"`
	Calls   int       `json:"calls"`
	Tokens  int       `json:"tokens"`
	Cost    float64   `json:"cost_usd"`
}

type Job struct {
	Name       string   `json:"name"`
	Tools      []string `json:"tools"`
	Times      int      `json:"times"`
	Sessions   []string `json:"sessions"`
	Tokens     int      `json:"tokens"`
	TokensEach int      `json:"tokens_each"`
	Cost       float64  `json:"cost_usd"`
	CostEach   float64  `json:"cost_usd_each"`
	Runs       []JobRun `json:"runs"`
}

type jobRun struct {
	JobRun
	tools []string
	stems []string
}

func (a jobRun) same(b jobRun) bool {
	return slices.Equal(a.tools, b.tools) && words.Alike(a.stems, b.stems, nil) > 0
}

func repeatedJobs(sittings []sitting, all []heard) []Job {
	var groups [][]jobRun
	for _, asked := range all {
		if asked.steer || asked.Text == "" {
			continue
		}
		var until time.Time
		for _, h := range all {
			if h.seat == asked.seat && !h.steer && h.At.After(asked.At) && (until.IsZero() || h.At.Before(until)) {
				until = h.At
			}
		}
		run := jobRun{JobRun: JobRun{Session: asked.Session, At: asked.At, Text: asked.Text}, stems: words.Stems(asked.Text)}
		for _, w := range sittings[asked.seat].worked {
			if w.at.Before(asked.At) || !until.IsZero() && !w.at.Before(until) {
				continue
			}
			run.Tokens, run.Cost, run.Calls = run.Tokens+w.tokens, run.Cost+w.cost, run.Calls+len(w.tools)
			for _, tool := range w.tools {
				if len(run.tools) == 0 || run.tools[len(run.tools)-1] != tool {
					run.tools = append(run.tools, tool)
				}
			}
		}
		if len(run.tools) == 0 {
			continue
		}
		if g := slices.IndexFunc(groups, func(group []jobRun) bool { return slices.ContainsFunc(group, run.same) }); g >= 0 {
			groups[g] = append(groups[g], run)
			continue
		}
		groups = append(groups, []jobRun{run})
	}
	var jobs []Job
	for _, group := range groups {
		if len(group) < RepeatedJobTimes {
			continue
		}
		slices.SortStableFunc(group, func(a, b jobRun) int { return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Session, b.Session)) })
		job := Job{Name: group[0].Text, Tools: group[0].tools, Times: len(group)}
		for _, run := range group {
			job.Runs, job.Tokens, job.Cost = append(job.Runs, run.JobRun), job.Tokens+run.Tokens, job.Cost+run.Cost
			if len(run.Text) < len(job.Name) {
				job.Name = run.Text
			}
			if !slices.Contains(job.Sessions, run.Session) {
				job.Sessions = append(job.Sessions, run.Session)
			}
		}
		job.TokensEach, job.CostEach = job.Tokens/job.Times, job.Cost/float64(job.Times)
		jobs = append(jobs, job)
	}
	slices.SortStableFunc(jobs, func(a, b Job) int { return cmp.Compare(b.Cost, a.Cost) })
	return jobs
}
