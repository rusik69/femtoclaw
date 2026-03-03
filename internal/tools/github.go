package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v69/github"
)

// GithubSearchIssues searches for GitHub issues.
func GithubSearchIssues(client *github.Client, query string) (string, error) {
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

// GithubForkRepo forks a GitHub repository.
func GithubForkRepo(client *github.Client, owner, repo string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("github client not initialized")
	}
	ctx := context.Background()
	repoObj, _, err := client.Repositories.CreateFork(ctx, owner, repo, nil)
	if err != nil {
		return "", err // Let the caller fix error formatting if needed
	}
	return fmt.Sprintf("Fork created: %s (Clone URL: %s)", *repoObj.HTMLURL, *repoObj.CloneURL), nil
}

// GithubCreatePR creates a PR on GitHub.
func GithubCreatePR(client *github.Client, owner, repo, title, body, head, base string) (string, error) {
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
	pr, _, err := client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("PR created: %s", *pr.HTMLURL), nil
}

// GithubCommentIssue comments on a GitHub issue or PR.
func GithubCommentIssue(client *github.Client, owner, repo string, number int, body string) (string, error) {
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
