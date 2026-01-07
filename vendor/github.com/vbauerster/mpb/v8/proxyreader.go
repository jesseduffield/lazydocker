package mpb

import (
	"io"
	"time"
)

type proxyReader struct {
	io.ReadCloser
	bar *Bar
}

func (x proxyReader) Read(p []byte) (int, error) {
	n, err := x.ReadCloser.Read(p)
	x.bar.IncrBy(n)
	return n, err
}

type proxyWriterTo struct {
	proxyReader
}

func (x proxyWriterTo) WriteTo(w io.Writer) (int64, error) {
	n, err := x.ReadCloser.(io.WriterTo).WriteTo(w)
	x.bar.IncrInt64(n)
	return n, err
}

type ewmaProxyReader struct {
	io.ReadCloser
	bar *Bar
}

func (x ewmaProxyReader) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := x.ReadCloser.Read(p)
	x.bar.EwmaIncrBy(n, time.Since(start))
	return n, err
}

type ewmaProxyWriterTo struct {
	ewmaProxyReader
}

func (x ewmaProxyWriterTo) WriteTo(w io.Writer) (int64, error) {
	start := time.Now()
	n, err := x.ReadCloser.(io.WriterTo).WriteTo(w)
	x.bar.EwmaIncrInt64(n, time.Since(start))
	return n, err
}

func newProxyReader(r io.Reader, b *Bar, hasEwma bool) io.ReadCloser {
	rc := toReadCloser(r)
	if hasEwma {
		epr := ewmaProxyReader{rc, b}
		if _, ok := r.(io.WriterTo); ok {
			return ewmaProxyWriterTo{epr}
		}
		return epr
	}
	pr := proxyReader{rc, b}
	if _, ok := r.(io.WriterTo); ok {
		return proxyWriterTo{pr}
	}
	return pr
}

func toReadCloser(r io.Reader) io.ReadCloser {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}
	return io.NopCloser(r)
}
