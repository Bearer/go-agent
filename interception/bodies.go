package interception

import (
	"io"
	"io/ioutil"
	"net/http"

	"github.com/bearer/go-agent/events"
	"github.com/bearer/go-agent/proxy"
)

// BodyReadCloser wraps a io.ReadCloser to give access to the first peekSize
// bytes without interfering with the normal behaviour
type BodyReadCloser struct {
	peekSize   int
	peekBuffer []byte
	peekError  error
	pos        int
	readCloser io.ReadCloser
}

// NewBodyReadCloser constructs a BodyReadCloser wrapper
func NewBodyReadCloser(readCloser io.ReadCloser, peekSize int) *BodyReadCloser {
	return &BodyReadCloser{
		readCloser: readCloser,
		pos:        0,
		peekSize:   peekSize,
	}
}

// Read gives the usual io.Reader behaviour
func (r *BodyReadCloser) Read(p []byte) (int, error) {
	if r.peekBuffer == nil || r.pos < len(r.peekBuffer) {
		r.ensurePeekBuffer()
		to := r.pos + len(p)
		if to > len(r.peekBuffer) {
			to = len(r.peekBuffer)
		}
		peekN := copy(p, r.peekBuffer[r.pos:to])
		r.pos += peekN

		// Only return EOF when we've read past the peeked position
		if r.peekError != nil && (r.peekError != io.EOF || r.pos >= len(r.peekBuffer)) {
			return peekN, r.peekError
		}

		// Read beyond the peek buffer if neccessary
		if peekN < len(p) {
			n, err := r.readCloser.Read(p[peekN:])
			r.pos += n
			return peekN + n, err
		}

		return peekN, nil
	}

	return r.readCloser.Read(p)
}

// Peek returns the result of reading the first peek bytes block
func (r *BodyReadCloser) Peek() ([]byte, error) {
	r.ensurePeekBuffer()
	return r.peekBuffer, r.peekError
}

func (r *BodyReadCloser) ensurePeekBuffer() {
	if r.peekBuffer != nil {
		return
	}

	buffer := make([]byte, r.peekSize)
	n, err := io.ReadFull(r.readCloser, buffer)
	r.peekBuffer = buffer[:n]

	r.peekError = err
	if err == io.ErrUnexpectedEOF {
		r.peekError = io.EOF
	}
}

// Close closes the underlying io.ReadCloser
func (r *BodyReadCloser) Close() error {
	return r.readCloser.Close()
}

// BodyParsingProvider is an events.Listener provider returning listeners
// performing data collection, hashing, and sanitization on request/reponse
// bodies.
type BodyParsingProvider struct{}

// Listeners implements events.ListenerProvider.
func (p BodyParsingProvider) Listeners(e events.Event) (l []events.Listener) {
	switch e.Topic() {
	case TopicBodies:
		l = []events.Listener{
			p.RequestBodyParser,
			p.ResponseBodyParser,
		}
	}
	return
}

// ParseFormData parses form data
func ParseFormData(reader io.Reader) (map[string][]string, error) {
	request := &http.Request{Method: `POST`, Body: ioutil.NopCloser(reader), Header: make(http.Header)}
	request.Header.Set(proxy.ContentTypeHeader, proxy.ContentTypeSimpleForm)

	err := request.ParseForm()
	return request.Form, err
}
