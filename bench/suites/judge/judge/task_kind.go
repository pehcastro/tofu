package judge

import (
	"cmp"
	"fmt"

	"tofu/internal/agent/loop"
	"tofu/internal/librarian/rules"
)

type KindCase struct {
	Task string
	Kind rule.Verb
}

func KindCorpus() []KindCase {
	return []KindCase{
		{"fix the crash when the picker closes", rule.VerbDebug},
		{"why does resume lose the head", rule.VerbDebug},
		{"the status bar shows the wrong model after a switch", rule.VerbDebug},
		{"tests in the parser package fail on windows since yesterday", rule.VerbDebug},
		{"the session list hangs when a store file is half written", rule.VerbDebug},
		{"something panics with a nil map when I paste a long line", rule.VerbDebug},
		{"the cost total doubles after a resume, find out why", rule.VerbDebug},
		{"debug the failing test in the ledger reader", rule.VerbDebug},
		{"login stopped working after the last update", rule.VerbDebug},
		{"the build is red on main, get it green", rule.VerbDebug},
		{"look over my last change", rule.VerbReview},
		{"check the diff on this branch before I merge it", rule.VerbReview},
		{"go through the commit I just made and point out anything off", rule.VerbReview},
		{"is the patch in the open pull request safe to land", rule.VerbReview},
		{"read what the other agent changed in the theme package and judge it", rule.VerbReview},
		{"review the staged changes", rule.VerbReview},
		{"give me a second opinion on the refactor I pushed this morning", rule.VerbReview},
		{"audit yesterday's edits to the settings loader", rule.VerbReview},
		{"how does the inbox decide a request is open", rule.VerbExplore},
		{"where is the context window trimmed", rule.VerbExplore},
		{"walk me through what happens when a sub-agent is spawned", rule.VerbExplore},
		{"what reads the shipped rules at startup", rule.VerbExplore},
		{"explore the transport package and tell me how retries work", rule.VerbExplore},
		{"which files would I need to touch to add a new provider", rule.VerbExplore},
		{"add a flag that prints the version as json", rule.VerbWrite},
		{"rename the plan field in the budget struct to allowance", rule.VerbWrite},
		{"add a debug flag to the run command", rule.VerbWrite},
		{"write a function that parses durations like 3m and 2h", rule.VerbWrite},
		{"move the colour constants into the theme file", rule.VerbWrite},
		{"make the picker remember the last model chosen", rule.VerbWrite},
		{"build the review screen for the settings page", rule.VerbWrite},
		{"split the long handler into one function per route", rule.VerbWrite},
		{"plan how we move sessions from json files to sqlite", rule.VerbPlan},
		{"write tickets for the three gaps we found today", rule.VerbPlan},
		{"break the export feature into an epic with its parts", rule.VerbPlan},
		{"sketch the steps to support a second terminal theme before anyone codes it", rule.VerbPlan},
		{"thanks, that works", rule.VerbNone},
		{"yes, go ahead", rule.VerbNone},
		{"good morning", rule.VerbNone},
		{"ok", rule.VerbNone},
	}
}

type KindTally struct {
	Right  int
	Missed []string
}

func (t *KindTally) count(c KindCase, got rule.Verb) {
	if got == c.Kind {
		t.Right++
		return
	}
	t.Missed = append(t.Missed, fmt.Sprintf("%q as %s, labelled %s", c.Task, cmp.Or(string(got), "none"), cmp.Or(string(c.Kind), "none")))
}

func KindAgainstTheWordList(judge *turn.KindJudge) (jev, words KindTally, fellBack []string) {
	for _, c := range KindCorpus() {
		named := turn.TaskNamed(c.Task).Verb
		words.count(c, named)
		kind, wordsDecided := judge.Of(c.Task, named)
		if wordsDecided != nil {
			fellBack = append(fellBack, c.Task+": "+wordsDecided.Error())
		}
		jev.count(c, kind)
	}
	return jev, words, fellBack
}
