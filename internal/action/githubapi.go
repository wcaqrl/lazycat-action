package action

import (
	"context"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/githubapi"
)

func metadataGateway(getenv func(string) string) func(context.Context, config.Config) (*githubapi.Gateway, error) {
	return func(ctx context.Context, cfg config.Config) (*githubapi.Gateway, error) {
		token := strings.TrimSpace(getenv("GITHUB_TOKEN"))
		if token == "" {
			return nil, nil
		}
		repositories := []string{}
		for _, value := range []string{cfg.Source.URL, cfg.Changelog.GitURL} {
			if repo := githubapi.Repository(value); repo != "" {
				repositories = append(repositories, repo)
			}
		}
		if len(repositories) == 0 {
			return nil, nil
		}
		return githubapi.Start(ctx, githubapi.Client{Token: token}, repositories)
	}
}
