//go:build unix

package dump

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"time"

	"github.com/opencontainers/go-digest"
	"go.podman.io/storage/pkg/chunked/internal/minimal"
	storagePath "go.podman.io/storage/pkg/chunked/internal/path"
	"golang.org/x/sys/unix"
)

const (
	ESCAPE_STANDARD = 0
	NOESCAPE_SPACE  = 1 << iota
	ESCAPE_EQUAL
	ESCAPE_LONE_DASH
)

func escaped(val []byte, escape int) string {
	noescapeSpace := escape&NOESCAPE_SPACE != 0
	escapeEqual := escape&ESCAPE_EQUAL != 0
	escapeLoneDash := escape&ESCAPE_LONE_DASH != 0

	if escapeLoneDash && len(val) == 1 && val[0] == '-' {
		return fmt.Sprintf("\\x%.2x", val[0])
	}

	// This is intended to match the C isprint API with LC_CTYPE=C
	isprint := func(c byte) bool {
		return c >= 32 && c < 127
	}
	// This is intended to match the C isgraph API with LC_CTYPE=C
	isgraph := func(c byte) bool {
		return c > 32 && c < 127
	}

	var result string
	for _, c := range val {
		hexEscape := false
		var special string

		switch c {
		case '\\':
			special = "\\\\"
		case '\n':
			special = "\\n"
		case '\r':
			special = "\\r"
		case '\t':
			special = "\\t"
		case '=':
			hexEscape = escapeEqual
		default:
			if noescapeSpace {
				hexEscape = !isprint(c)
			} else {
				hexEscape = !isgraph(c)
			}
		}

		if special != "" {
			result += special
		} else if hexEscape {
			result += fmt.Sprintf("\\x%.2x", c)
		} else {
			result += string(c)
		}
	}
	return result
}

func escapedOptional(val []byte, escape int) string {
	if len(val) == 0 {
		return "-"
	}
	return escaped(val, escape)
}

func getStMode(mode uint32, typ string) (uint32, error) {
	switch typ {
	case minimal.TypeReg, minimal.TypeLink:
		mode |= unix.S_IFREG
	case minimal.TypeChar:
		mode |= unix.S_IFCHR
	case minimal.TypeBlock:
		mode |= unix.S_IFBLK
	case minimal.TypeDir:
		mode |= unix.S_IFDIR
	case minimal.TypeFifo:
		mode |= unix.S_IFIFO
	case minimal.TypeSymlink:
		mode |= unix.S_IFLNK
	default:
		return 0, fmt.Errorf("unknown type %s", typ)
	}
	return mode, nil
}

func dumpNode(out io.Writer, added map[string]*minimal.FileMetadata, links map[string]int, verityDigests map[string]string, entry *minimal.FileMetadata) error {
	path := storagePath.CleanAbsPath(entry.Name)

	parent := filepath.Dir(path)
	if _, found := added[parent]; !found && path != "/" {
		parentEntry := &minimal.FileMetadata{
			Name: parent,
			Type: minimal.TypeDir,
			Mode: 0o755,
		}
		if err := dumpNode(out, added, links, verityDigests, parentEntry); err != nil {
			return err
		}

	}
	if e, found := added[path]; found {
		// if the entry was already added, make sure it has the same data
		if !reflect.DeepEqual(*e, *entry) {
			return fmt.Errorf("entry %q already added with different data", path)
		}
		return nil
	}
	added[path] = entry

	if _, err := fmt.Fprint(out, escaped([]byte(path), ESCAPE_STANDARD)); err != nil {
		return err
	}

	nlinks := links[entry.Name] + links[entry.Linkname] + 1
	link := ""
	if entry.Type == minimal.TypeLink {
		link = "@"
	}

	rdev := unix.Mkdev(uint32(entry.Devmajor), uint32(entry.Devminor))

	entryTime := entry.ModTime
	if entryTime == nil {
		t := time.Unix(0, 0)
		entryTime = &t
	}

	mode, err := getStMode(uint32(entry.Mode), entry.Type)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, " %d %s%o %d %d %d %d %d.%d ", entry.Size,
		link, mode,
		nlinks, entry.UID, entry.GID, rdev,
		entryTime.Unix(), entryTime.Nanosecond()); err != nil {
		return err
	}

	var payload string
	if entry.Linkname != "" {
		if entry.Type == minimal.TypeSymlink {
			payload = entry.Linkname
		} else {
			payload = storagePath.CleanAbsPath(entry.Linkname)
		}
	} else if entry.Digest != "" {
		d, err := digest.Parse(entry.Digest)
		if err != nil {
			return fmt.Errorf("invalid digest %q for %q: %w", entry.Digest, entry.Name, err)
		}
		path, err := storagePath.RegularFilePathForValidatedDigest(d)
		if err != nil {
			return fmt.Errorf("determining physical file path for %q: %w", entry.Name, err)
		}
		payload = path
	}

	if _, err := fmt.Fprint(out, escapedOptional([]byte(payload), ESCAPE_LONE_DASH)); err != nil {
		return err
	}

	/* inline content.  */
	if _, err := fmt.Fprint(out, " -"); err != nil {
		return err
	}

	/* store the digest.  */
	if _, err := fmt.Fprint(out, " "); err != nil {
		return err
	}
	digest := verityDigests[payload]
	if _, err := fmt.Fprint(out, escapedOptional([]byte(digest), ESCAPE_LONE_DASH)); err != nil {
		return err
	}

	for k, vEncoded := range entry.Xattrs {
		v, err := base64.StdEncoding.DecodeString(vEncoded)
		if err != nil {
			return fmt.Errorf("decode xattr %q: %w", k, err)
		}
		name := escaped([]byte(k), ESCAPE_EQUAL)

		value := escaped(v, ESCAPE_EQUAL)
		if _, err := fmt.Fprintf(out, " %s=%s", name, value); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(out, "\n"); err != nil {
		return err
	}
	return nil
}

// GenerateDump generates a dump of the TOC in the same format as `composefs-info dump`
func GenerateDump(tocI any, verityDigests map[string]string) (io.Reader, error) {
	toc, ok := tocI.(*minimal.TOC)
	if !ok {
		return nil, fmt.Errorf("invalid TOC type")
	}
	pipeR, pipeW := io.Pipe()
	go func() {
		closed := false
		w := bufio.NewWriter(pipeW)
		defer func() {
			if !closed {
				w.Flush()
				pipeW.Close()
			}
		}()

		links := make(map[string]int)
		added := make(map[string]*minimal.FileMetadata)
		for _, e := range toc.Entries {
			if e.Linkname == "" {
				continue
			}
			if e.Type == minimal.TypeSymlink {
				continue
			}
			links[e.Linkname] = links[e.Linkname] + 1
		}

		if len(toc.Entries) == 0 {
			root := &minimal.FileMetadata{
				Name: "/",
				Type: minimal.TypeDir,
				Mode: 0o755,
			}

			if err := dumpNode(w, added, links, verityDigests, root); err != nil {
				pipeW.CloseWithError(err)
				closed = true
				return
			}
		}

		for _, e := range toc.Entries {
			if e.Type == minimal.TypeChunk {
				continue
			}
			if err := dumpNode(w, added, links, verityDigests, &e); err != nil {
				pipeW.CloseWithError(err)
				closed = true
				return
			}
		}
	}()
	return pipeR, nil
}
