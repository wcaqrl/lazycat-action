package publishflow

import (
	"context"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestPlatformPublisherSelectsAuthenticationProtocol(t *testing.T) {
	t.Setenv("LZC_API_HOST", "")
	pat := platformPublisher(platformauth.Result{
		Protocol: platformauth.ProtocolPAT,
		Provider: auth.StaticToken("pat"),
		BaseURL:  "https://api.example.invalid",
	})
	if pat.BaseURL != "https://api.example.invalid" || pat.HTTPClient != nil || !pat.SDK {
		t.Fatalf("PAT publisher=%#v", pat)
	}

	legacy := platformPublisher(platformauth.Result{
		Protocol: platformauth.ProtocolLegacySession,
		Provider: auth.StaticToken("legacy-session"),
	})
	if legacy.BaseURL != "" || legacy.HTTPClient != nil || legacy.SDK {
		t.Fatalf("legacy publisher=%#v", legacy)
	}
}

func TestPlatformPublisherUsesResolvedSnapshotAfterEnvironmentMutation(t *testing.T) {
	values := map[string]string{
		"LZC_API_TOKEN": "pat-a",
		"LZC_API_HOST":  "host-a.example.invalid",
	}
	resolver := platformauth.Resolver{LookupEnv: func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}}
	resolved, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values = map[string]string{
		"LAZYCAT_TOKEN": "legacy-session",
		"LZC_API_HOST":  "host-b.example.invalid",
	}
	publisher := platformPublisher(resolved)
	if !publisher.SDK || publisher.BaseURL != "https://host-a.example.invalid" {
		t.Fatalf("publisher=%#v", publisher)
	}
}
