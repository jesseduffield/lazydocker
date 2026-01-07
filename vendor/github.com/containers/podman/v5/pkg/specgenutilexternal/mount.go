package specgenutilexternal

import (
	"encoding/csv"
	"errors"
	"strings"
)

// FindMountType parses the input and extracts the type of the mount type and
// the remaining non-type tokens.
func FindMountType(input string) (mountType string, tokens []string, err error) {
	// Split by comma, iterate over the slice and look for
	// "type=$mountType". Everything else is appended to tokens.
	found := false
	csvReader := csv.NewReader(strings.NewReader(input))
	records, err := csvReader.ReadAll()
	if err != nil {
		return "", nil, err
	}
	if len(records) != 1 {
		return "", nil, errors.New("incorrect mount format: should be --mount type=<bind|glob|tmpfs|volume>,[src=<host-dir|volume-name>,]target=<ctr-dir>[,options]")
	}
	for _, s := range records[0] {
		kv := strings.Split(s, "=")
		if found || (len(kv) != 2 || kv[0] != "type") {
			tokens = append(tokens, s)
			continue
		}
		mountType = kv[1]
		found = true
	}
	if !found {
		mountType = "volume"
	}
	return
}
