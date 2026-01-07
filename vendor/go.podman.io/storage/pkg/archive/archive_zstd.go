package archive

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

type wrapperZstdDecoder struct {
	decoder *zstd.Decoder
}

func (w *wrapperZstdDecoder) Close() error {
	w.decoder.Close()
	return nil
}

func (w *wrapperZstdDecoder) DecodeAll(input, dst []byte) ([]byte, error) {
	return w.decoder.DecodeAll(input, dst)
}

func (w *wrapperZstdDecoder) Read(p []byte) (int, error) {
	return w.decoder.Read(p)
}

func (w *wrapperZstdDecoder) Reset(r io.Reader) error {
	return w.decoder.Reset(r)
}

func (w *wrapperZstdDecoder) WriteTo(wr io.Writer) (int64, error) {
	return w.decoder.WriteTo(wr)
}

func zstdReader(buf io.Reader) (io.ReadCloser, error) {
	decoder, err := zstd.NewReader(buf)
	return &wrapperZstdDecoder{decoder: decoder}, err
}

func zstdWriter(dest io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(dest)
}
