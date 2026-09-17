package platformauth_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestResolverPrefersLZCAPIToken(t *testing.T) {
	values := map[string]string{
		"LZC_API_TOKEN": " pat-token ",
		"LAZYCAT_TOKEN": " legacy-token ",
		"LZC_API_HOST":  "api.example.invalid",
	}
	result, err := (platformauth.Resolver{LookupEnv: func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}}).Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Protocol != platformauth.ProtocolPAT {
		t.Fatalf("protocol=%q", result.Protocol)
	}
	if result.BaseURL != "https://api.example.invalid" {
		t.Fatalf("base URL=%q", result.BaseURL)
	}
	token, err := result.Provider.Token(context.Background())
	if err != nil || token != "pat-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestResolverFallsBackToLegacyLazyCatToken(t *testing.T) {
	values := map[string]string{
		"LAZYCAT_TOKEN": " legacy-session-token ",
		"LZC_API_HOST":  "https://ignored-for-legacy.example",
	}
	resolver := platformauth.Resolver{LookupEnv: func(name string) (string, bool) {
		value, found := values[name]
		return value, found
	}}
	result, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Protocol != platformauth.ProtocolLegacySession {
		t.Fatalf("result protocol=%q", result.Protocol)
	}
	if result.BaseURL != "" {
		t.Fatalf("legacy base URL=%q", result.BaseURL)
	}
	token, err := result.Provider.Token(context.Background())
	if err != nil || token != "legacy-session-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestResolverRejectsUnsafeAPIHostOnlyForPAT(t *testing.T) {
	_, err := (platformauth.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "LZC_API_TOKEN" {
			return "pat-token", true
		}
		if name == "LZC_API_HOST" {
			return "https://unsafe.example", true
		}
		return "", false
	}}).Resolve(context.Background())
	if err == nil || !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolvedAuthenticationIsAnImmutableSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		initial  map[string]string
		mutated  map[string]string
		protocol platformauth.Protocol
		token    string
		baseURL  string
	}{
		{
			name:     "legacy remains legacy after PAT appears",
			initial:  map[string]string{"LAZYCAT_TOKEN": "legacy-session"},
			mutated:  map[string]string{"LAZYCAT_TOKEN": "legacy-session", "LZC_API_TOKEN": "new-pat", "LZC_API_HOST": "new.example.invalid"},
			protocol: platformauth.ProtocolLegacySession, token: "legacy-session",
		},
		{
			name:     "PAT remains PAT after PAT disappears",
			initial:  map[string]string{"LZC_API_TOKEN": "original-pat", "LAZYCAT_TOKEN": "legacy-session", "LZC_API_HOST": "old.example.invalid"},
			mutated:  map[string]string{"LAZYCAT_TOKEN": "legacy-session", "LZC_API_HOST": "new.example.invalid"},
			protocol: platformauth.ProtocolPAT, token: "original-pat", baseURL: "https://old.example.invalid",
		},
		{
			name:     "PAT host remains captured",
			initial:  map[string]string{"LZC_API_TOKEN": "pat", "LZC_API_HOST": "host-a.example.invalid"},
			mutated:  map[string]string{"LZC_API_TOKEN": "pat", "LZC_API_HOST": "host-b.example.invalid"},
			protocol: platformauth.ProtocolPAT, token: "pat", baseURL: "https://host-a.example.invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := test.initial
			resolver := platformauth.Resolver{LookupEnv: func(name string) (string, bool) {
				value, found := values[name]
				return value, found
			}}
			result, err := resolver.Resolve(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			values = test.mutated
			token, err := result.Provider.Token(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Protocol != test.protocol || token != test.token || result.BaseURL != test.baseURL {
				t.Fatalf("result protocol=%q token=%q baseURL=%q", result.Protocol, token, result.BaseURL)
			}
		})
	}
}

func TestResolverRequiresSupportedToken(t *testing.T) {
	t.Setenv("LZC_API_HOST", "")
	_, err := (platformauth.Resolver{LookupEnv: func(string) (string, bool) {
		return "", false
	}}).Resolve(context.Background())
	if err == nil || !errors.Is(err, lpkgo.ErrUnauthenticated) {
		t.Fatalf("err=%v", err)
	}
}

func TestProviderCachesLZCAPIToken(t *testing.T) {
	t.Setenv("LZC_API_HOST", "")
	var lookups atomic.Int64
	provider := platformauth.NewProvider(platformauth.Resolver{
		LookupEnv: func(name string) (string, bool) {
			if name == "LZC_API_TOKEN" {
				lookups.Add(1)
				return "pat-token", true
			}
			return "", false
		},
	})

	start := make(chan struct{})
	results := make(chan string, 16)
	errors := make(chan error, 16)
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			token, err := provider.Token(context.Background())
			results <- token
			errors <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for token := range results {
		if token != "pat-token" {
			t.Fatalf("token=%q", token)
		}
	}
	if lookups.Load() != 1 {
		t.Fatalf("lookups=%d", lookups.Load())
	}
}
