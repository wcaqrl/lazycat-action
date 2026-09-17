package platformauth

import (
	"context"
	"errors"
	"os"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

type Result struct {
	Provider auth.TokenProvider
	Protocol Protocol
	BaseURL  string
}

type Protocol string

const (
	ProtocolPAT           Protocol = "pat"
	ProtocolLegacySession Protocol = "legacy-session"
)

type Resolver struct {
	LookupEnv func(string) (string, bool)
}

func (resolver Resolver) Resolve(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, authError(lpkgo.CodeInvalidArgument, errors.New("context is required"))
	}
	if err := ctx.Err(); err != nil {
		return Result{}, authError(lpkgo.CodeCancelled, err)
	}
	lookup := resolver.lookupEnv()
	token, protocol := credentials(lookup)
	if token == "" {
		return Result{}, authError(lpkgo.CodeUnauthenticated, errors.New("LZC_API_TOKEN PAT or legacy LAZYCAT_TOKEN session token is required"))
	}
	baseURL := ""
	if protocol == ProtocolPAT {
		var err error
		baseURL, err = appstore.ResolvePATBaseURL(environmentValue(lookup, "LZC_API_HOST"))
		if err != nil {
			return Result{}, authError(lpkgo.CodeInvalidArgument, err)
		}
	}
	return Result{Provider: auth.StaticToken(token), Protocol: protocol, BaseURL: baseURL}, nil
}

func (resolver Resolver) lookupEnv() func(string) (string, bool) {
	lookup := resolver.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return lookup
}

func credentials(lookup func(string) (string, bool)) (string, Protocol) {
	if token := environmentValue(lookup, "LZC_API_TOKEN"); token != "" {
		return token, ProtocolPAT
	}
	token := environmentValue(lookup, "LAZYCAT_TOKEN")
	if token == "" {
		return "", ProtocolPAT
	}
	return token, ProtocolLegacySession
}

func environmentValue(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func authError(code lpkgo.Code, cause error) error {
	return &lpkgo.Error{Code: code, Op: "platformauth.resolve", Cause: cause}
}
