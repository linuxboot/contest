package buffer

import (
	"bytes"
)

// Buffer is a struct that implements the io.ReadCloser and io.WriteCloser interface.
type Buffer struct {
	buf bytes.Buffer
}

// New returns a new Buffer.
func New() *Buffer {
	return &Buffer{}
}

// Read implements the io.ReadCloser interface.
func (b *Buffer) Read(p []byte) (n int, err error) {
	return b.buf.Read(p)
}

// Write implements the io.WriteCloser interface.
func (b *Buffer) Write(p []byte) (n int, err error) {
	return b.buf.Write(p)
}

// Close implements the io.Closer interface.
func (b *Buffer) Close() error {
	// b.buf.Reset()
	return nil
}
