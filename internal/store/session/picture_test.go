package session

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func pictureStore(t *testing.T) (*Store, *Log, []byte, string) {
	data := bytes.Repeat([]byte("a screenshot of a page with text on it "), 400)
	secret := base64.StdEncoding.EncodeToString(data)[120:160]
	store := OpenAt(t.TempDir())
	store.Redacts(func() sys.KeyRedactor { return sys.KeyRedactorOf(map[string]string{"SHOT_KEY": secret}) })
	log, err := store.Open(Header{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return store, log, data, secret
}

func TestARequestBodyHoldsEachPictureOnceAndAssemblesWhole(t *testing.T) {
	store, log, data, secret := pictureStore(t)
	picture, err := log.KeepPicture("image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	picture.Data = data
	encoded := base64.StdEncoding.EncodeToString(data)
	image := `{"type":"image_url","image_url":{"url":"data:image/png;base64,` + encoded + `"}}`
	body := `{"model":"m","messages":[{"role":"user","content":[` + image + `,{"type":"text","text":"the key is ` + secret + `"}]},{"role":"user","content":[` + image + `]}]}`
	parts, err := log.KeepBody([]byte(body), []Picture{picture})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Dir(log.ID()), blobsName))
	if err != nil {
		t.Fatal(err)
	}
	if held := bytes.Count(raw, []byte(encoded)); held != 1 {
		t.Errorf("blobs.jsonl holds the picture %d times, want once", held)
	}
	blobs, err := store.Blobs(log.ID())
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(body, "the key is "+secret, "the key is "+sys.KeyRedactedMark, 1)
	if got := string(Assemble(parts, blobs)); got != want {
		t.Errorf("the body assembles to %d bytes, want the %d sent with only the key in its text masked", len(got), len(want))
	}
}

func TestSaidResumesFromTheNewestListChangeWithItsPictures(t *testing.T) {
	store, log, data, _ := pictureStore(t)
	picture, err := log.KeepPicture("image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	keep := func(body MessageBody) string {
		hash, err := log.Keep(body)
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	appendEvent := func(kind EventKind, body any) {
		if _, err := log.Append(Event{Kind: kind}, body); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent(EventMessage, MessageBody{Role: RoleUser, Content: "before every change"})
	appendEvent(EventListChange, ListChangeBody{Kind: "compaction", After: []string{keep(MessageBody{Role: RoleUser, Content: "an older change"})}})
	appendEvent(EventListChange, ListChangeBody{Kind: "pictures dropped", After: []string{
		keep(MessageBody{Role: RoleSystem, Content: "the system prompt"}),
		keep(MessageBody{Role: RoleTool, ToolCallID: "c1", Content: "a kept picture", Pictures: []Picture{picture}}),
	}})
	appendEvent(EventMessage, MessageBody{Role: RoleUser, Content: "after the change"})
	appendEvent(EventToolResult, ResultBody{Content: "an old result with no pictures field"})
	events, err := store.Events(log.ID())
	if err != nil {
		t.Fatal(err)
	}
	said, _, err := store.Said(log.ID(), events)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, event := range said {
		var message MessageBody
		if err := json.Unmarshal(event.Body, &message); err != nil {
			t.Fatal(err)
		}
		got = append(got, message.Content)
		if message.Content == "a kept picture" && (len(message.Pictures) != 1 || !bytes.Equal(message.Pictures[0].Data, data)) {
			t.Errorf("the kept picture comes back as %d pictures, want its %d bytes", len(message.Pictures), len(data))
		}
	}
	if want := "a kept picture|after the change|an old result with no pictures field"; strings.Join(got, "|") != want {
		t.Errorf("said is %q, want %q", strings.Join(got, "|"), want)
	}
	appendEvent(EventListChange, ListChangeBody{Kind: ListChangeFork, After: []string{keep(MessageBody{Role: RoleUser, Content: "the fork's own list"})}})
	appendEvent(EventMessage, MessageBody{Role: RoleUser, Content: "written after the fork"})
	events, _ = store.Events(log.ID())
	if said, _, err = store.Said(log.ID(), events); err != nil || len(said) != 1 {
		t.Errorf("after a fork change said is %d messages and %v, want the one written after it", len(said), err)
	}
}
