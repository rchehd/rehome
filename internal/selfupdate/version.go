package selfupdate

import (
	"fmt"
	"regexp"
	"strconv"
)

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type version struct {
	numbers    [3]uint64
	prerelease string
}

func parseVersion(text string) (version, error) {
	parts := versionPattern.FindStringSubmatch(text)
	if parts == nil {
		return version{}, fmt.Errorf("expected a release version, got %q", text)
	}
	v := version{prerelease: parts[4]}
	for i := range v.numbers {
		n, err := strconv.ParseUint(parts[i+1], 10, 64)
		if err != nil {
			return version{}, err
		}
		v.numbers[i] = n
	}
	return v, nil
}

func (v version) olderThan(stable version) bool {
	for i, n := range v.numbers {
		if n != stable.numbers[i] {
			return n < stable.numbers[i]
		}
	}
	return v.prerelease != ""
}
