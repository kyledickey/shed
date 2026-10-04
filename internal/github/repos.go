package github

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	gh "github.com/google/go-github/v92/github"
)

const perPage = 100

// Repo is a repository the App can access.
type Repo struct {
	FullName      string
	DefaultBranch string
	Private       bool
}

// Repos returns the repositories of every installation of the App, sorted by
// full name.
func (c *Client) Repos(ctx context.Context) ([]Repo, error) {
	app, err := c.appClient()
	if err != nil {
		return nil, err
	}
	var installs []int64
	for inst, err := range app.Apps.ListInstallationsIter(ctx, &gh.ListOptions{PerPage: perPage}) {
		if err != nil {
			return nil, fmt.Errorf("github: list installations: %w", err)
		}
		installs = append(installs, inst.GetID())
	}

	var repos []Repo
	for _, id := range installs {
		client, err := c.installationClient(ctx, id)
		if err != nil {
			return nil, err
		}
		for r, err := range client.Apps.ListReposIter(ctx, &gh.ListOptions{PerPage: perPage}) {
			if err != nil {
				return nil, fmt.Errorf("github: list repositories of installation %d: %w", id, err)
			}
			repos = append(repos, Repo{FullName: r.GetFullName(), DefaultBranch: r.GetDefaultBranch(), Private: r.GetPrivate()})
		}
	}
	slices.SortFunc(repos, func(a, b Repo) int { return strings.Compare(a.FullName, b.FullName) })
	return repos, nil
}

// Branches returns the branch names of the repository fullName (owner/name).
func (c *Client) Branches(ctx context.Context, fullName string) ([]string, error) {
	client, owner, repo, err := c.repoClient(ctx, fullName)
	if err != nil {
		return nil, err
	}
	var names []string
	opts := &gh.BranchListOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	for b, err := range client.Repositories.ListBranchesIter(ctx, owner, repo, opts) {
		if err != nil {
			return nil, fmt.Errorf("github: list branches of %s: %w", fullName, err)
		}
		names = append(names, b.GetName())
	}
	return names, nil
}

// Commit describes a git commit.
type Commit struct {
	SHA string
	// Message is the first line of the commit message.
	Message string
	Author  string
}

// Commit returns the commit that ref, a branch name or SHA, points to in the
// repository fullName (owner/name).
func (c *Client) Commit(ctx context.Context, fullName, ref string) (Commit, error) {
	client, owner, repo, err := c.repoClient(ctx, fullName)
	if err != nil {
		return Commit{}, err
	}
	rc, _, err := client.Repositories.GetCommit(ctx, owner, repo, ref, nil)
	if err != nil {
		return Commit{}, fmt.Errorf("github: get commit %s@%s: %w", fullName, ref, err)
	}
	author := rc.GetCommit().GetAuthor().GetName()
	if author == "" {
		author = rc.GetAuthor().GetLogin()
	}
	return Commit{SHA: rc.GetSHA(), Message: firstLine(rc.GetCommit().GetMessage()), Author: author}, nil
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// CloneURL returns an HTTPS URL for cloning the repository fullName
// (owner/name) that embeds a short-lived installation token. It is a secret.
func (c *Client) CloneURL(ctx context.Context, fullName string) (string, error) {
	id, owner, repo, err := c.repoInstallation(ctx, fullName)
	if err != nil {
		return "", err
	}
	token, err := c.installationToken(ctx, id)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(c.webURL)
	if err != nil {
		return "", fmt.Errorf("github: parse web URL: %w", err)
	}
	u.User = url.UserPassword("x-access-token", token)
	u.Path = "/" + owner + "/" + repo + ".git"
	return u.String(), nil
}

// CIState is the aggregate state of the CI results for a commit.
type CIState string

// CI states.
const (
	// CIPending means some check or status has not finished and none failed.
	CIPending CIState = "pending"
	// CISuccess means every check and status passed.
	CISuccess CIState = "success"
	// CIFailure means at least one check or status failed.
	CIFailure CIState = "failure"
	// CINone means the commit has no checks or statuses. Checks may simply
	// not have been created yet.
	CINone CIState = "none"
)

// CIStatus combines the check runs and commit statuses of commit sha in the
// repository fullName (owner/name). A failure outranks pending results, so
// callers can stop waiting as soon as anything fails.
func (c *Client) CIStatus(ctx context.Context, fullName, sha string) (CIState, error) {
	client, owner, repo, err := c.repoClient(ctx, fullName)
	if err != nil {
		return "", err
	}
	var seen, pending, failed bool

	runs := client.Checks.ListCheckRunsForRefIter(ctx, owner, repo, sha,
		&gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: perPage}})
	for run, err := range runs {
		if err != nil {
			return "", fmt.Errorf("github: list check runs of %s@%s: %w", fullName, sha, err)
		}
		seen = true
		if run.GetStatus() != "completed" {
			pending = true
			continue
		}
		switch run.GetConclusion() {
		case "success", "neutral", "skipped":
		default:
			failed = true
		}
	}

	combined, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, sha, nil)
	if err != nil {
		return "", fmt.Errorf("github: get combined status of %s@%s: %w", fullName, sha, err)
	}
	if combined.GetTotalCount() > 0 {
		seen = true
		switch combined.GetState() {
		case "success":
		case "pending":
			pending = true
		default: // failure or error
			failed = true
		}
	}

	switch {
	case failed:
		return CIFailure, nil
	case pending:
		return CIPending, nil
	case seen:
		return CISuccess, nil
	}
	return CINone, nil
}
