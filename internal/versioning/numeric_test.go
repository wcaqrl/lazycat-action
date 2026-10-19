package versioning_test

import (
	"regexp"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/versioning"
)

func TestNumericVersionsCompareAllComponents(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"1.43.3.10896", "1.43.3.10861", 1},
		{"1.43.3.9", "1.43.3.10", -1},
		{"1.43.4.1", "1.43.3.999999", 1},
		{"1.44.0.1", "1.43.99.999999", 1},
		{"2.0.0.1", "1.99.99.999999", 1},
		{"1.043.3.0009", "1.43.3.9", 0},
		{"1.43.3.0", "1.43.3", 0},
		{"1.43.3.9999999999999999999999999", "1.43.3.999999999999999999999999", 1},
	} {
		got, err := versioning.CompareNumericVersions(test.left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("%s vs %s: %d, %v", test.left, test.right, got, err)
		}
	}
	for _, value := range []string{"", "1..3", "1.43.3-beta", "1.43.-1", "1.43.3+build"} {
		if _, err := versioning.CompareNumericVersions(value, "1.2.3"); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestNumericImageSelectionPreservesFourthComponent(t *testing.T) {
	pattern := regexp.MustCompile(`^(?P<version>(?P<major>[0-9]+)\.(?P<minor>[0-9]+)\.(?P<patch>[0-9]+)\.[0-9]+)-[a-f0-9]+$`)
	rule := versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortNumeric, TagRegex: pattern, VersionRegex: pattern, VersionTemplate: "{major}.{minor}.{patch}"}
	selected, err := versioning.Select(rule, []versioning.Candidate{
		{Tag: "1.43.3.9-ffffff"}, {Tag: "1.43.3.10-aaaaaa"},
		{Tag: "1.43.2.99999-eeeeee"}, {Tag: "latest"}, {Tag: "beta"},
		{Tag: "1.43.3.99999-cccccc-arm64"},
	})
	if err != nil || selected.SourceVersion != "1.43.3.10" || selected.Version != "1.43.3" || selected.Candidate.Tag != "1.43.3.10-aaaaaa" {
		t.Fatalf("selected=%#v err=%v", selected, err)
	}
	if _, err := versioning.RankSemVer(rule, []versioning.Candidate{{Tag: "1.43.4.10903-e5521bd8c"}}); err != nil {
		t.Fatal(err)
	}
}
