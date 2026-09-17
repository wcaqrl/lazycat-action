package yamledit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/yamledit"
)

func TestSetPackageVersionPreservesCommentsAndIsIdempotent(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "package.yml")
	original := `# package comment
package: cloud.lazycat.example
version: 1.0.0 # keep inline comment
name: Example
`
	if err := os.WriteFile(filename, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}

	change, err := yamledit.SetPackageVersion(filename, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if change != (yamledit.Change{Changed: true, Old: "1.0.0", New: "1.2.3"}) {
		t.Fatalf("change=%#v", change)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{"# package comment", "version: 1.2.3 # keep inline comment", "name: Example"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}

	change, err = yamledit.SetPackageVersion(filename, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if change != (yamledit.Change{Changed: false, Old: "1.2.3", New: "1.2.3"}) {
		t.Fatalf("idempotent change=%#v", change)
	}
}

func TestSetPackageVersionInsertsAfterPackage(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "package.yml")
	if err := os.WriteFile(filename, []byte("package: cloud.lazycat.example\nname: Example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := yamledit.SetPackageVersion(filename, "1.2.3-beta.1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(data), "version:") < strings.Index(string(data), "package:") || strings.Index(string(data), "version:") > strings.Index(string(data), "name:") {
		t.Fatalf("unexpected key order:\n%s", data)
	}
}

func TestSetPackageVersionRejectsInvalidVersionWithoutChangingFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "package.yml")
	original := []byte("package: cloud.lazycat.example\nversion: 1.0.0\n")
	if err := os.WriteFile(filename, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := yamledit.SetPackageVersion(filename, "v1.2.3"); err == nil {
		t.Fatal("expected invalid version to fail")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("file changed:\n%s", data)
	}
}

func TestPackageVersionValidationRejectsLeadingZeroPrereleaseNumbers(t *testing.T) {
	for _, value := range []string{"1.2.3-01", "1.2.3-alpha.01"} {
		if yamledit.IsValidPackageVersion(value) {
			t.Fatalf("version %q unexpectedly passed", value)
		}
	}
	for _, value := range []string{"1.2.3-0", "1.2.3-alpha.1", "1.2.3+build.01"} {
		if !yamledit.IsValidPackageVersion(value) {
			t.Fatalf("version %q unexpectedly failed", value)
		}
	}
}
