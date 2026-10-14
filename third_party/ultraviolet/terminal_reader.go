package uv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/cancelreader"
	"github.com/rivo/uniseg"
)

var ErrReaderNotStarted = fmt.Errorf("reader not started")

const DefaultEscTimeout = 50 * time.Millisecond

type TerminalReader struct {
	EventDecoder

	MouseMode *MouseMode

	EscTimeout time.Duration

	r     io.Reader
	table map[string]Key

	term string

	paste []byte

	lookup bool

	vtInput bool

	eventScanner *eventScanner

	utf16Half   [2]bool
	utf16Buf    [2][2]rune
	graphemeBuf [2][]rune

	lastMouseBtns uint32

	lastWinsizeX, lastWinsizeY int16

	logger Logger
}

func NewTerminalReader(r io.Reader, termType string) *TerminalReader {
	d := &TerminalReader{
		EscTimeout: DefaultEscTimeout,
		r:          r,
		term:       termType,
		lookup:     true,
	}
	d.r = r
	if d.table == nil {
		d.table = buildKeysTable(d.Legacy, d.term, d.UseTerminfo)
	}
	evs := newEventScanner()
	evs.EventDecoder = d.EventDecoder
	evs.table = d.table
	evs.lookup = d.lookup
	evs.setLogger(d.logger)
	d.eventScanner = evs
	return d
}

const readBufSize = 4096

func (d *TerminalReader) sendBytes(ctx context.Context, readc chan []byte) error {
	for {
		var readBuf [readBufSize]byte
		n, err := d.r.Read(readBuf[:])
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case readc <- readBuf[:n]:
		}
	}
}

func (d *TerminalReader) StreamEvents(ctx context.Context, eventc chan<- Event) error {
	var buf bytes.Buffer
	errc := make(chan error, 1)
	readc := make(chan []byte)
	timeout := time.NewTimer(d.EscTimeout)
	ttimeout := time.Now().Add(d.EscTimeout)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := d.streamData(ctx, readc); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, cancelreader.ErrCanceled) {
				errc <- nil
				return
			}
			errc <- err
			return
		}
	}()

	for {
		select {
		case <-ctx.Done():
			d.sendEvents(eventc, buf.Bytes(), true)
			wg.Wait()
			return nil
		case err := <-errc:
			d.sendEvents(eventc, buf.Bytes(), true)
			wg.Wait()
			return err
		case <-timeout.C:
			d.logf("timeout reached")

			var n int
			timedout := time.Now().After(ttimeout)
			if buf.Len() > 0 && timedout {
				d.logf("timeout expired, processing buffer")
				n = d.sendEvents(eventc, buf.Bytes(), true)
			}

			if n > 0 {
				buf.Next(n)
			}

			if buf.Len() > 0 {
				if !timeout.Stop() {

					select {
					case <-timeout.C:
					default:
					}
				}

				d.logf("resetting timeout for remaining buffer")
				timeout.Reset(d.EscTimeout)
			}

		case read := <-readc:
			d.logf("input: %q", read)
			buf.Write(read)
			ttimeout = time.Now().Add(d.EscTimeout)
			n := d.sendEvents(eventc, buf.Bytes(), false)
			if !timeout.Stop() {

				select {
				case <-timeout.C:
				default:
				}
			}

			if n > 0 {
				d.logf("processed %d bytes from buffer", n)
				buf.Next(n)
			}

			if buf.Len() > 0 {
				d.logf("resetting timeout for remaining buffer after parse")
				timeout.Reset(d.EscTimeout)
			}
		}
	}
}

func (d *TerminalReader) SetLogger(logger Logger) {
	d.logger = logger
}

func (d *TerminalReader) sendEvents(eventc chan<- Event, buf []byte, expired bool) int {
	n, events := d.eventScanner.scanEvents(buf, expired)
	for _, event := range events {
		eventc <- event
	}
	return n
}

type eventScanner struct {
	EventDecoder

	utf16Half   [2]bool
	utf16Buf    [2][2]rune
	graphemeBuf [2][]rune
	paste       []byte
	table       map[string]Key
	lookup      bool
	logger      Logger
}

func newEventScanner() *eventScanner {
	return &eventScanner{}
}

func (d *eventScanner) setLogger(logger Logger) {
	d.logger = logger
}

func (d *eventScanner) logf(format string, v ...interface{}) {
	logf(d.logger, format, v...)
}

func (d *eventScanner) scanEvents(buf []byte, expired bool) (total int, events []Event) {
	if len(buf) == 0 {
		return 0, nil
	}

	var dn int
	d.logf("processing buf %d %q", len(buf), buf)
	dn, buf = d.deserializeWin32Input(buf)
	total += dn

	if d.lookup && len(buf) > 2 && buf[0] == ansi.ESC {
		if k, ok := d.table[string(buf)]; ok {
			return len(buf), []Event{KeyPressEvent(k)}
		}
	}

	for len(buf) > 0 {
		esc := buf[0] == ansi.ESC
		n, event := d.Decode(buf)

		if d.paste != nil {
			if _, ok := event.(PasteEndEvent); !ok {
				switch event := event.(type) {
				case KeyPressEvent:
					if len(event.Text) > 0 {
						d.paste = append(d.paste, event.Text...)
					} else {
						seq := string(buf[:n])
						isWin32 := strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "_")
						switch {
						case isWin32 && event.Code == KeyEnter && event.Code == event.BaseCode:

							d.paste = append(d.paste, '\n')
						case isWin32 && unicode.IsControl(event.Code) && event.Code == event.BaseCode:

							d.paste = append(d.paste, string(event.Code)...)
						case !isWin32:

							if esc && n <= 2 && !expired {

								return total, events
							}
							d.paste = append(d.paste, seq...)
						}
					}
				case UnknownEvent:
					if !expired {

						return total, events
					}
				default:

				}
				buf = buf[n:]
				total += n
				continue
			}
		}

		var isUnknown bool
		switch event.(type) {
		case ignoredEvent:

			event = nil
		case UnknownEvent:
			isUnknown = true

			if !expired {
				return total, events
			}

			if k, ok := d.table[string(buf[:n])]; ok {
				events = append(events, KeyPressEvent(k))
				return total + n, events
			}

			events = append(events, event)
		case PasteStartEvent:
			d.paste = []byte{}
		case PasteEndEvent:
			var paste []rune
			for len(d.paste) > 0 {
				r, w := utf8.DecodeRune(d.paste)
				if r != utf8.RuneError {
					paste = append(paste, r)
				}
				d.paste = d.paste[w:]
			}
			d.paste = nil
			events = append(events, PasteEvent{string(paste)})
		}

		if !isUnknown && event != nil {
			if esc && n <= 2 && !expired {

				return total, events
			}

			if m, ok := event.(MultiEvent); ok {

				events = append(events, m...)
			} else {

				events = append(events, event)
			}
		}

		buf = buf[n:]
		total += n
	}

	return total, events
}

func (d *eventScanner) encodeGraphemeBufs() []byte {
	var b []byte
	for kd := range d.graphemeBuf {
		if len(d.graphemeBuf[kd]) > 0 {
			switch kd {
			case 1:
				b = append(b, string(d.graphemeBuf[kd])...)
			case 0:

				grs := uniseg.NewGraphemes(string(d.graphemeBuf[kd]))
				for grs.Next() {
					var codepoints string
					gr := grs.Str()
					for i, r := range gr {
						if r == 0 {
							continue
						}
						if i > 0 {
							codepoints += ":"
						}
						codepoints += strconv.FormatInt(int64(r), 10)
					}

					seq := fmt.Sprintf("\x1b[%d;1:3;%su", d.graphemeBuf[kd][0], codepoints)
					b = append(b, seq...)
				}
			}
			d.graphemeBuf[kd] = d.graphemeBuf[kd][:0]
		}
	}
	return b
}

func (d *eventScanner) storeGraphemeRune(kd int, r rune) {
	if d.utf16Half[kd] {

		d.utf16Half[kd] = false
		d.utf16Buf[kd][1] = r
		r := utf16.DecodeRune(d.utf16Buf[kd][0], d.utf16Buf[kd][1])
		d.graphemeBuf[kd] = append(d.graphemeBuf[kd], r)
	} else if utf16.IsSurrogate(r) {

		d.utf16Half[kd] = true
		d.utf16Buf[kd][0] = r
	} else {

		d.graphemeBuf[kd] = append(d.graphemeBuf[kd], r)
	}
}

func (d *eventScanner) deserializeWin32Input(buf []byte) (int, []byte) {
	p := ansi.GetParser()
	defer ansi.PutParser(p)

	var processed int
	var state byte
	des := make([]byte, 0, len(buf))

	for len(buf) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(buf, state, p)
		switch width {
		case 0:
			if p.Command() == '_' {
				vk, _ := p.Param(0, 0)
				if vk == 0 {

					uc, _ := p.Param(2, 0)
					kd, _ := p.Param(3, 0)
					kd = clamp(kd, 0, 1)
					d.storeGraphemeRune(kd, rune(uc))
					processed += n
					break
				}
			}
			fallthrough
		default:
			des = append(des, d.encodeGraphemeBufs()...)
			des = append(des, seq...)
		}

		state = newState
		buf = buf[n:]
	}

	des = append(des, d.encodeGraphemeBufs()...)

	return processed, des
}

func (d *TerminalReader) logf(format string, v ...interface{}) {
	logf(d.logger, format, v...)
}

func logf(logger Logger, format string, v ...interface{}) {
	if logger == nil {
		return
	}
	logger.Printf(format, v...)
}
