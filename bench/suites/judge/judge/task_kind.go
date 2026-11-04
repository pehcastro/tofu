package judge

import (
	"cmp"
	"fmt"

	"tofu/internal/agent/loop"
	"tofu/internal/librarian/rules"
)

type KindCase struct {
	Task string
	Kind rules.Verb
}

func KindCorpus() []KindCase {
	return []KindCase{
		{"fix the crash when the picker closes", rules.VerbDebug},
		{"why does resume lose the head", rules.VerbDebug},
		{"the status bar shows the wrong model after a switch", rules.VerbDebug},
		{"tests in the parser package fail on windows since yesterday", rules.VerbDebug},
		{"the session list hangs when a store file is half written", rules.VerbDebug},
		{"something panics with a nil map when I paste a long line", rules.VerbDebug},
		{"the cost total doubles after a resume, find out why", rules.VerbDebug},
		{"debug the failing test in the ledger reader", rules.VerbDebug},
		{"login stopped working after the last update", rules.VerbDebug},
		{"the build is red on main, get it green", rules.VerbDebug},
		{"look over my last change", rules.VerbReview},
		{"check the diff on this branch before I merge it", rules.VerbReview},
		{"go through the commit I just made and point out anything off", rules.VerbReview},
		{"is the patch in the open pull request safe to land", rules.VerbReview},
		{"read what the other agent changed in the theme package and judge it", rules.VerbReview},
		{"review the staged changes", rules.VerbReview},
		{"give me a second opinion on the refactor I pushed this morning", rules.VerbReview},
		{"audit yesterday's edits to the settings loader", rules.VerbReview},
		{"how does the inbox decide a request is open", rules.VerbExplore},
		{"where is the context window trimmed", rules.VerbExplore},
		{"walk me through what happens when a sub-agent is spawned", rules.VerbExplore},
		{"what reads the shipped rules at startup", rules.VerbExplore},
		{"explore the transport package and tell me how retries work", rules.VerbExplore},
		{"which files would I need to touch to add a new provider", rules.VerbExplore},
		{"add a flag that prints the version as json", rules.VerbWrite},
		{"rename the plan field in the budget struct to allowance", rules.VerbWrite},
		{"add a debug flag to the run command", rules.VerbWrite},
		{"write a function that parses durations like 3m and 2h", rules.VerbWrite},
		{"move the colour constants into the theme file", rules.VerbWrite},
		{"make the picker remember the last model chosen", rules.VerbWrite},
		{"build the review screen for the settings page", rules.VerbWrite},
		{"split the long handler into one function per route", rules.VerbWrite},
		{"plan how we move sessions from json files to sqlite", rules.VerbPlan},
		{"write tickets for the three gaps we found today", rules.VerbPlan},
		{"break the export feature into an epic with its parts", rules.VerbPlan},
		{"sketch the steps to support a second terminal theme before anyone codes it", rules.VerbPlan},
		{"thanks, that works", rules.VerbNone},
		{"yes, go ahead", rules.VerbNone},
		{"good morning", rules.VerbNone},
		{"ok", rules.VerbNone},
	}
}

type KindTally struct {
	Right  int
	Missed []string
}

func (t *KindTally) count(c KindCase, got rules.Verb) {
	if got == c.Kind {
		t.Right++
		return
	}
	t.Missed = append(t.Missed, fmt.Sprintf("%q as %s, labelled %s", c.Task, cmp.Or(string(got), "none"), cmp.Or(string(c.Kind), "none")))
}

func KindAgainstTheWordList(judge *loop.KindJudge) (jev, words KindTally, fellBack []string) {
	for _, c := range KindCorpus() {
		named := loop.TaskNamed(c.Task).Verb
		words.count(c, named)
		kind, wordsDecided := judge.Of(c.Task, named)
		if wordsDecided != nil {
			fellBack = append(fellBack, c.Task+": "+wordsDecided.Error())
		}
		jev.count(c, kind)
	}
	return jev, words, fellBack
}
