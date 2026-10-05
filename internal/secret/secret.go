package secret

import (
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type substitution struct {
	real     string
	scrubbed string
}

type Identities []substitution

type machine struct {
	home, user, email, gitName, workdir string
}

var credentialSubstitutions = []substitution{
	{"sk-or-v1-", "redacted-openrouter-key-"},
	{"sk-ant-api", "redacted-anthropic-api-key"},
	{"sk-ant-oat", "redacted-anthropic-oauth-token"},
	{"sk-proj-", "redacted-openai-key-"},
	{"ghp_", "redacted-github-token-"},
	{"gho_", "redacted-github-oauth-token-"},
	{"github_pat_", "redacted-github-pat-"},
	{"AKIA", "redacted-aws-key-"},
	{"xoxb-", "redacted-slack-bot-token-"},
	{"xoxp-", "redacted-slack-user-token-"},
	{"eyJhbGciOi", "redacted-jwt-algorithm-"},
	{"eyJ0eXAiOi", "redacted-jwt-type-"},
	{"-----BEGIN ", "redacted-pem-block "},
}

var pathForms = []func(string) string{
	func(slashed string) string { return strings.ReplaceAll(slashed, "/", `\\`) },
	func(slashed string) string { return strings.ReplaceAll(slashed, "/", `\`) },
	func(slashed string) string { return slashed },
	func(slashed string) string {
		if hasVolume(slashed) {
			return "/" + strings.ToLower(slashed[:1]) + slashed[2:]
		}
		return slashed
	},
	strings.NewReplacer(":", "-", "/", "-").Replace,
}

func Machine() Identities {
	home, _ := os.UserHomeDir()
	workdir, _ := os.Getwd()
	m := machine{home: filepath.ToSlash(home), workdir: filepath.ToSlash(workdir)}
	if current, err := user.Current(); err == nil {
		m.user = current.Username[strings.LastIndex(current.Username, `\`)+1:]
	}
	out, _ := exec.Command("git", "config", "--get-regexp", `^user\.(email|name)$`).Output()
	for line := range strings.Lines(string(out)) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch key {
		case "user.email":
			m.email = value
		case "user.name":
			m.gitName = value
		}
	}
	return identitiesOf(m)
}

func identitiesOf(m machine) Identities {
	var table Identities
	add := func(real, scrubbed string) {
		if real != "" && !slices.ContainsFunc(table, func(entry substitution) bool { return entry.real == real }) {
			table = append(table, substitution{real, scrubbed})
		}
	}
	projects := projectsRoot(m.home, m.workdir)
	for _, dir := range []substitution{{m.home, path.Dir(m.home) + "/owner"}, {projects, path.Dir(projects) + "/projects"}} {
		if dir.real == "" {
			continue
		}
		for _, form := range pathForms {
			add(form(dir.real), form(dir.scrubbed))
		}
	}
	add(m.email, "owner@example.com")
	add(m.gitName, "owner-handle")
	add(m.user, "owner")
	slices.SortStableFunc(table, func(a, b substitution) int { return len(b.real) - len(a.real) })
	return table
}

func projectsRoot(home, workdir string) string {
	if workdir == "" || home != "" && within(workdir, home) {
		return ""
	}
	prefix, rest := "", workdir
	if hasVolume(workdir) {
		prefix, rest = workdir[:2], workdir[2:]
	}
	for _, part := range strings.Split(strings.Trim(rest, "/"), "/") {
		prefix += "/" + part
		if part != "" && !within(home, prefix) {
			return prefix
		}
	}
	return ""
}

func within(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func hasVolume(slashed string) bool {
	return len(slashed) >= 2 && slashed[1] == ':'
}

func (identities Identities) Scrub(text string) string {
	for _, entry := range identities {
		text = strings.ReplaceAll(text, entry.real, entry.scrubbed)
	}
	for _, entry := range credentialSubstitutions {
		text = strings.ReplaceAll(text, entry.real, entry.scrubbed)
	}
	return text
}

func (identities Identities) LeaksIn(text string) []string {
	var found []string
	for _, entry := range identities {
		if strings.Contains(text, entry.real) {
			found = append(found, entry.real)
		}
	}
	return append(found, CredentialsIn(text)...)
}

func CredentialsIn(text string) []string {
	var found []string
	for _, entry := range credentialSubstitutions {
		if strings.Contains(text, entry.real) {
			found = append(found, entry.real)
		}
	}
	return found
}
