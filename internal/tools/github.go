package tools

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/google/go-github/v69/github"
)

// githubSleep sleeps 60–600 seconds randomly before GitHub API calls to avoid rate limiting.
func githubSleep() {
	d := time.Duration(60+rand.Intn(541)) * time.Second
	log.Printf("[github] sleeping %v before API call", d)
	time.Sleep(d)
}

// GithubSearchIssues searches for GitHub issues.
func GithubSearchIssues(client *github.Client, query string) (string, error) {
	githubSleep()
	if client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	ctx := context.Background()
	opts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 5},
	}
	result, _, err := client.Search.Issues(ctx, query, opts)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d issues:\n", *result.Total))
	for _, issue := range result.Issues {
		repoURL := ""
		if issue.RepositoryURL != nil {
			repoURL = *issue.RepositoryURL
		}
		sb.WriteString(fmt.Sprintf("- #%d %s (Repo: %s)\n  URL: %s\n", *issue.Number, *issue.Title, repoURL, *issue.HTMLURL))
	}
	return sb.String(), nil
}

// GithubForkRepo returns the fork URL for a repository.
// If githubUser is set, assumes the fork already exists at github.com/githubUser/repo and returns immediately.
// If forkURL is non-empty, uses that directly.
// Falls back to creating via API only if neither is available.
func GithubForkRepo(client *github.Client, owner, repo, forkURL, githubUser string) (string, error) {
	if forkURL != "" {
		url := strings.TrimSpace(forkURL)
		url = strings.TrimSuffix(url, ".git")
		if !strings.HasPrefix(url, "https://github.com/") && !strings.HasPrefix(url, "http://github.com/") {
			url = "https://github.com/" + strings.TrimPrefix(strings.TrimPrefix(url, "github.com/"), "/")
		}
		cloneURL := url + ".git"
		return fmt.Sprintf("Using fork: %s (Clone URL: %s)", url, cloneURL), nil
	}
	githubSleep()
	if githubUser != "" {
		url := fmt.Sprintf("https://github.com/%s/%s", githubUser, repo)
		cloneURL := url + ".git"
		// Trigger fork creation in case it doesn't exist yet; ignore "job scheduled" response.
		if client != nil {
			ctx := context.Background()
			existing, _, getErr := client.Repositories.Get(ctx, githubUser, repo)
			if getErr != nil {
				// Fork doesn't exist yet — trigger creation and wait for provisioning.
				client.Repositories.CreateFork(ctx, owner, repo, nil) //nolint:errcheck
				time.Sleep(30 * time.Second)
			} else {
				_ = existing
			}
		}
		return fmt.Sprintf("Fork ready: %s (Clone URL: %s)", url, cloneURL), nil
	}
	if client == nil {
		return "", fmt.Errorf("github client not initialized (set GITHUB_USER to skip fork API)")
	}
	ctx := context.Background()
	repoObj, _, err := client.Repositories.CreateFork(ctx, owner, repo, nil)
	if err != nil {
		return "", fmt.Errorf("fork API failed: %w. Set GITHUB_USER env var to skip fork creation and push directly", err)
	}
	return fmt.Sprintf("Fork created: %s (Clone URL: %s)", *repoObj.HTMLURL, *repoObj.CloneURL), nil
}

// GithubCreatePR creates a PR on GitHub, retrying up to 3 times on transient failures.
func GithubCreatePR(client *github.Client, owner, repo, title, body, head, base string) (string, error) {
	githubSleep()
	if client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	ctx := context.Background()
	newPR := &github.NewPullRequest{
		Title: &title,
		Body:  &body,
		Head:  &head,
		Base:  &base,
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
		if err == nil {
			return fmt.Sprintf("PR created: %s", *pr.HTMLURL), nil
		}
		lastErr = err
		if attempt < 4 {
			time.Sleep(time.Duration(10*(attempt+1)) * time.Second)
		}
	}
	return "", fmt.Errorf("PR creation failed after 5 attempts: %w", lastErr)
}

// GithubListPRComments lists comments on a PR (issue) since the given time.
func GithubListPRComments(client *github.Client, owner, repo string, number int, since time.Time) ([]*github.IssueComment, error) {
	githubSleep()
	if client == nil {
		return nil, fmt.Errorf("github client not initialized")
	}
	ctx := context.Background()
	opts := &github.IssueListCommentsOptions{Since: &since}
	comments, _, err := client.Issues.ListComments(ctx, owner, repo, number, opts)
	if err != nil {
		return nil, err
	}
	return comments, nil
}

// GithubCommentIssue comments on a GitHub issue or PR.
func GithubCommentIssue(client *github.Client, owner, repo string, number int, body string) (string, error) {
	githubSleep()
	if client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	ctx := context.Background()
	comment := &github.IssueComment{Body: &body}
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, number, comment)
	if err != nil {
		return "", err
	}
	return "Comment posted successfully", nil
}

// GithubWriteFile writes or updates a file in a repository (for GitHub Pages logging).
func GithubWriteFile(client *github.Client, owner, repo, path, content, message, branch string) (string, error) {
	githubSleep()
	if client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	ctx := context.Background()

	if branch == "" {
		branch = "main"
	}

	// Try to get existing file to get its SHA (for updates)
	fileContent, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: branch})
	if err != nil && !is404(err) {
		return "", err
	}

	var sha string
	if fileContent != nil {
		sha = *fileContent.SHA
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, path, &github.RepositoryContentFileOptions{
		Message: &message,
		Content: []byte(content),
		Branch:  &branch,
		SHA:     &sha,
	})
	if err != nil {
		return "", err
	}
	_ = resp // silence unused variable warning
	return fmt.Sprintf("File %s updated in %s/%s", path, owner, repo), nil
}

func is404(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "404")
}
