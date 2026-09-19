package codex

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"boji/internal/transport"
)

type Identity struct {
	InstallationID string
	SessionID      string
	ThreadID       string
	WindowID       string
	TurnID         string
}

type turnMetadata struct {
	InstallationID string `json:"installation_id"`
	SessionID      string `json:"session_id"`
	ThreadID       string `json:"thread_id"`
	TurnID         string `json:"turn_id"`
	WindowID       string `json:"window_id"`
	RequestKind    string `json:"request_kind"`
}

func (i Identity) filled() (Identity, error) {
	for _, slot := range []*string{&i.SessionID, &i.ThreadID, &i.WindowID, &i.TurnID} {
		if *slot != "" {
			continue
		}
		generated, err := randomUUID()
		if err != nil {
			return Identity{}, err
		}
		*slot = generated
	}
	return i, nil
}

func (i Identity) TurnMetadata() (string, map[string]string, error) {
	encoded, err := asciiJSON(turnMetadata{
		InstallationID: i.InstallationID,
		SessionID:      i.SessionID,
		ThreadID:       i.ThreadID,
		TurnID:         i.TurnID,
		WindowID:       i.WindowID,
		RequestKind:    RequestKindTurn,
	})
	if err != nil {
		return "", nil, err
	}
	if len(encoded) > TurnMetadataHeaderCap {
		return "", nil, transport.Fail("codex.TurnMetadata", transport.KindRequestTooLarge, nil,
			"the turn metadata header is %d bytes and the backend caps it at %d",
			len(encoded), TurnMetadataHeaderCap)
	}
	return encoded, map[string]string{
		HeaderInstallationID: i.InstallationID,
		"session_id":         i.SessionID,
		"thread_id":          i.ThreadID,
		HeaderWindowID:       i.WindowID,
		"turn_id":            i.TurnID,
		HeaderTurnMetadata:   encoded,
	}, nil
}

func asciiJSON(metadata turnMetadata) (string, error) {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", transport.Fail("codex.TurnMetadata", transport.KindBadRequest, err, "encoding the turn metadata")
	}
	var out strings.Builder
	for _, char := range string(encoded) {
		if char < 0x7f {
			out.WriteRune(char)
			continue
		}
		for _, unit := range utf16.Encode([]rune{char}) {
			_, _ = fmt.Fprintf(&out, "\\u%04x", unit)
		}
	}
	return out.String(), nil
}

func randomUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", transport.Fail("codex.Identity", transport.KindBadRequest, err, "drawing an identifier")
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}
