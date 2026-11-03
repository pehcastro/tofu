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
	isPath   bool
}

type Identities []substitution

type machine struct {
	home, user, email, gitName, workdir string
}

var credentialSubstitutions = []substitution{
	{real: "sk-or-v1-", scrubbed: "redacted-openrouter-key-"},
	{real: "sk-ant-api", scrubbed: "redacted-anthropic-api-key"},
	{real: "sk-ant-oat", scrubbed: "redacted-anthropic-oauth-token"},
	{real: "sk-proj-", scrubbed: "redacted-openai-key-"},
	{real: "ghp_", scrubbed: "redacted-github-token-"},
	{real: "gho_", scrubbed: "redacted-github-oauth-token-"},
	{real: "github_pat_", scrubbed: "redacted-github-pat-"},
	{real: "AKIA", scrubbed: "redacted-aws-key-"},
	{real: "xoxb-", scrubbed: "redacted-slack-bot-token-"},
	{real: "xoxp-", scrubbed: "redacted-slack-user-token-"},
	{real: "eyJhbGciOi", scrubbed: "redacted-jwt-algorithm-"},
	{real: "eyJ0eXAiOi", scrubbed: "redacted-jwt-type-"},
	{real: "-----BEGIN ", scrubbed: "redacted-pem-block "},
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
	add := func(entry substitution) {
		if entry.real != "" && !slices.ContainsFunc(table, func(seen substitution) bool { return seen.real == entry.real }) {
			table = append(table, entry)
		}
	}
	projects := projectsRoot(m.home, m.workdir)
	for _, dir := range []substitution{{real: m.home, scrubbed: path.Dir(m.home) + "/owner"}, {real: projects, scrubbed: path.Dir(projects) + "/projects"}} {
		if dir.real == "" {
			continue
		}
		for _, form := range pathForms {
			add(substitution{form(dir.real), form(dir.scrubbed), true})
		}
	}
	add(substitution{real: m.email, scrubbed: "owner@example.com"})
	add(substitution{real: m.gitName, scrubbed: "owner-handle"})
	add(substitution{real: m.user, scrubbed: "owner"})
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

func (entry substitution) replaceIn(text string) string {
	if !entry.isPath {
		return strings.ReplaceAll(text, entry.real, entry.scrubbed)
	}
	var out strings.Builder
	for {
		before, after, found := strings.Cut(text, entry.real)
		out.WriteString(before)
		if !found {
			return out.String()
		}
		if after != "" && continuesName(after[0]) {
			out.WriteString(entry.real)
		} else {
			out.WriteString(entry.scrubbed)
		}
		text = after
	}
}

func continuesName(next byte) bool {
	return next == '_' || '0' <= next && next <= '9' || 'a' <= next && next <= 'z' || 'A' <= next && next <= 'Z'
}

func (identities Identities) Scrub(text string) string {
	for _, entry := range slices.Concat(identities, credentialSubstitutions) {
		text = entry.replaceIn(text)
	}
	return text
}

func (identities Identities) LeaksIn(text string) []string {
	var found []string
	for _, entry := range identities {
		if entry.replaceIn(text) != text {
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
