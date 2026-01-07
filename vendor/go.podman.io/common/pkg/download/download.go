package download

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// FromURL downloads the specified source to a file in tmpdir (OS defaults if
// empty).
func FromURL(tmpdir, source string) (string, error) {
	tmp, err := os.CreateTemp(tmpdir, "")
	if err != nil {
		return "", fmt.Errorf("creating temporary download file: %w", err)
	}
	defer tmp.Close()

	response, err := http.Get(source) // nolint:noctx
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", source, err)
	}
	defer response.Body.Close()

	_, err = io.Copy(tmp, response.Body)
	if err != nil {
		return "", fmt.Errorf("copying %s to %s: %w", source, tmp.Name(), err)
	}

	return tmp.Name(), nil
}
