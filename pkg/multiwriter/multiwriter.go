package multiwriter

import (
	"io"
	"sync"
)

type MultiWriter struct {
	writers []io.Writer
	m       sync.Mutex
}

func NewMultiWriter() *MultiWriter {
	return &MultiWriter{}
}

func (mw *MultiWriter) AddWriter(w io.Writer) {
	mw.m.Lock()
	defer mw.m.Unlock()

	mw.writers = append(mw.writers, w)
}

func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	mw.m.Lock()
	defer mw.m.Unlock()

	return io.MultiWriter(mw.writers...).Write(p)
}

func (mw *MultiWriter) Close() error {
	return nil
}
