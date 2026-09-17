package appversion_test

import (
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/appversion"
)

func TestNormalize(t *testing.T) {
	for _, raw := range []string{"1.2.3", "v1.2.3"} {
		version, tag, err := appversion.Normalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		if version != "1.2.3" || tag != "v1.2.3" {
			t.Fatalf("raw=%q version=%q tag=%q", raw, version, tag)
		}
	}
	for _, raw := range []string{"V1.2.3", "vv1.2.3", "1.2", "1.2.3-01"} {
		if _, _, err := appversion.Normalize(raw); err == nil {
			t.Fatalf("raw=%q unexpectedly passed", raw)
		}
	}
}
