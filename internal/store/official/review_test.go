package official

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestFindReviewUsesOriginalCreationWindowAndPaginates(t *testing.T) {
	created := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/sdk/v3/developer/app/dev.example.app/review/list" || r.Header.Get("X-API-Token") != "test-token" || r.Header.Get("X-User-Token") != "" {
			t.Errorf("request path/auth mismatch")
		}
		q := r.URL.Query()
		if q.Get("created_at_start") != "2026-10-01 09:55:00" || q.Get("created_at_end") != "2026-10-01 10:05:00" || q.Get("sort") != "-id" {
			t.Errorf("query=%v", q)
		}
		items := []map[string]any{{"id": 111, "status": 0}}
		if q.Get("page") == "1" {
			items = []map[string]any{{"id": 222, "status": -1, "reason": "fix screenshots", "created_at": created, "updated_at": created.Add(7 * 24 * time.Hour), "version": map[string]string{"name": "1.4.2", "package": "dev.example.app"}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errorCode": 0, "data": map[string]any{"items": items, "total": 51}})
	}))
	defer server.Close()
	review, err := (Publisher{BaseURL: server.URL, SDK: true, HTTPClient: server.Client()}).FindReview(t.Context(), auth.StaticToken("test-token"), ReviewLookup{PackageID: "dev.example.app", ID: 222, CreatedAt: created})
	if err != nil || review == nil || review.ID != 222 || review.StatusName() != "rejected" || requests != 2 {
		t.Fatalf("review=%#v requests=%d err=%v", review, requests, err)
	}
}

func TestFindReviewRecoveryRequiresVersionHashAndSubmissionChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("created_at_start") == "" || q.Get("created_at_end") == "" {
			t.Error("recovery must limit creation range")
		}
		fmt.Fprint(w, `{"errorCode":0,"data":{"total":3,"items":[{"id":33,"submit_channel":3,"version":{"name":"1.4.2","pkg_hash":"abc"}},{"id":22,"submit_channel":5,"version":{"name":"1.4.2","pkg_hash":"wrong"}},{"id":11,"submit_channel":5,"status":-1,"version":{"name":"1.4.2","pkg_hash":"abc"}}]}}`)
	}))
	defer server.Close()
	review, err := (Publisher{BaseURL: server.URL, SDK: true, HTTPClient: server.Client()}).FindReview(t.Context(), auth.StaticToken("test-token"), ReviewLookup{PackageID: "dev.example.app", Version: "1.4.2", SHA256: "abc", StartedAt: time.Now().Add(-time.Hour)})
	if err != nil || review == nil || review.ID != 11 {
		t.Fatalf("review=%#v err=%v", review, err)
	}
}

func TestSubmitReviewPreservesSDKReviewID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"review":{"id":73,"status":0,"created_at":"2026-10-09T10:00:00+08:00"},"canceled_reviews":[]}`)
	}))
	defer server.Close()
	review, err := submitReview(t.Context(), server.Client(), server.URL, "token", appstore.UploadInfo{Package: "dev.example.app", Version: "1.4.2"}, nil, map[string]string{"en": "release"})
	if err != nil || review == nil || review.ID != 73 || review.CreatedAt.IsZero() {
		t.Fatalf("review=%#v err=%v", review, err)
	}
}

func TestInvalidReviewListDoesNotPretendNoReviewExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"errorCode":0,"data":{}}`) }))
	defer server.Close()
	_, err := (Publisher{BaseURL: server.URL, SDK: true, HTTPClient: server.Client()}).FindReview(t.Context(), auth.StaticToken("token"), ReviewLookup{PackageID: "dev.example.app", Version: "1.4.2"})
	if err == nil {
		t.Fatal("missing pagination metadata must fail closed")
	}
}
