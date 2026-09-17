package httpx

import (
	"net/http"
	"time"
)

func NoRedirect(source *http.Client, timeout time.Duration) *http.Client {
	client := http.Client{}
	if source != nil {
		client = *source
	}
	if client.Timeout <= 0 {
		client.Timeout = timeout
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}
