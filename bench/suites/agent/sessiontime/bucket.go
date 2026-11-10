package main

import (
	"strings"

	"tofu/bench/kit/transcript"
)

type bucket string

const (
	bucketAPI       bucket = "API calls"
	bucketBuild            = bucket(transcript.Build)
	bucketTest             = bucket(transcript.Test)
	bucketScript           = bucket(transcript.Script)
	bucketOtherBash        = bucket(transcript.OtherBash)
	bucketBashRead         = bucket(transcript.BashRead)
	bucketWait             = bucket(transcript.Wait)
	bucketSearch    bucket = "native search, glob, grep"
	bucketGit              = bucket(transcript.Git)
	bucketFile      bucket = "read, edit, write"
	bucketWeb       bucket = "web and browser"
	bucketOtherTool bucket = "other tools"
	bucketSubAgent  bucket = "waiting on sub-agents, background tasks, wakeups"
	bucketPerson    bucket = "waiting on the person"
	bucketIdle      bucket = "idle gaps over 15 minutes"
)

var counted = []bucket{bucketAPI, bucketBuild, bucketTest, bucketScript, bucketOtherBash, bucketBashRead, bucketWait, bucketSearch, bucketGit, bucketFile, bucketWeb, bucketOtherTool}

var excluded = []bucket{bucketSubAgent, bucketPerson, bucketIdle}

func toolBucket(tool string) bucket {
	name := strings.ToLower(tool)
	switch name {
	case "read", "edit", "write", "multiedit", "notebookedit", "quote", "history", "artifact_fetch":
		return bucketFile
	case "search", "glob", "grep", "symbols", "zoom", "gosymbol", "toolsearch":
		return bucketSearch
	case "test":
		return bucketTest
	case "typecheck":
		return bucketBuild
	case "fetch", "web_search", "webfetch", "websearch", "github_pr_diff":
		return bucketWeb
	case "spawn", "ask", "task", "agent", "taskoutput":
		return bucketSubAgent
	case "ask_person", "askuserquestion":
		return bucketPerson
	case "bashoutput":
		return bucketWait
	case "killshell", "killbash":
		return bucketOtherBash
	}
	if strings.HasPrefix(name, "browser") {
		return bucketWeb
	}
	return bucketOtherTool
}
