package theme

import (
	"testing"

	"charm.land/glamour/v2/styles"
)

func TestProseFlushesTheGutterGlamourWouldIndentEveryMessageBy(t *testing.T) {
	shipped, ours := styles.DarkStyleConfig.Document.Margin, Prose().Document.Margin
	if ours == nil {
		t.Fatal("Prose leaves the document margin unset, so every rendered message sits in a gutter")
	}
	if *ours != 0 {
		t.Fatalf("Prose leaves a document margin of %d, so every rendered message sits in a gutter", *ours)
	}
	if shipped == nil || *shipped == 0 {
		t.Error("glamour now ships a flush document, so Prose flushing it proves nothing")
	}
}
