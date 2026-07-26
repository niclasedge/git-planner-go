package hub

import "errors"

var (
	errNoRepos = errors.New("no repositories found: check token scopes or set github.repos in config.yaml")
	// errAllReposFailed marks a systemic failure rather than one bad repo. The
	// section keeps its previous data and backs off instead of blanking the page.
	errAllReposFailed = errors.New("every repository failed to refresh")
)
