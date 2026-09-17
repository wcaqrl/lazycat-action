package appversion

import (
	"fmt"
	"regexp"
	"strings"
)

var pattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func IsValid(value string) bool {
	return pattern.MatchString(value)
}

func Normalize(raw string) (string, string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if !IsValid(value) {
		return "", "", fmt.Errorf("invalid version %q: expected SemVer with an optional leading v", raw)
	}
	return value, "v" + value, nil
}
