package multiwriter

import "io"

type Multiwriter struct {
	Writer []io.Writer
	w      io.Writer
}

func New() *Multiwriter {
	return &Multiwriter{}
}

func (mw *Multiwriter) AddWriter(writer io.Writer) error {
	mw.Writer = append(mw.Writer, writer)

	mw.w = io.MultiWriter(mw.Writer...)

	return nil
}

func (mw *Multiwriter) Write(b []byte) (int, error) {
	return mw.w.Write(b)
}

func (mw *Multiwriter) Close() error {
	return nil
}
