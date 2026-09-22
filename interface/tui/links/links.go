package links

import (
	"net/url"
	"strings"

	isession "tofu/internal/session"
)

type Link struct {
	URL   string
	From  string
	Count int
}

const (
	httpScheme  = "http://"
	httpsScheme = "https://"
	stops       = " \t\n\r\"`<>\\|"
	trimmed     = ",.;!'\""
	openers     = "([{"
	closers     = ")]}"
)

func Find(text string) []string {
	var found []string
	for at := 0; at < len(text); {
		start, begins := schemeAt(text, at)
		if !begins {
			break
		}
		candidate := run(text[start:])
		at = start + len(candidate)
		if one, keep := safe(candidate[:end(candidate)]); keep {
			found = append(found, one)
		}
	}
	return found
}

func schemeAt(text string, from int) (int, bool) {
	for at := from; at < len(text); at++ {
		if at > 0 && joined(text[at-1]) {
			continue
		}
		for _, scheme := range []string{httpsScheme, httpScheme} {
			if at+len(scheme) <= len(text) && strings.EqualFold(text[at:at+len(scheme)], scheme) {
				return at, true
			}
		}
	}
	return 0, false
}

func joined(before byte) bool {
	return before >= 'a' && before <= 'z' || before >= 'A' && before <= 'Z' ||
		before >= '0' && before <= '9' || strings.IndexByte("+-.:/", before) >= 0
}

func run(text string) string {
	for at := 0; at < len(text); at++ {
		if text[at] < ' ' || text[at] == 0x7f || strings.IndexByte(stops, text[at]) >= 0 {
			return text[:at]
		}
	}
	return text
}

func end(candidate string) int {
	var balance [len(closers)]int
	for at := 0; at < len(candidate); at++ {
		if opener := strings.IndexByte(openers, candidate[at]); opener >= 0 {
			balance[opener]++
		}
		if closer := strings.IndexByte(closers, candidate[at]); closer >= 0 {
			balance[closer]--
		}
	}
	last := len(candidate)
	for last > 0 {
		letter := candidate[last-1]
		closer := strings.IndexByte(closers, letter)
		if closer >= 0 {
			if balance[closer] >= 0 {
				return last
			}
			balance[closer]++
		} else if strings.IndexByte(trimmed, letter) < 0 {
			return last
		}
		last--
	}
	return last
}

const credentialValue = 24

func credentialWords() []string {
	return []string{"token", "key", "secret", "password", "passwd", "auth", "credential", "signature", "sig", "session", "access", "bearer", "code"}
}

func credentialed(query url.Values) bool {
	words := credentialWords()
	for name, values := range query {
		folded := strings.ToLower(name)
		for _, word := range words {
			if strings.Contains(folded, word) {
				return true
			}
		}
		for _, value := range values {
			if len(value) >= credentialValue {
				return true
			}
		}
	}
	return false
}

func safe(candidate string) (string, bool) {
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	parsed.User = nil
	if credentialed(parsed.Query()) {
		parsed.RawQuery = ""
	}
	return parsed.String(), true
}

type sighting struct {
	text string
	from string
}

func speaker(role string) (string, bool) {
	switch role {
	case isession.RoleUser:
		return "you", true
	case isession.RoleAssistant:
		return "the answer", true
	case isession.RoleTool:
		return "a tool result", true
	}
	return "", false
}

func Collect(talk isession.Conversation) []Link {
	var said []sighting
	for _, one := range talk.Said {
		who, shown := speaker(one.Role)
		if !shown {
			continue
		}
		said = append(said, sighting{text: one.Text, from: who})
		for _, call := range one.Calls {
			said = append(said, sighting{text: string(call.Args), from: call.Name})
		}
	}
	at := map[string]int{}
	var collected []Link
	for index := len(said) - 1; index >= 0; index-- {
		for _, one := range Find(said[index].text) {
			if seen, repeat := at[one]; repeat {
				collected[seen].Count++
				continue
			}
			at[one] = len(collected)
			collected = append(collected, Link{URL: one, From: said[index].from, Count: 1})
		}
	}
	return collected
}
