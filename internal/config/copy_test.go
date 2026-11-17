package config_test

import (
	"github.com/wcaqrl/lazycat-action/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyLimitsAcceptDurationsAndRejectInvalidModesOrBounds(t *testing.T) {
	for _, tc := range []struct {
		name, mode, limits string
		valid              bool
	}{
		{"configured", "lazycat", "copy_timeout: 30m\n      copy_idle_timeout: 10m", true},
		{"only_total", "lazycat", "copy_timeout: 5m", true},
		{"negative", "lazycat", "copy_timeout: -1s", false},
		{"negative_idle", "lazycat", "copy_idle_timeout: -1s", false},
		{"idle_exceeds_total", "lazycat", "copy_timeout: 1m\n      copy_idle_timeout: 2m", false},
		{"direct_cannot_ignore_limits", "direct", "copy_timeout: 1m", false},
		{"invalid_duration", "lazycat", "copy_timeout: later", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := "version: 2\nsource:\n  kind: git\n  url: https://github.com/acme/app.git\nbuild:\n  prepare:\n    mode: images\nimages:\n  - id: web\n    target: service\n    service: web\n    source: ghcr.io/acme/app:1\n    delivery:\n      mode: " + tc.mode + "\n      " + tc.limits + "\n"
			path := filepath.Join(t.TempDir(), "lazycat.yml")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := config.Load(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
