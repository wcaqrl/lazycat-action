package official

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestRetryablePublishErrorRejectsForbiddenCodes(t *testing.T) {
	for _, code := range []lpkgo.Code{
		lpkgo.CodeInvalidArgument,
		lpkgo.CodeInvalidConfig,
		lpkgo.CodeInvalidManifest,
		lpkgo.CodeUnauthenticated,
		lpkgo.CodePermissionDenied,
		lpkgo.CodeNotFound,
		lpkgo.CodeCommandFailed,
		lpkgo.CodeIntegrityMismatch,
		lpkgo.CodeCancelled,
		lpkgo.CodeDeadlineExceeded,
	} {
		t.Run(string(code), func(t *testing.T) {
			err := &lpkgo.Error{Code: code, Retryable: true, Cause: errors.New("must not retry")}
			if retryablePublishError(err) {
				t.Fatalf("code %s was retryable", code)
			}
		})
	}
}

func TestRetryablePublishErrorRejectsStatus600EvenWhenFlagged(t *testing.T) {
	err := &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, StatusCode: 600, Retryable: true, Cause: errors.New("not HTTP 5xx")}
	if retryablePublishError(err) {
		t.Fatal("status 600 was retryable")
	}
}

func TestRetryablePublishErrorRejectsNon4294xxEvenWhenFlagged(t *testing.T) {
	err := &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, StatusCode: http.StatusTeapot, Retryable: true, Cause: errors.New("known client error")}
	if retryablePublishError(err) {
		t.Fatal("status 418 was retryable")
	}
}

func TestRetryablePublishErrorDoesNotReplayAmbiguousReviewOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{name: "review rate limit", status: http.StatusTooManyRequests, want: true},
		{name: "review server error", status: http.StatusInternalServerError, want: false},
		{name: "review network outcome unknown", status: 0, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := &lpkgo.Error{
				Code: lpkgo.CodeRemoteUnavailable, Op: "store.official.review",
				StatusCode: test.status, Retryable: true, Cause: errors.New("review failed"),
			}
			if got := retryablePublishError(err); got != test.want {
				t.Fatalf("retryable=%v, want %v", got, test.want)
			}
		})
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, time.July, 13, 8, 0, 0, 0, time.UTC)
	value := now.Add(45 * time.Second).Format(http.TimeFormat)
	delay, ok := parseRetryAfter(value, now)
	if !ok || delay != 45*time.Second {
		t.Fatalf("delay=%s ok=%v", delay, ok)
	}
}

func TestRetryAfterRecorderKeepsGreatestValidDelayAndIgnoresStatus600(t *testing.T) {
	responses := []*http.Response{
		{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"3"}}, Body: io.NopCloser(strings.NewReader(""))},
		{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"8"}}, Body: io.NopCloser(strings.NewReader(""))},
		{StatusCode: 600, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader(""))},
	}
	index := 0
	recorder := newRetryAfterRecorder(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		response := responses[index]
		index++
		return response, nil
	}))
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range responses {
		if _, err := recorder.RoundTrip(request); err != nil {
			t.Fatal(err)
		}
	}
	if delay := recorder.Delay(); delay != 8*time.Second {
		t.Fatalf("delay=%s", delay)
	}
}

func TestSafeOfficialResponseMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "message", body: `{"message":"version already pending review"}`, want: "version already pending review"},
		{name: "msg", body: `{"msg":"duplicate review"}`, want: "duplicate review"},
		{name: "error string", body: `{"error":"application rejected"}`, want: "application rejected"},
		{name: "nested error", body: `{"error":{"message":"invalid package metadata"}}`, want: "invalid package metadata"},
		{name: "single line", body: "{\"message\":\"  version\\n  already\\t pending  \"}", want: "version already pending"},
		{name: "plain text hidden", body: "version already pending review", want: ""},
		{name: "unknown JSON hidden", body: `{"detail":"not approved"}`, want: ""},
		{name: "credential marker hidden", body: `{"message":"token lcst_must_not_leak is invalid"}`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := safeOfficialResponseMessage([]byte(test.body)); got != test.want {
				t.Fatalf("message=%q, want %q", got, test.want)
			}
		})
	}

	message := safeOfficialResponseMessage([]byte(`{"message":"` + strings.Repeat("a", 1024) + `"}`))
	if message == "" || len(message) > maxOfficialResponseMessageBytes {
		t.Fatalf("bounded message length=%d", len(message))
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
