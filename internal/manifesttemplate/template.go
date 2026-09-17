package manifesttemplate

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

const markerPrefix = "lazycat-action-template-control-"

type control struct {
	marker   string
	original string
}

type Protected struct {
	data     []byte
	controls []control
}

func Protect(input []byte) (Protected, error) {
	if bytes.Contains(input, []byte(markerPrefix)) {
		return Protected{}, fmt.Errorf("manifest contains reserved marker prefix %q", markerPrefix)
	}

	lines := bytes.Split(input, []byte("\n"))
	controls := make([]control, 0)
	for index, line := range lines {
		if !isStandaloneControl(line) {
			continue
		}

		marker := fmt.Sprintf("%s%d", markerPrefix, len(controls))
		original := string(line)
		indentation := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
		lines[index] = []byte(string(indentation) + "# " + marker)
		controls = append(controls, control{marker: marker, original: original})
	}

	data := bytes.Join(lines, []byte("\n"))
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Protected{}, fmt.Errorf("validating protected manifest YAML: %w", err)
	}

	return Protected{data: data, controls: controls}, nil
}

func (protected Protected) Bytes() []byte {
	return bytes.Clone(protected.data)
}

func (protected Protected) Restore(encoded []byte) ([]byte, error) {
	lines := bytes.Split(bytes.Clone(encoded), []byte("\n"))
	for _, control := range protected.controls {
		markerComment := "# " + control.marker
		occurrences := 0
		lineIndex := -1
		for index, line := range lines {
			if strings.TrimSpace(string(line)) == markerComment {
				occurrences++
				lineIndex = index
			}
		}
		if occurrences != 1 {
			return nil, fmt.Errorf("template control marker %q occurs %d times, want exactly once", control.marker, occurrences)
		}
		lines[lineIndex] = []byte(control.original)
	}

	return bytes.Join(lines, []byte("\n")), nil
}

func isStandaloneControl(line []byte) bool {
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return false
	}

	action := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	action = strings.TrimSpace(strings.TrimPrefix(action, "-"))
	words := strings.Fields(action)
	if len(words) == 0 {
		return false
	}

	switch strings.Trim(words[0], "-") {
	case "if", "else", "end", "with", "range":
		return true
	default:
		return false
	}
}
