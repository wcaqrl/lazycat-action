package versioning_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/versioning"
)

var benchmarkSelection versioning.Selection

func TestSelectStableAndBeta(t *testing.T) {
	candidates := []versioning.Candidate{
		{Tag: "v1.9.0", Digest: digest("1"), Created: at(1)},
		{Tag: "v2.0.0-beta.1", Digest: digest("2"), Created: at(2)},
		{Tag: "v2.0.0-rc.1", Digest: digest("3"), Created: at(3)},
		{Tag: "v2.0.0", Digest: digest("4"), Created: at(4)},
	}
	tests := []struct {
		channel versioning.Channel
		wantTag string
		wantVer string
	}{
		{channel: versioning.ChannelStable, wantTag: "v2.0.0", wantVer: "2.0.0"},
		{channel: versioning.ChannelBeta, wantTag: "v2.0.0-rc.1", wantVer: "2.0.0-rc.1"},
	}
	for _, test := range tests {
		selection, err := versioning.Select(versioning.Rule{Channel: test.channel, Sort: versioning.SortSemVer, VersionTemplate: "{version}"}, candidates)
		if err != nil {
			t.Fatal(err)
		}
		if selection.Candidate.Tag != test.wantTag || selection.Version != test.wantVer {
			t.Fatalf("channel=%q selection=%#v", test.channel, selection)
		}
	}
}

func TestSelectDateNormalizesPaddedSemVerComponents(t *testing.T) {
	rule := versioning.Rule{
		Channel:         versioning.ChannelDate,
		Sort:            versioning.SortSemVer,
		TagRegex:        regexp.MustCompile(`^[0-9]{8}$`),
		VersionRegex:    regexp.MustCompile(`^(?P<version>[0-9]{4})(?P<month>[0-9]{2})(?P<day>[0-9]{2})$`),
		VersionTemplate: "{version}.{month}.{day}",
	}
	selection, err := versioning.Select(rule, []versioning.Candidate{
		{Tag: "20260626", Digest: digest("1"), Created: at(1)},
		{Tag: "20260101", Digest: digest("2"), Created: at(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Candidate.Tag != "20260626" || selection.Version != "2026.6.26" {
		t.Fatalf("selection=%#v", selection)
	}
}

func TestSelectNightlyUsesAMD64CreationTimeAndDigest(t *testing.T) {
	selection, err := versioning.Select(versioning.Rule{
		Channel:      versioning.ChannelNightly,
		Sort:         versioning.SortCreated,
		TagRegex:     regexp.MustCompile(`^nightly`),
		ExcludeRegex: regexp.MustCompile(`arm`),
	}, []versioning.Candidate{
		{Tag: "nightly-arm", Digest: digest("a"), Created: time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)},
		{Tag: "nightly", Digest: "sha256:a1b2c3d4e5f6" + strings.Repeat("0", 52), Created: time.Date(2026, 7, 10, 15, 30, 20, 0, time.UTC)},
		{Tag: "nightly-old", Digest: digest("b"), Created: time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Candidate.Tag != "nightly" || selection.Version != "0.0.0-nightly.20260710153020.a1b2c3d4e5f6" {
		t.Fatalf("selection=%#v", selection)
	}
}

func TestSelectMutableUsesNewestCreatedCandidate(t *testing.T) {
	selection, err := versioning.SelectMutable(versioning.Rule{
		Channel:  versioning.ChannelCustom,
		Sort:     versioning.SortCreated,
		TagRegex: regexp.MustCompile(`^latest$`),
	}, []versioning.Candidate{
		{Tag: "old", Digest: digest("1"), Created: at(3)},
		{Tag: "latest", Digest: digest("2"), Created: at(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Candidate.Tag != "latest" || selection.Version != "" {
		t.Fatalf("selection=%#v", selection)
	}
}

func TestBumpPatch(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{name: "increments patch", value: "1.4.6", want: "1.4.7"},
		{name: "rejects prerelease", value: "1.4.6-rc.1", wantErr: "must be stable"},
		{name: "rejects metadata", value: "1.4.6+build", wantErr: "must be stable"},
		{name: "rejects invalid", value: "latest", wantErr: "parse current version"},
		{name: "rejects overflow", value: "1.2.18446744073709551615", wantErr: "overflows"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := versioning.BumpPatch(test.value)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
}

func TestSelectCustomByMappedSemVerAndCreated(t *testing.T) {
	rule := versioning.Rule{
		Channel:         versioning.ChannelCustom,
		Sort:            versioning.SortSemVer,
		TagRegex:        regexp.MustCompile(`^release-`),
		VersionRegex:    regexp.MustCompile(`^release-(?P<version>\d+\.\d+\.\d+)$`),
		VersionTemplate: "{version}",
	}
	selection, err := versioning.Select(rule, []versioning.Candidate{
		{Tag: "release-1.9.0", Digest: digest("1"), Created: at(4)},
		{Tag: "release-2.0.0", Digest: digest("2"), Created: at(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "2.0.0" {
		t.Fatalf("selection=%#v", selection)
	}

	rule.Sort = versioning.SortCreated
	selection, err = versioning.Select(rule, []versioning.Candidate{
		{Tag: "release-1.9.0", Digest: digest("1"), Created: at(4)},
		{Tag: "release-2.0.0", Digest: digest("2"), Created: at(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "1.9.0" {
		t.Fatalf("selection=%#v", selection)
	}
}

func TestSelectUpdatedPrefersTagUpdateTimeThenSemVerAndTag(t *testing.T) {
	rule := versioning.Rule{
		Channel:         versioning.ChannelStable,
		Sort:            versioning.SortUpdated,
		TagRegex:        regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-copy)?$`),
		VersionRegex:    regexp.MustCompile(`^v(?P<version>\d+\.\d+\.\d+)(?:-copy)?$`),
		VersionTemplate: "{version}",
	}
	updated := time.Date(2026, 7, 12, 8, 30, 0, 0, time.UTC)
	selection, err := versioning.Select(rule, []versioning.Candidate{
		{Tag: "v1.2.26", Digest: digest("2"), Updated: updated.Add(-28 * 24 * time.Hour)},
		{Tag: "v1.2.15", Digest: digest("1"), Updated: updated},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Candidate.Tag != "v1.2.15" || selection.Version != "1.2.15" {
		t.Fatalf("selection=%#v", selection)
	}

	ranked, err := versioning.RankUpdated(rule, []versioning.Candidate{
		{Tag: "v1.2.15", Updated: updated},
		{Tag: "v1.2.26", Updated: updated},
		{Tag: "v1.2.26-copy", Updated: updated},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v1.2.26-copy", "v1.2.26", "v1.2.15"}
	for index, expected := range want {
		if ranked[index].Candidate.Tag != expected {
			t.Fatalf("ranked[%d]=%q want %q", index, ranked[index].Candidate.Tag, expected)
		}
	}
}

func TestSelectUpdatedRequiresTagUpdateTime(t *testing.T) {
	_, err := versioning.Select(versioning.Rule{
		Channel: versioning.ChannelStable,
		Sort:    versioning.SortUpdated,
	}, []versioning.Candidate{{Tag: "v1.2.3", Digest: digest("1")}})
	if err == nil || !strings.Contains(err.Error(), "update time is required") {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectExpandsNamedVersionTemplateGroups(t *testing.T) {
	rule := versioning.Rule{
		Channel:         versioning.ChannelCustom,
		Sort:            versioning.SortCreated,
		TagRegex:        regexp.MustCompile(`^\d{8}\.\d+$`),
		VersionRegex:    regexp.MustCompile(`^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$`),
		VersionTemplate: "{version}.{build}.0",
	}
	selection, err := versioning.Select(rule, []versioning.Candidate{{Tag: "20260603.01", Digest: digest("1"), Created: at(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Version != "20260603.1.0" {
		t.Fatalf("selection=%#v", selection)
	}

	rule.VersionTemplate = "{version}.{missing}.0"
	_, err = versioning.Select(rule, []versioning.Candidate{{Tag: "20260603.01", Digest: digest("1"), Created: at(1)}})
	if err == nil || !strings.Contains(err.Error(), "unresolved version template placeholder") {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectRejectsInvalidRulesAndCandidates(t *testing.T) {
	tests := []struct {
		name       string
		rule       versioning.Rule
		candidates []versioning.Candidate
	}{
		{name: "empty", rule: versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer}},
		{name: "missing named group", rule: versioning.Rule{Channel: versioning.ChannelCustom, Sort: versioning.SortSemVer, TagRegex: regexp.MustCompile(`.`), VersionRegex: regexp.MustCompile(`(.*)`)}, candidates: []versioning.Candidate{{Tag: "1.2.3", Digest: digest("1")}}},
		{name: "invalid mapped semver", rule: versioning.Rule{Channel: versioning.ChannelCustom, Sort: versioning.SortSemVer, TagRegex: regexp.MustCompile(`.`), VersionRegex: regexp.MustCompile(`(?P<version>.*)`), VersionTemplate: "v{version}"}, candidates: []versioning.Candidate{{Tag: "1.2.3", Digest: digest("1")}}},
		{name: "short nightly digest", rule: versioning.Rule{Channel: versioning.ChannelNightly, Sort: versioning.SortCreated, TagRegex: regexp.MustCompile(`.`)}, candidates: []versioning.Candidate{{Tag: "nightly", Digest: "sha256:abc", Created: at(1)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := versioning.Select(test.rule, test.candidates); err == nil {
				t.Fatal("expected selection to fail")
			}
		})
	}
}

func BenchmarkSelectStable1000(b *testing.B) {
	candidates := benchmarkCandidates(1000)
	rule := versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer}
	b.ReportAllocs()
	for b.Loop() {
		selection, err := versioning.Select(rule, candidates)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSelection = selection
	}
}

func BenchmarkSelectCustomCreated1000(b *testing.B) {
	candidates := benchmarkCandidates(1000)
	rule := versioning.Rule{
		Channel:  versioning.ChannelCustom,
		Sort:     versioning.SortCreated,
		TagRegex: regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`),
	}
	b.ReportAllocs()
	for b.Loop() {
		selection, err := versioning.Select(rule, candidates)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSelection = selection
	}
}

func benchmarkCandidates(count int) []versioning.Candidate {
	candidates := make([]versioning.Candidate, 0, count)
	for index := range count {
		candidates = append(candidates, versioning.Candidate{
			Tag:     fmt.Sprintf("v1.%d.%d", index/100, index%100),
			Digest:  digest(fmt.Sprintf("%x", index%16)),
			Created: time.Date(2026, 1, 1+index%28, 0, 0, index, 0, time.UTC),
		})
	}
	return candidates
}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }
func at(day int) time.Time           { return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC) }
