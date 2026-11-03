package browser

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"tofu/internal/konst"
)

func WriteMessage(w io.Writer, message []byte) error {
	if len(message) > konst.BrowserHostMessageBytes {
		return fmt.Errorf("native message of %d bytes is over Chrome's %d byte cap", len(message), konst.BrowserHostMessageBytes)
	}
	_, err := w.Write(append(binary.LittleEndian.AppendUint32(nil, uint32(len(message))), message...))
	return err
}

func ReadMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length > konst.BrowserExtensionMessageBytes {
		return nil, fmt.Errorf("native message of %d bytes is over the %d byte cap", length, konst.BrowserExtensionMessageBytes)
	}
	message := make([]byte, length)
	_, err := io.ReadFull(r, message)
	if errors.Is(err, io.EOF) {
		return nil, io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, err
	}
	return message, nil
}
