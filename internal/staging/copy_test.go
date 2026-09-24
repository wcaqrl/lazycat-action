package staging_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/staging"
)

func TestCopyPinsPlatformAndSerializesRegistryTransfer(t *testing.T) {
	directory := t.TempDir()
	logfile := filepath.Join(directory, "args")
	crane := filepath.Join(directory, "crane")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$CRANE_ARGS\"\n"
	if err := os.WriteFile(crane, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("CRANE_ARGS", logfile)
	if err := staging.Copy(context.Background(), "ghcr.io/acme/app:v1@sha256:abc", "ttl.sh/acme-app:24h", platform.Target{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(data))
	want := []string{"copy", "--platform", "linux/amd64", "--jobs", "1", "ghcr.io/acme/app:v1@sha256:abc", "ttl.sh/acme-app:24h"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("args=%q want=%q", got, want)
	}
}
