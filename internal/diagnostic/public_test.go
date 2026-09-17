package diagnostic_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/diagnostic"
)

func TestSafePathNormalizesAbsoluteAndCrossPlatformPaths(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "linux absolute", value: "/home/runner/work/repo/lzc-manifest.yml", want: "lzc-manifest.yml"},
		{name: "windows drive", value: `C:\actions-runner\_work\repo\lzc-manifest.yml`, want: "lzc-manifest.yml"},
		{name: "windows extended", value: `\\?\C:\actions-runner\_work\repo\lzc-manifest.yml`, want: "lzc-manifest.yml"},
		{name: "UNC", value: `\\server\share\repo\lzc-manifest.yml`, want: "lzc-manifest.yml"},
		{name: "relative", value: "config/lzc-manifest.yml", want: "config/lzc-manifest.yml"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := diagnostic.SafePath(test.value); got != test.want {
				t.Fatalf("SafePath(%q)=%q want=%q", test.value, got, test.want)
			}
		})
	}
}

func TestSafePathSuppressesTraversalControlCharactersAndSecrets(t *testing.T) {
	for _, value := range []string{
		"../../runner/lzc-manifest.yml",
		`..\..\runner\lzc-manifest.yml`,
		"config/\x1b[2Jlzc-manifest.yml",
		"config/lzc-\u202Emanifest.yml",
		"config/lzc-\u2066manifest.yml",
		"config/lzc-\u2028manifest.yml",
		"config/lzc-\u2029manifest.yml",
		"config/lzc-\u00a0manifest.yml",
		"config/client_secret.yml",
	} {
		if got := diagnostic.SafePath(value); got != "" {
			t.Fatalf("SafePath(%q)=%q want empty", value, got)
		}
	}
}

func TestKnownProjectPathKeepsOnlyAllowlistedProjectFiles(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "config", "lzc-manifest.yml")
	if got := diagnostic.KnownProjectPath(root, inside, inside); got != "config/lzc-manifest.yml" {
		t.Fatalf("inside path=%q", got)
	}
	outside := filepath.Join(filepath.Dir(root), "other", "lzc-manifest.yml")
	if got := diagnostic.KnownProjectPath(root, outside, inside); got != "" {
		t.Fatalf("outside path=%q want empty", got)
	}
	if got := diagnostic.KnownProjectPath(root, root, root); got != "" {
		t.Fatalf("root path=%q want empty", got)
	}
	if got := diagnostic.KnownProjectPath(root, "opaque-credential-value", inside); got != "" {
		t.Fatalf("unknown path=%q want empty", got)
	}
}

func TestSafeDetailBoundsTextWithoutSuppressingYAMLSyntaxWordToken(t *testing.T) {
	value := "yaml:\n line 4: found character that cannot start any token"
	if got := diagnostic.SafeDetail(value); got != "yaml: line 4: found character that cannot start any token" {
		t.Fatalf("detail=%q", got)
	}
	long := strings.Repeat("x", 600)
	if got := diagnostic.SafeDetail(long); len(got) != 512 {
		t.Fatalf("bounded length=%d", len(got))
	}
}

func TestSafeDetailSuppressesCredentialShapes(t *testing.T) {
	for _, value := range []string{
		"authorization bearer abc",
		"token=must-not-leak",
		"access_token: must-not-leak",
		"ghp_must_not_leak",
		"github_pat_must_not_leak",
		"password must-not-leak",
	} {
		if got := diagnostic.SafeDetail(value); got != "" {
			t.Fatalf("SafeDetail(%q)=%q want empty", value, got)
		}
	}
}

func TestSafeYAMLSyntaxDetailAllowsOnlyKnownParserProblems(t *testing.T) {
	for _, value := range []string{
		"yaml: line 90: block sequence entries are not allowed in this context",
		"yaml: line 4: found character that cannot start any token",
		"yaml: line 7: did not find expected key",
	} {
		if got := diagnostic.SafeYAMLSyntaxDetail(errors.New(value)); got != value {
			t.Fatalf("SafeYAMLSyntaxDetail(%q)=%q", value, got)
		}
	}
	for _, err := range []error{
		errors.New("yaml: unmarshal errors: line 2: cannot unmarshal !!str `ghs_sec` into []string"),
		errors.New("yaml: line 2: ghs_secret_value"),
		&os.PathError{Op: "open", Path: "/home/runner/work/private/lzc-build.yml", Err: os.ErrNotExist},
	} {
		if got := diagnostic.SafeYAMLSyntaxDetail(err); got != "" {
			t.Fatalf("SafeYAMLSyntaxDetail(%v)=%q want empty", err, got)
		}
	}
}
