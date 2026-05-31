package proto

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// MaxFrameBytes caps a single line-delimited JSON-RPC frame. stdio framing needs
// only a few MB; the limit keeps a runaway client from forcing unbounded buffering.
const MaxFrameBytes = 16 << 20

// ErrFrameTooLong is returned when an inbound frame exceeds MaxFrameBytes. The
// reader resynchronizes to the next newline so the session can keep serving.
var ErrFrameTooLong = errors.New("json-rpc frame exceeds maximum size")

// Transport reads line-delimited frames and writes serialized JSON values. A
// single mutex serializes writes so a slow write never interleaves output.
type Transport struct {
	reader *bufio.Reader
	out    io.Writer
	outMu  sync.Mutex
}

// NewTransport wires a transport over the given streams.
func NewTransport(in io.Reader, out io.Writer) *Transport {
	return &Transport{reader: bufio.NewReaderSize(in, 64*1024), out: out}
}

// ReadFrame returns the next frame without its newline, ErrFrameTooLong when the
// line exceeds MaxFrameBytes (after resyncing to the next line), or io.EOF.
func (t *Transport) ReadFrame() ([]byte, error) {
	return readLineLimited(t.reader, MaxFrameBytes)
}

// Write marshals v and writes it as one newline-terminated frame.
func (t *Transport) Write(v interface{}) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	t.outMu.Lock()
	defer t.outMu.Unlock()
	_, err = t.out.Write(body)
	return err
}

// readLineLimited reads up to and including the next newline. When the line
// would exceed maxBytes it discards the rest of the line and returns
// ErrFrameTooLong, leaving the reader positioned at the start of the next line.
func readLineLimited(r *bufio.Reader, maxBytes int) ([]byte, error) {
	var out []byte
	tooLong := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(out)+len(chunk) > maxBytes {
				tooLong = true
				out = nil
			} else {
				out = append(out, chunk...)
			}
		}
		switch {
		case err == nil:
			if tooLong {
				return nil, ErrFrameTooLong
			}
			return out, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if tooLong {
				return nil, ErrFrameTooLong
			}
			if len(out) > 0 {
				return out, nil
			}
			return nil, io.EOF
		default:
			return nil, err
		}
	}
}
