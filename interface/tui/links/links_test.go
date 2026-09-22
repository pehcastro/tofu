package links

import (
	"encoding/json"
	"strings"
	"testing"

	isession "tofu/internal/session"
)

func TestWhereAURLStopsInProse(t *testing.T) {
	for _, one := range []struct {
		name  string
		text  string
		found []string
	}{
		{"a full stop", "read https://go.dev/doc.", []string{"https://go.dev/doc"}},
		{"a comma", "https://go.dev/doc, then the next one", []string{"https://go.dev/doc"}},
		{"a semicolon and a bang", "https://go.dev/a; https://go.dev/b!", []string{"https://go.dev/a", "https://go.dev/b"}},
		{"a closing bracket that opened outside", "(see https://go.dev/doc)", []string{"https://go.dev/doc"}},
		{"a markdown link", "[the docs](https://go.dev/doc)", []string{"https://go.dev/doc"}},
		{"a bracket the url owns", "https://en.wikipedia.org/wiki/Go_(programming_language)", []string{"https://en.wikipedia.org/wiki/Go_(programming_language)"}},
		{"a bracket the url owns, inside prose", "see https://en.wikipedia.org/wiki/Go_(programming_language).", []string{"https://en.wikipedia.org/wiki/Go_(programming_language)"}},
		{"a code fence", "```\ncurl https://go.dev/dl/go.tar.gz\n```", []string{"https://go.dev/dl/go.tar.gz"}},
		{"angle brackets", "<https://go.dev/doc>", []string{"https://go.dev/doc"}},
		{"a quoted url", `"https://go.dev/doc"`, []string{"https://go.dev/doc"}},
		{"a trailing quote", "he said https://go.dev/doc'", []string{"https://go.dev/doc"}},
		{"an upper case scheme", "HTTPS://GO.DEV/DOC", []string{"https://GO.DEV/DOC"}},
		{"a scheme inside a word", "xhttps://go.dev/doc", nil},
		{"a scheme with no host", "https:// and nothing", nil},
		{"plain http", "http://localhost:8080/health", []string{"http://localhost:8080/health"}},
	} {
		t.Run(one.name, func(t *testing.T) {
			found := Find(one.text)
			if strings.Join(found, " ") != strings.Join(one.found, " ") {
				t.Fatalf("%q found %q, want %q", one.text, found, one.found)
			}
		})
	}
}

func TestOnlyAWebDestinationIsShown(t *testing.T) {
	for _, refused := range []string{
		"javascript:alert(1)",
		"JavaScript:alert(document.cookie)",
		"file:///etc/passwd",
		"file://C:/Users/Luiz/.env",
		"mailto:someone@example.invalid",
		"data:text/html;base64,PHNjcmlwdD4=",
	} {
		if found := Find("open " + refused + " now"); found != nil {
			t.Errorf("%s was shown as %q", refused, found)
		}
		if shown, keep := safe(refused); keep {
			t.Errorf("%s passed the destination check as %q", refused, shown)
		}
	}
	for _, refused := range []string{"ftp://files.example.com/x", "ws://socket.example.com/x", "vscode://file/C:/secret"} {
		if shown, keep := safe(refused); keep {
			t.Errorf("%s passed the destination check as %q", refused, shown)
		}
	}
}

func TestACredentialShapedParameterIsCutAndTheHostAndPathStay(t *testing.T) {
	for _, one := range []struct {
		text string
		want string
	}{
		{"https://api.example.com/v1/runs?token=abc123", "https://api.example.com/v1/runs"},
		{"https://api.example.com/v1/runs?api_key=sk-live-1", "https://api.example.com/v1/runs"},
		{"https://api.example.com/p?account_id=7&X-Amz-Signature=deadbeef", "https://api.example.com/p"},
		{"https://api.example.com/p?blob=aaaaaaaaaaaaaaaaaaaaaaaaaaaa", "https://api.example.com/p"},
		{"https://user:hunter2@api.example.com/p", "https://api.example.com/p"},
		{"https://go.dev/search?q=links&page=2", "https://go.dev/search?q=links&page=2"},
	} {
		found := Find(one.text)
		if len(found) != 1 || found[0] != one.want {
			t.Errorf("%q became %q, want [%s]", one.text, found, one.want)
		}
		if strings.Contains(strings.Join(found, " "), "abc123") || strings.Contains(strings.Join(found, " "), "hunter2") {
			t.Errorf("%q kept the credential: %q", one.text, found)
		}
	}
}

func spoken(said ...isession.Utterance) isession.Conversation {
	return isession.Conversation{Session: "turn-1", Said: said}
}

func message(role, content string) isession.Utterance {
	return isession.Utterance{Role: role, Text: content}
}

func stepped(tool, command string) isession.Conversation {
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}
	said := isession.Utterance{Role: isession.RoleAssistant, Calls: []isession.Call{{Name: tool, Args: args}}}
	return isession.Conversation{Session: "turn-1", Said: []isession.Utterance{said}, FromSteps: true}
}

func TestTheSameURLThreeTimesIsOneEntryThatSaysThree(t *testing.T) {
	collected := Collect(spoken(
		message("user", "read https://go.dev/doc"),
		message("assistant", "https://go.dev/doc says this"),
		message("tool", "fetched https://go.dev/doc and https://go.dev/dl"),
	))
	if len(collected) != 2 {
		t.Fatalf("collected %d entries, want 2: %+v", len(collected), collected)
	}
	for _, one := range collected {
		want := 1
		if one.URL == "https://go.dev/doc" {
			want = 3
		}
		if one.Count != want {
			t.Errorf("%s counted %d, want %d", one.URL, one.Count, want)
		}
	}
}

func TestTheNewestSightingIsFirstAndNamesWhereItCameFrom(t *testing.T) {
	collected := Collect(spoken(
		message("user", "start at https://go.dev/a"),
		message("tool", "then https://go.dev/b"),
	))
	if len(collected) != 2 || collected[0].URL != "https://go.dev/b" {
		t.Fatalf("collected %+v, want the newest first", collected)
	}
	if collected[0].From != "a tool result" || collected[1].From != "you" {
		t.Errorf("the sources read %q and %q", collected[0].From, collected[1].From)
	}
}

func TestASystemPromptIsNotSomethingTheConversationCarried(t *testing.T) {
	if collected := Collect(spoken(message(isession.RoleSystem, "rules live at https://go.dev/rules"))); collected != nil {
		t.Fatalf("a system message put %+v in the picker", collected)
	}
}

func TestASessionRecordedBeforeMessagesWereKeptFallsBackToItsSteps(t *testing.T) {
	collected := Collect(stepped("bash", "curl -sL https://go.dev/dl/go1.25.1.tar.gz"))
	if len(collected) != 1 || collected[0].URL != "https://go.dev/dl/go1.25.1.tar.gz" || collected[0].From != "bash" {
		t.Fatalf("the steps of a session with no message event gave %+v", collected)
	}
}

func TestAnEmptySessionCarriesNoLink(t *testing.T) {
	if collected := Collect(isession.Conversation{}); collected != nil {
		t.Fatalf("an empty session gave %+v", collected)
	}
}
