package registry

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestFilterTagsAllowsLargeRepositoryWithNarrowFilter(t *testing.T) {
	tags := make([]string, defaultMaxTags+1)
	for index := range tags {
		tags[index] = fmt.Sprintf("unrelated-%05d", index)
	}
	tags[0] = "server-vulkan-b10290"

	eligible, err := filterTags("ghcr.io/ggml-org/llama.cpp", tags, TagFilter{Include: regexp.MustCompile(`^server-vulkan-b\d+$`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 1 || eligible[0] != "server-vulkan-b10290" {
		t.Fatalf("eligible=%#v", eligible)
	}
}

func TestFilterTagsRejectsTooManyMatchingTags(t *testing.T) {
	tags := make([]string, defaultMaxMatchingTags+1)
	for index := range tags {
		tags[index] = fmt.Sprintf("server-vulkan-b%05d", index)
	}

	_, err := filterTags("ghcr.io/ggml-org/llama.cpp", tags, TagFilter{Include: regexp.MustCompile(`^server-vulkan-b\d+$`)})
	if err == nil || !strings.Contains(err.Error(), "matching the configured filter") {
		t.Fatalf("err=%v", err)
	}
}
