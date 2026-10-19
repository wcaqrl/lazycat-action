package versioning

import (
	"fmt"
	"strings"
)

// CompareNumericVersions compares every dot-separated decimal component rather
// than truncating to SemVer's three core components. Plex 1.43.3.10896 must rank
// above 1.43.3.10861, even though both map to the same application base 1.43.3.
// Comparing normalized decimal strings avoids both lexical 9/10 ordering bugs
// and integer overflow. Vendor hash suffixes are excluded by version_regex.
func CompareNumericVersions(left, right string) (int, error) {
	a, b := strings.Split(left, "."), strings.Split(right, ".")
	for _, parts := range [][]string{a, b} {
		for _, part := range parts {
			if part == "" {
				return 0, fmt.Errorf("invalid dotted numeric version")
			}
			for _, digit := range part {
				if digit < '0' || digit > '9' {
					return 0, fmt.Errorf("invalid dotted numeric version")
				}
			}
		}
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := "", ""
		if i < len(a) {
			x = strings.TrimLeft(a[i], "0")
		}
		if i < len(b) {
			y = strings.TrimLeft(b[i], "0")
		}
		if len(x) < len(y) {
			return -1, nil
		}
		if len(x) > len(y) {
			return 1, nil
		}
		if comparison := strings.Compare(x, y); comparison != 0 {
			return comparison, nil
		}
	}
	return 0, nil
}
