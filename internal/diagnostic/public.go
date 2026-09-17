package diagnostic

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxDetailBytes = 512
	maxPathBytes   = 256
)

var safeYAMLParserProblems = map[string]struct{}{
	"block mapping entries are not allowed in this context":  {},
	"block sequence entries are not allowed in this context": {},
	"could not find expected ':'":                            {},
	"did not find expected ',' or ']'":                       {},
	"did not find expected ',' or '}'":                       {},
	"did not find expected key":                              {},
	"did not find expected node content":                     {},
	"found character that cannot start any token":            {},
	"found unexpected document indicator":                    {},
	"found unexpected end of stream":                         {},
	"mapping keys are not allowed in this context":           {},
	"mapping values are not allowed in this context":         {},
}

// KnownProjectPath exposes a path only when it exactly matches one of the
// project files already established by inspection.
func KnownProjectPath(root, filename string, knownFiles ...string) string {
	filename = strings.TrimSpace(strings.ToValidUTF8(filename, ""))
	if filename == "" || hasControl(filename) {
		return ""
	}
	candidate, ok := absoluteLocalPath(root, filename)
	if !ok {
		return ""
	}
	for _, known := range knownFiles {
		allowed, allowedOK := absoluteLocalPath(root, known)
		if !allowedOK || candidate != allowed {
			continue
		}
		return relativeProjectPath(root, allowed)
	}
	return ""
}

func absoluteLocalPath(root, filename string) (string, bool) {
	if crossPlatformAbsolute(filename) && !filepath.IsAbs(filename) {
		return "", false
	}
	cleaned := filepath.Clean(filename)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(root, cleaned)
	}
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return "", false
	}
	return filepath.Clean(absolute), true
}

func relativeProjectPath(root, filename string) string {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(filepath.Clean(absoluteRoot), filename)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return SafePath(relative)
}

// SafePath returns a single bounded path token without exposing absolute
// runner locations, traversal, control characters, or credential markers.
func SafePath(value string) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if value == "" || hasControl(value) || containsCredentialMarker(value) {
		return ""
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	cleaned := path.Clean(normalized)
	if crossPlatformAbsolute(normalized) {
		cleaned = path.Base(cleaned)
	}
	if cleaned == "" || cleaned == "." || cleaned == ".." || cleaned == "/" || strings.HasPrefix(cleaned, "../") || containsCredentialMarker(cleaned) {
		return ""
	}
	return truncate(cleaned, maxPathBytes)
}

// SafeDetail performs the final defense-in-depth normalization for a detail
// already selected by a trusted producer.
func SafeDetail(value string) string {
	value = normalize(value)
	if value == "" || hasControl(value) || containsCredentialMarker(value) {
		return ""
	}
	return truncate(value, maxDetailBytes)
}

// SafeYAMLSyntaxDetail exposes only stable parser problem strings that do not
// echo YAML values. Type errors, path errors, and arbitrary validation causes
// remain private.
func SafeYAMLSyntaxDetail(err error) string {
	if err == nil {
		return ""
	}
	var pathError *fs.PathError
	if errors.As(err, &pathError) {
		return ""
	}
	detail := normalize(err.Error())
	const prefix = "yaml: line "
	if !strings.HasPrefix(detail, prefix) || containsCredentialMarker(detail) {
		return ""
	}
	rest := strings.TrimPrefix(detail, prefix)
	line, problem, found := strings.Cut(rest, ": ")
	if !found || !positiveDecimal(line) {
		return ""
	}
	if _, allowed := safeYAMLParserProblems[problem]; !allowed {
		return ""
	}
	return truncate(detail, maxDetailBytes)
}

func normalize(value string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " ")
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return strings.TrimSpace(strings.ToValidUTF8(value[:maximum], ""))
}

func positiveDecimal(value string) bool {
	if value == "" || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func crossPlatformAbsolute(value string) bool {
	value = strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(value, "/") {
		return true
	}
	return len(value) >= 2 && value[1] == ':' && (value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z')
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.In(character, unicode.Zl, unicode.Zp) || character != ' ' && unicode.Is(unicode.Zs, character) {
			return true
		}
	}
	return false
}

func containsCredentialMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"lcst_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
		"bearer ", "authorization", "x-user-token", "cookie", "password", "passwd", "secret",
		"credential", "api key", "api-key", "api_key", "apikey", "private key", "private_key",
		"token=", "token:", "token ", `"token"`, "'token'", "access_token", "refresh_token", "id_token", "client_token",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
