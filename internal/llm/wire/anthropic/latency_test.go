package anthropic

import (
	"io"
	"strings"
	"testing"
	"time"
)

type pacedReader struct {
	chunks [][]byte
	delays []time.Duration
	index  int
	held   []byte
}

func (r *pacedReader) Read(into []byte) (int, error) {
	for len(r.held) == 0 {
		if r.index >= len(r.chunks) {
			return 0, io.EOF
		}
		time.Sleep(r.delays[r.index])
		r.held = r.chunks[r.index]
		r.index++
	}
	n := copy(into, r.held)
	r.held = r.held[n:]
	return n, nil
}

func TestAStreamedResponseDrawsItsFirstWordsLongBeforeTheStepCompletes(t *testing.T) {
	const (
		timeToFirstByte  = 700 * time.Millisecond
		timeBetweenWords = 22 * time.Millisecond
		wordCount        = 90
	)
	sentence := strings.Fields("the gate reads toolgate.go before the policy decides and the tool never runs unjudged")
	words := make([]string, wordCount)
	for index := range words {
		words[index] = sentence[index%len(sentence)] + " "
	}

	events := []string{eventMessageStart, eventTextStart}
	for _, word := range words {
		events = append(events, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+word+`"}}`)
	}
	events = append(events, eventTextStop, eventStopTurn, eventStop)

	chunks := make([][]byte, len(events))
	delays := make([]time.Duration, len(events))
	delays[0] = timeToFirstByte
	for index, event := range events {
		chunks[index] = []byte("event: x\ndata: " + event + "\n\n")
		if index > 0 {
			delays[index] = timeBetweenWords
		}
	}

	start := time.Now()
	var firstDelta time.Duration
	result, err := ReadStream(&pacedReader{chunks: chunks, delays: delays}, true, func(string) {
		if firstDelta == 0 {
			firstDelta = time.Since(start)
		}
	})
	completed := time.Since(start)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Content == "" {
		t.Fatal("the paced stream produced no content")
	}
	if firstDelta == 0 {
		t.Fatal("no delta ever arrived")
	}
	if firstDelta >= completed {
		t.Fatalf("the first delta arrived at %s, no sooner than the completed step at %s", firstDelta, completed)
	}

	t.Logf("paced at %s to first byte and %s between words (constructed: no streamed turn has ever "+
		"been recorded here to draw real timings from; the rate approximates published Claude streaming throughput):",
		timeToFirstByte, timeBetweenWords)
	t.Logf("today, the interface draws nothing until the step completes: %s", completed)
	t.Logf("with this change, the interface draws its first delta at: %s", firstDelta)
	t.Logf("the wait before something is drawn falls by %s, %.0f%% of the step", completed-firstDelta,
		100*float64(completed-firstDelta)/float64(completed))
}
