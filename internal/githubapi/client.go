// Package githubapi reads GitHub release/tag metadata without exporting tokens.
package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxBody = 4 << 20

type Response struct {
	Body   []byte
	Header http.Header
}
type Error struct {
	Status     int
	Kind       string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.Kind == "rate-limit" {
		return fmt.Sprintf("GitHub API rate limit reached (HTTP %d; retry after %s)", e.Status, e.RetryAfter.Round(time.Second))
	}
	return fmt.Sprintf("GitHub metadata request failed (HTTP %d; %s)", e.Status, e.Kind)
}
func (e *Error) PublicErrorDetail() string { return e.Error() }

type Client struct {
	BaseURL     string
	Token       string
	HTTPClient  *http.Client
	MaxAttempts int
	MaxWait     time.Duration
	Sleep       func(context.Context, time.Duration) error
}

func (c Client) Get(ctx context.Context, path string) (*Response, error) {
	if _, err := metadataPath(path); err != nil {
		return nil, err
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	client := http.Client{Timeout: 20 * time.Second}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	// Never forward Authorization across redirects, including repository moves.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	attempts := c.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	budget := c.MaxWait
	if budget <= 0 {
		budget = 30 * time.Second
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = wait
	}
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub metadata URL")
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "lazycat-action")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		response, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt+1 == attempts {
				return nil, fmt.Errorf("GitHub metadata connection failed")
			}
			delay := time.Second << attempt
			if delay > budget {
				return nil, fmt.Errorf("GitHub metadata retry budget exhausted")
			}
			if err = sleep(ctx, delay); err != nil {
				return nil, err
			}
			budget -= delay
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
		response.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("unable to read GitHub metadata response")
		}
		if len(raw) > maxBody {
			return nil, fmt.Errorf("GitHub metadata response exceeds size limit")
		}
		if response.StatusCode == http.StatusOK {
			if !json.Valid(raw) {
				return nil, fmt.Errorf("GitHub metadata is not valid JSON")
			}
			return &Response{Body: raw, Header: response.Header.Clone()}, nil
		}
		limited := response.StatusCode == 429 || (response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "" || strings.Contains(strings.ToLower(string(raw)), "rate limit")))
		kind := "upstream-error"
		if limited {
			kind = "rate-limit"
		}
		failure := &Error{Status: response.StatusCode, Kind: kind}
		retry := limited || response.StatusCode == 500 || response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504
		if !retry {
			return nil, failure
		}
		delay, explicit := retryDelay(response.Header, time.Now())
		if !explicit {
			delay = time.Second << attempt
			if limited {
				delay = time.Minute
			}
		}
		failure.RetryAfter = delay
		if attempt+1 == attempts || delay > budget {
			return nil, failure
		}
		if err := sleep(ctx, delay); err != nil {
			return nil, err
		}
		budget -= delay
	}
	return nil, fmt.Errorf("GitHub metadata attempts exhausted")
}

func retryDelay(header http.Header, now time.Time) (time.Duration, bool) {
	if raw := header.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 && seconds <= 86400*365 {
			return time.Duration(seconds) * time.Second, true
		}
		if date, err := http.ParseTime(raw); err == nil {
			return max(time.Duration(0), date.Sub(now)), true
		}
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return max(time.Duration(0), time.Unix(reset, 0).Sub(now)), true
		}
	}
	return 0, false
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func metadataPath(path string) (string, error) {
	u, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || u.IsAbs() || u.Host != "" || strings.Contains(u.EscapedPath(), "//") {
		return "", fmt.Errorf("unsupported GitHub metadata path")
	}
	// Split before decoding so an encoded slash stays inside the final tag
	// parameter. Repository and route components are still validated separately.
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for index, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub metadata encoding")
		}
		parts[index] = decoded
	}
	if len(parts) < 4 || parts[0] != "repos" || !repositoryPart(parts[1]) || !repositoryPart(parts[2]) {
		return "", fmt.Errorf("unsupported GitHub metadata repository")
	}
	for _, part := range parts {
		for _, component := range strings.Split(part, "/") {
			if component == "." || component == ".." || component == "" || strings.ContainsAny(component, "\\\r\n\x00") {
				return "", fmt.Errorf("unsupported GitHub metadata path")
			}
		}
	}
	allowed := false
	switch {
	case len(parts) == 4 && parts[3] == "releases":
		allowed = true
		for key, values := range u.Query() {
			if (key != "page" && key != "per_page") || len(values) != 1 {
				return "", fmt.Errorf("unsupported GitHub metadata query")
			}
			number, e := strconv.Atoi(values[0])
			if e != nil || number < 1 || number > 100 {
				return "", fmt.Errorf("invalid GitHub metadata pagination")
			}
		}
	case len(parts) == 5 && parts[3] == "releases" && parts[4] == "latest":
		allowed = u.RawQuery == ""
	case len(parts) == 6 && parts[3] == "releases" && parts[4] == "tags":
		allowed = u.RawQuery == ""
	case len(parts) == 7 && parts[3] == "git" && parts[4] == "ref" && parts[5] == "tags":
		allowed = u.RawQuery == ""
	case len(parts) == 6 && parts[3] == "git" && parts[4] == "tags":
		allowed = u.RawQuery == "" && commitPattern.MatchString(parts[5])
	}
	if !allowed {
		return "", fmt.Errorf("unsupported GitHub metadata endpoint")
	}
	return strings.ToLower(parts[1] + "/" + parts[2]), nil
}
