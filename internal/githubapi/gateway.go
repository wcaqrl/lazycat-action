package githubapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const EnvURL = "LAZYCAT_GITHUB_API_URL"
const EnvSession = "LAZYCAT_GITHUB_API_SESSION"
const sessionHeader = "X-Lazycat-GitHub-Session"

var repoPartPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func repositoryPart(value string) bool {
	return value != "." && value != ".." && repoPartPattern.MatchString(value)
}

type Endpoint struct {
	URL     string
	Session string
}

type endpointKey struct{}

// WithEndpoint carries only a short-lived local capability, never GITHUB_TOKEN.
func WithEndpoint(ctx context.Context, endpoint Endpoint) context.Context {
	return context.WithValue(ctx, endpointKey{}, endpoint)
}

func FromContext(ctx context.Context) Endpoint {
	value, _ := ctx.Value(endpointKey{}).(Endpoint)
	return value
}

func (e Endpoint) Environment() map[string]string {
	if e.URL == "" || e.Session == "" {
		return nil
	}
	return map[string]string{EnvURL: e.URL, EnvSession: e.Session}
}

type Gateway struct {
	server   *http.Server
	endpoint Endpoint
	once     sync.Once
	done     chan struct{}
	closeErr error
}

func (g *Gateway) Endpoint() Endpoint { return g.endpoint }
func (g *Gateway) Close() error {
	if g == nil {
		return nil
	}
	g.once.Do(func() { g.closeErr = g.server.Close(); close(g.done) })
	return g.closeErr
}

func Start(ctx context.Context, client Client, repositories []string) (*Gateway, error) {
	allowed := map[string]bool{}
	for _, repo := range repositories {
		parts := strings.Split(repo, "/")
		if len(parts) != 2 || !repositoryPart(parts[0]) || !repositoryPart(parts[1]) {
			return nil, fmt.Errorf("invalid GitHub metadata scope")
		}
		allowed[strings.ToLower(repo)] = true
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("GitHub metadata scope is empty")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("unable to start local GitHub reader")
	}
	gateway := &Gateway{endpoint: Endpoint{URL: "http://" + listener.Addr().String(), Session: hex.EncodeToString(secret)}, done: make(chan struct{})}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, message string) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"message": message})
		}
		if r.Method != http.MethodGet {
			fail(405, "GitHub reader supports GET only")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(sessionHeader)), []byte(gateway.endpoint.Session)) != 1 {
			fail(403, "GitHub reader session required")
			return
		}
		repo, err := metadataPath(r.URL.RequestURI())
		if err != nil || !allowed[repo] {
			fail(403, "GitHub metadata path is outside configured scope")
			return
		}
		response, err := client.Get(r.Context(), r.URL.RequestURI())
		if err != nil {
			var apiError *Error
			if errors.As(err, &apiError) {
				fail(apiError.Status, apiError.Error())
			} else {
				fail(502, "GitHub metadata read failed")
			}
			return
		}
		// Do not forward any upstream authentication/cookie/redirect headers.
		w.WriteHeader(200)
		w.Write(response.Body)
	})
	gateway.server = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 10 * time.Second}
	go func() { _ = gateway.server.Serve(listener) }()
	go func() {
		select {
		case <-ctx.Done():
			gateway.Close()
		case <-gateway.done:
		}
	}()
	return gateway, nil
}

// Repository accepts only actual GitHub repository URLs, not lookalike hosts.
func Repository(value string) string {
	if strings.HasPrefix(value, "git@github.com:") {
		value = "https://github.com/" + strings.TrimPrefix(value, "git@github.com:")
	}
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Host, "github.com") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return ""
	}
	if u.Scheme == "https" && u.User != nil {
		return ""
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || !repositoryPart(parts[0]) || !repositoryPart(parts[1]) {
		return ""
	}
	return parts[0] + "/" + parts[1]
}
