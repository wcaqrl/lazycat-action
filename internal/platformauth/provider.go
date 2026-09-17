package platformauth

import (
	"context"
	"sync"
)

type Provider struct {
	resolver Resolver
	mu       sync.Mutex
	token    string
}

func NewProvider(resolver Resolver) *Provider {
	return &Provider{resolver: resolver}
}

func (provider *Provider) Token(ctx context.Context) (string, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.token != "" {
		return provider.token, nil
	}
	resolved, err := provider.resolver.Resolve(ctx)
	if err != nil {
		return "", err
	}
	token, err := resolved.Provider.Token(ctx)
	if err != nil {
		return "", err
	}
	provider.token = token
	return token, nil
}
