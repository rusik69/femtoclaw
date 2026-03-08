package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rusik69/femtoclaw/internal/tools"
	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

var openAITools = []openai.Tool{
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "list_files",
			Description: "List files in the current directory or a subdirectory.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"path": {
						Type:        jsonschema.String,
						Description: "The path to list files from (default: .)",
					},
					"cwd": {
						Type:        jsonschema.String,
						Description: "Working directory for relative paths (optional).",
					},
				},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "read_file",
			Description: "Read the content of a file.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"path": {
						Type:        jsonschema.String,
						Description: "The path to the file to read.",
					},
					"cwd": {
						Type:        jsonschema.String,
						Description: "Working directory for relative paths (optional).",
					},
				},
				Required: []string{"path"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "write_file",
			Description: "Write content to a file. Creates or overwrites the file.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"path": {
						Type:        jsonschema.String,
						Description: "The path to the file to write.",
					},
					"content": {
						Type:        jsonschema.String,
						Description: "The content to write to the file.",
					},
					"cwd": {
						Type:        jsonschema.String,
						Description: "Working directory for relative paths (optional).",
					},
				},
				Required: []string{"path", "content"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "run_git_command",
			Description: "Run a git command (e.g., status, add, commit, push).",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"args": {
						Type:        jsonschema.String,
						Description: "The git command arguments (e.g., 'commit -m \"message\"').",
					},
					"cwd": {
						Type:        jsonschema.String,
						Description: "Working directory for the git command (optional).",
					},
				},
				Required: []string{"args"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "run_shell_command",
			Description: "Run an arbitrary shell command (e.g., 'go test ./...', 'make build'). USE CAUTION.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"command": {
						Type:        jsonschema.String,
						Description: "The shell command to run.",
					},
					"cwd": {
						Type:        jsonschema.String,
						Description: "Working directory for the shell command (optional).",
					},
				},
				Required: []string{"command"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "vibecode",
			Description: "Analyze the project structure and key files to 'vibe check' the code.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"path": {
						Type:        jsonschema.String,
						Description: "The path to the project root (default: .)",
					},
					"cwd": {
						Type:        jsonschema.String,
						Description: "Working directory for relative paths (optional).",
					},
				},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "github_search_issues",
			Description: "Search for GitHub issues. Always include '-linked:pr' in the query to exclude issues that already have a pull request.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"query": {
						Type:        jsonschema.String,
						Description: "The search query. Must include '-linked:pr' to skip issues with existing PRs (e.g., 'is:issue is:open -linked:pr language:go' or add label:bug, label:help-wanted, etc.).",
					},
				},
				Required: []string{"query"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "github_fork_repo",
			Description: "Fork a GitHub repository. Returns the fork URL and clone URL — use the clone URL to clone the fork. Always call this BEFORE cloning when you intend to submit a PR.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"owner": {
						Type:        jsonschema.String,
						Description: "The owner of the repository to fork.",
					},
					"repo": {
						Type:        jsonschema.String,
						Description: "The name of the repository to fork.",
					},
					"fork_url": {
						Type:        jsonschema.String,
						Description: "Optional: URL of an existing fork (e.g. https://github.com/USER/repo) when API fork failed and user forked manually.",
					},
				},
				Required: []string{"owner", "repo"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "github_create_pr",
			Description: "Create a Pull Request. Always include 'Fixes #N' or 'Closes #N' in the body to link the issue.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"owner": {
						Type:        jsonschema.String,
						Description: "The owner of the repository (upstream).",
					},
					"repo": {
						Type:        jsonschema.String,
						Description: "The name of the repository.",
					},
					"title": {
						Type:        jsonschema.String,
						Description: "The title of the PR. Include the issue number, e.g. 'Fix list type resolution (#42)'.",
					},
					"body": {
						Type:        jsonschema.String,
						Description: "The body of the PR. MUST contain 'Fixes #N' (or 'Closes #N') where N is the issue number, so GitHub auto-links and auto-closes the issue when merged.",
					},
					"head": {
						Type:        jsonschema.String,
						Description: "The name of the branch where your changes are implemented.",
					},
					"base": {
						Type:        jsonschema.String,
						Description: "The name of the branch you want the changes pulled into.",
					},
				},
				Required: []string{"owner", "repo", "title", "head", "base"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "github_comment_issue",
			Description: "Comment on an issue or PR.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"owner": {
						Type:        jsonschema.String,
						Description: "The owner of the repository.",
					},
					"repo": {
						Type:        jsonschema.String,
						Description: "The name of the repository.",
					},
					"issue_number": {
						Type:        jsonschema.Integer,
						Description: "The issue or PR number.",
					},
					"body": {
						Type:        jsonschema.String,
						Description: "The comment body.",
					},
				},
				Required: []string{"owner", "repo", "issue_number", "body"},
			},
		},
	},
	{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "github_write_file",
			Description: "Write or update a file in a repository. Use for GitHub Pages logging.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"owner": {
						Type:        jsonschema.String,
						Description: "The owner of the repository.",
					},
					"repo": {
						Type:        jsonschema.String,
						Description: "The name of the repository.",
					},
					"path": {
						Type:        jsonschema.String,
						Description: "The file path (e.g., 'logs/2024-01-15.md').",
					},
					"content": {
						Type:        jsonschema.String,
						Description: "The file content to write.",
					},
					"message": {
						Type:        jsonschema.String,
						Description: "The commit message.",
					},
					"branch": {
						Type:        jsonschema.String,
						Description: "The branch name (default: main).",
					},
				},
				Required: []string{"owner", "repo", "path", "content", "message"},
			},
		},
	},
}

func (b *Bot) processMessage(msg *tgbotapi.Message) (string, error) {
	ctx := context.Background()
	chatID := msg.Chat.ID

	githubUserInfo := ""
	if b.cfg.GitHubUser != "" {
		githubUserInfo = fmt.Sprintf("\nYour GitHub username is %s. When forking, push to your fork (https://github.com/%s/REPO) and use '%s:BRANCH' as the head in PRs.", b.cfg.GitHubUser, b.cfg.GitHubUser, b.cfg.GitHubUser)
	}
	systemPrompt := fmt.Sprintf(`You are FemtoClaw, an autonomous AI coding assistant.
	Act as a task agent: keep a short plan, execute tools, and report results back concisely.
	Maintain context across the conversation and use prior tool outputs and decisions.

The REQUIRED workflow for fixing any issue is, in this exact order:
1. github_search_issues — find an open issue with no linked PR (-linked:pr).
2. github_fork_repo — fork the repo; note the clone URL in the result.
3. run_git_command "clone <fork_clone_url>" — clone the FORK, never the upstream.
4. run_git_command "checkout -b fix-<issue>" — create a branch.
5. Analyze and fix the code (vibecode, read_file, write_file).
6. run_git_command "add ." then "commit -m ..." then "push origin fix-<issue>" — commit and push to the fork.
7. github_create_pr — open a PR from <your_fork_user>:fix-<issue> against the upstream default branch. The PR body MUST include "Fixes #N" (where N is the issue number) to auto-link and auto-close the issue. This step is MANDATORY — never end a task without creating the PR.
8. Report the PR URL and the linked issue number.

Rules:
- Never clone the upstream repo; always clone your fork.
- Never skip step 7 (github_create_pr). If push succeeds, PR creation must follow immediately.
- When searching for issues, always include "-linked:pr" in the query.
- Pass the repo path via "cwd" for all git/shell commands inside the clone.
- Always provide a brief plan before acting and a concise final report with the PR URL when done.
Default working directory: %s.%s
Be careful with shell commands.`, b.cfg.WorkDir, githubUserInfo)

	// 1. Prepare conversation history with system prompt that guides the behavior
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}

	history := b.getHistory(chatID)
	messages = append(messages, history...)

	// Inject a reminder about the GitHub username right before the user message
	// so it's always in recent context regardless of history length.
	if b.cfg.GitHubUser != "" {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf("Reminder: your GitHub username is %s. Always push to https://github.com/%s/REPO and use '%s:BRANCH' as the head in PRs.", b.cfg.GitHubUser, b.cfg.GitHubUser, b.cfg.GitHubUser),
		})
	}

	userContent := msg.Text
	if userContent == "" {
		userContent = " "
	}
	userMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userContent,
	}
	messages = append(messages, userMessage)
	b.appendHistoryBatch(chatID, userMessage)

	// Loop to handle tool calls
	var finalReply string
	retried := false
	for {
		cleaned := sanitizeMessages(fixToolCallSequences(messages))
		log.Printf("[chat] chatID=%d sending %d messages to OpenAI", chatID, len(cleaned))
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		resp, err := b.openaiClient.CreateChatCompletion(
			reqCtx,
			openai.ChatCompletionRequest{
				Model:    b.cfg.Model,
				Messages: cleaned,
				Tools:    openAITools,
			},
		)
		cancel()

		if err != nil {
			roles := make([]string, len(cleaned))
			for i, m := range cleaned {
				roles[i] = m.Role
			}
			log.Printf("[chat] error chatID=%d msgs=%d roles=%v: %v", chatID, len(cleaned), roles, err)
			if !retried && strings.Contains(err.Error(), "role 'tool'") {
				log.Printf("[chat] clearing history and retrying")
				retried = true
				b.clearHistory(chatID)
				messages = []openai.ChatCompletionMessage{
					{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
					userMessage,
				}
				continue
			}
			return "", err
		}

		if len(resp.Choices) == 0 {
			return "No response choices returned", nil
		}

		choice := resp.Choices[0]
		msgContent := choice.Message

		messages = append(messages, msgContent)

		// If simple content, send it
		if msgContent.Content != "" {
			finalReply = msgContent.Content
		}

		// If no tool calls, write the assistant message to history and stop.
		if len(msgContent.ToolCalls) == 0 {
			b.appendHistoryBatch(chatID, msgContent)
			break
		}

		// Handle tool calls — collect all responses, then write the entire group atomically.
		batchMsgs := []openai.ChatCompletionMessage{msgContent}
		for _, toolCall := range msgContent.ToolCalls {
			var output string
			var err error

			log.Printf("[tool] %s %s", toolCall.Function.Name, toolCall.Function.Arguments)

			var args map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); jsonErr != nil {
				output = fmt.Sprintf("Error parsing arguments: %v", jsonErr)
			} else {
				cwd, _ := args["cwd"].(string)
				if cwd == "" {
					cwd = b.cfg.WorkDir
				}
				switch toolCall.Function.Name {
				case "list_files":
					path, ok := args["path"].(string)
					if !ok {
						output = "Error: invalid path parameter"
						break
					}
					output, err = tools.ListFiles(path, cwd)
				case "read_file":
					path, ok := args["path"].(string)
					if !ok {
						output = "Error: invalid path parameter"
						break
					}
					output, err = tools.ReadFile(path, cwd)
				case "write_file":
					path, ok := args["path"].(string)
					if !ok {
						output = "Error: invalid path parameter"
						break
					}
					content, ok := args["content"].(string)
					if !ok {
						output = "Error: invalid content parameter"
						break
					}
					output, err = tools.WriteFile(path, content, cwd)
				case "run_git_command":
					cmdArgs, ok := args["args"].(string)
					if !ok {
						output = "Error: invalid args parameter"
						break
					}
					output, err = tools.RunGitCommand(cmdArgs, cwd)
				case "run_shell_command":
					cmd, ok := args["command"].(string)
					if !ok {
						output = "Error: invalid command parameter"
						break
					}
					output, err = tools.RunShellCommand(cmd, cwd)
				case "vibecode":
					path, ok := args["path"].(string)
					if !ok {
						output = "Error: invalid path parameter"
						break
					}
					output, err = tools.Vibecode(path, cwd)
				case "github_search_issues":
					query, ok := args["query"].(string)
					if !ok {
						output = "Error: invalid query parameter"
						break
					}
					if !strings.Contains(query, "-linked:pr") {
						query += " -linked:pr"
					}
					output, err = tools.GithubSearchIssues(b.githubClient, query)
				case "github_fork_repo":
					owner, ok := args["owner"].(string)
					if !ok {
						output = "Error: invalid owner parameter"
						break
					}
					repo, ok := args["repo"].(string)
					if !ok {
						output = "Error: invalid repo parameter"
						break
					}
					forkURL, _ := args["fork_url"].(string)
					output, err = tools.GithubForkRepo(b.githubClient, owner, repo, forkURL, b.cfg.GitHubUser)
				case "github_create_pr":
					owner, ok := args["owner"].(string)
					if !ok {
						output = "Error: invalid owner parameter"
						break
					}
					repo, ok := args["repo"].(string)
					if !ok {
						output = "Error: invalid repo parameter"
						break
					}
					title, ok := args["title"].(string)
					if !ok {
						output = "Error: invalid title parameter"
						break
					}
					body, ok := args["body"].(string)
					if !ok {
						output = "Error: invalid body parameter"
						break
					}
					head, ok := args["head"].(string)
					if !ok {
						output = "Error: invalid head parameter"
						break
					}
					base, ok := args["base"].(string)
					if !ok {
						output = "Error: invalid base parameter"
						break
					}
					output, err = tools.GithubCreatePR(b.githubClient, owner, repo, title, body, head, base)
					if err == nil {
						if _, _, num, ok := parsePRURL(output); ok {
							b.registerTrackedPR(owner, repo, num, title, msg.Chat.ID)
							log.Printf("[pr-watcher] tracking %s/%s#%d", owner, repo, num)
						}
					}
				case "github_comment_issue":
					owner, ok := args["owner"].(string)
					if !ok {
						output = "Error: invalid owner parameter"
						break
					}
					repo, ok := args["repo"].(string)
					if !ok {
						output = "Error: invalid repo parameter"
						break
					}
					numFloat, ok := args["issue_number"].(float64)
					if !ok {
						output = "Error: invalid issue_number"
						break
					}
					body, ok := args["body"].(string)
					if !ok {
						output = "Error: invalid body parameter"
						break
					}
					output, err = tools.GithubCommentIssue(b.githubClient, owner, repo, int(numFloat), body)
				case "github_write_file":
					owner, ok := args["owner"].(string)
					if !ok {
						output = "Error: invalid owner parameter"
						break
					}
					repo, ok := args["repo"].(string)
					if !ok {
						output = "Error: invalid repo parameter"
						break
					}
					path, ok := args["path"].(string)
					if !ok {
						output = "Error: invalid path parameter"
						break
					}
					content, ok := args["content"].(string)
					if !ok {
						output = "Error: invalid content parameter"
						break
					}
					message, ok := args["message"].(string)
					if !ok {
						output = "Error: invalid message parameter"
						break
					}
					branch, _ := args["branch"].(string)
					output, err = tools.GithubWriteFile(b.githubClient, owner, repo, path, content, message, branch)
				default:
					output = "Unknown tool"
				}
			}

			if err != nil {
				output = fmt.Sprintf("Error: %v", err)
			}
			outLog := output
			if len(outLog) > 200 {
				outLog = outLog[:200] + "..."
			}
			log.Printf("[tool] %s output: %s", toolCall.Function.Name, outLog)

			toolMessage := openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    output,
				ToolCallID: toolCall.ID,
			}
			messages = append(messages, toolMessage)
			batchMsgs = append(batchMsgs, toolMessage)
		}
		b.appendHistoryBatch(chatID, batchMsgs...)
	}

	return finalReply, nil
}
