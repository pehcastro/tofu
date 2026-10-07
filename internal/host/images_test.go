package host

import (
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/session"
)

func TestAPastedImageSurvivesAForkBeforeItIsSentAndIsTakenOnce(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{Dir: project})
	dir, err := h.AttachmentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flag.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Attached(1, "flag.png", 3, "PNG")
	h.mu.Lock()
	h.id = session.NewEventID()
	h.mu.Unlock()
	images, err := h.takePendingImages("what is this " + ImageToken(1))
	if err != nil || len(images) != 1 || string(images[0].Data) != "png" {
		t.Fatalf("the image pasted before a fork is lost when the message is sent after it: %d images, %v", len(images), err)
	}
	again, err := h.takePendingImages("and again " + ImageToken(1))
	if err != nil || len(again) != 0 {
		t.Fatalf("an image already sent is still pending: %d images, %v", len(again), err)
	}
}
