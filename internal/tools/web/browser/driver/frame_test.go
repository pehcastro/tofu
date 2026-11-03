package browser

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestFrameReadsTwoMessagesBackToBackFromOneStream(t *testing.T) {
	var stream bytes.Buffer
	sent := [][]byte{[]byte(`{"t":"hello","tabs":[]}`), []byte(`{"t":"result","id":7,"text":"café"}`)}
	for _, message := range sent {
		if err := WriteMessage(&stream, message); err != nil {
			t.Fatal(err)
		}
	}
	if prefix := stream.Bytes()[:4]; !bytes.Equal(prefix, []byte{byte(len(sent[0])), 0, 0, 0}) {
		t.Fatalf("length prefix %v is not %d as 32-bit little-endian", prefix, len(sent[0]))
	}
	for _, want := range sent {
		got, err := ReadMessage(&stream)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("read %q, %v; want %q", got, err, want)
		}
	}
	if got, err := ReadMessage(&stream); !errors.Is(err, io.EOF) {
		t.Fatalf("an exhausted stream read %q, %v; want io.EOF", got, err)
	}
}

func TestFrameRefusesAWriteOverOneMegabyte(t *testing.T) {
	var stream bytes.Buffer
	if err := WriteMessage(&stream, make([]byte, 1<<20+1)); err == nil {
		t.Fatal("a 1 MB + 1 message was written")
	}
	if stream.Len() != 0 {
		t.Fatalf("a refused write put %d bytes on the stream", stream.Len())
	}
	if err := WriteMessage(&stream, make([]byte, 1<<20)); err != nil {
		t.Fatalf("a message of exactly 1 MB was refused: %v", err)
	}
}

func TestFrameRefusesAReadOverSixtyFourMebibytes(t *testing.T) {
	prefix := binary.LittleEndian.AppendUint32(nil, 64<<20+1)
	if got, err := ReadMessage(bytes.NewReader(prefix)); err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a 64 MiB + 1 length read %d bytes, %v; want a refusal before the body", len(got), err)
	}
}

func TestFrameFailsOnAStreamCutMidLengthAndMidBody(t *testing.T) {
	var whole bytes.Buffer
	if err := WriteMessage(&whole, []byte(`{"t":"unshared"}`)); err != nil {
		t.Fatal(err)
	}
	for _, cut := range []int{2, 4, 9} {
		if got, err := ReadMessage(bytes.NewReader(whole.Bytes()[:cut])); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("a stream cut after %d bytes read %q, %v; want io.ErrUnexpectedEOF", cut, got, err)
		}
	}
}
