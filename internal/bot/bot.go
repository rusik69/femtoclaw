package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v69/github"
	"github.com/rusik69/femtoclaw/internal/config"
	"github.com/rusik69/femtoclaw/internal/tools"
	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
	"golang.org/x/oauth2"
)

// Bot handles the Telegram bot logic
type Bot struct {
	api          *tgbotapi.BotAPI
	openaiClient *openai.Client
	githubClient *github.Client
	cfg          *config.Config
	historyMu    sync.Mutex
	history      map[int64][]openai.ChatCompletionMessage
	taskMu       sync.Mutex
	tasks        map[string]*Task
	nextTaskID   int64
}

type TaskStatus string

const (
	TaskQueued  TaskStatus = "queued"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskError   TaskStatus = "error"
)

type Task struct {
	ID        string
	ChatID    int64
	Request   string
	Status    TaskStatus
	CreatedAt time.Time
	UpdatedAt time.Time
	Result    string
	Error     string
}

// NewBot initializes a new Bot instance
func NewBot(cfg *config.Config) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	config := openai.DefaultConfig(cfg.OpenAIToken)
	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
	}
	openaiClient := openai.NewClientWithConfig(config)

	// Initialize GitHub client
	var ghClient *github.Client
	if cfg.GitHubToken != "" {
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: cfg.GitHubToken},
		)
		tc := oauth2.NewClient(ctx, ts)
		ghClient = github.NewClient(tc)
	}

	return &Bot{
		api:          bot,
		openaiClient: openaiClient,
		githubClient: ghClient,
		cfg:          cfg,
		history:      make(map[int64][]openai.ChatCompletionMessage),
		tasks:        make(map[string]*Task),
	}, nil
}

// Start runs the bot loop
func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil || update.Message.From == nil { // ignore any non-Message updates
			continue
		}

		if !b.isAllowed(update.Message.From.UserName) {
			log.Printf("User %s tried to access the bot but is not allowed", update.Message.From.UserName)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "I'm sorry, I generally don't talk to strangers.")
			b.api.Send(msg)
			continue
		}

		log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

		// Process the message
		go b.handleMessage(update.Message)
	}
}

func (b *Bot) isAllowed(username string) bool {
	if len(b.cfg.AllowedUsers) == 0 {
		return true // Allow all if no list provided (minimalistic)
	}
	for _, u := range b.cfg.AllowedUsers {
		if u == username {
			return true
		}
	}
	return false
}

const maxHistoryMessages = 40

func (b *Bot) newTaskID() string {
	b.taskMu.Lock()
	defer b.taskMu.Unlock()
	b.nextTaskID++
	return fmt.Sprintf("T%06d", b.nextTaskID)
}

func (b *Bot) setTaskStatus(taskID string, status TaskStatus, result string, errText string) {
	b.taskMu.Lock()
	defer b.taskMu.Unlock()

	task, ok := b.tasks[taskID]
	if !ok {
		return
	}
	task.Status = status
	task.UpdatedAt = time.Now()
	if result != "" {
		task.Result = result
	}
	if errText != "" {
		task.Error = errText
	}
}

func (b *Bot) getTask(taskID string) (*Task, bool) {
	b.taskMu.Lock()
	defer b.taskMu.Unlock()
	task, ok := b.tasks[taskID]
	if !ok {
		return nil, false
	}
	copy := *task
	return &copy, true
}

func (b *Bot) getHistory(chatID int64) []openai.ChatCompletionMessage {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()

	history := b.history[chatID]
	if len(history) == 0 {
		return nil
	}

	return fixToolCallSequences(append([]openai.ChatCompletionMessage(nil), history...))
}

// sanitizeMessages ensures no message has empty Content, which some APIs reject as null.
func sanitizeMessages(msgs []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if m.Content == "" && len(m.MultiContent) == 0 {
			out[i].Content = " "
		}
	}
	return out
}

// fixToolCallSequences rebuilds the message list keeping only valid sequences.
// An assistant message with tool_calls is kept only when every one of its
// tool_call IDs has a matching tool response immediately following it.
// Only tool responses whose ToolCallID belongs to the assistant are included;
// stray tool messages with unrecognised IDs are dropped.
func fixToolCallSequences(msgs []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	var out []openai.ChatCompletionMessage
	i := 0
	for i < len(msgs) {
		m := msgs[i]
		if m.Role == openai.ChatMessageRoleAssistant && len(m.ToolCalls) > 0 {
			expected := make(map[string]bool, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				expected[tc.ID] = true
			}
			j := i + 1
			var matched []openai.ChatCompletionMessage
			for j < len(msgs) && msgs[j].Role == openai.ChatMessageRoleTool {
				if expected[msgs[j].ToolCallID] {
					matched = append(matched, msgs[j])
					delete(expected, msgs[j].ToolCallID)
				}
				j++
			}
			if len(expected) == 0 {
				out = append(out, m)
				out = append(out, matched...)
			}
			i = j
		} else if m.Role == openai.ChatMessageRoleTool {
			i++
		} else {
			out = append(out, m)
			i++
		}
	}
	return out
}

// appendHistoryBatch appends multiple messages atomically so that an assistant
// message with tool_calls and all its tool responses are never interleaved with
// messages from concurrently-running tasks.
func (b *Bot) appendHistoryBatch(chatID int64, msgs ...openai.ChatCompletionMessage) {
	if len(msgs) == 0 {
		return
	}
	b.historyMu.Lock()
	defer b.historyMu.Unlock()

	b.history[chatID] = append(b.history[chatID], msgs...)
	if len(b.history[chatID]) > maxHistoryMessages {
		trimmed := b.history[chatID][len(b.history[chatID])-maxHistoryMessages:]
		b.history[chatID] = fixToolCallSequences(trimmed)
	}
}

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
						Description: "The search query. Must include '-linked:pr' to skip issues with existing PRs (e.g., 'is:issue is:open -linked:pr label:\"good first issue\" language:go').",
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
			Description: "Create a Pull Request.",
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
						Description: "The title of the PR.",
					},
					"body": {
						Type:        jsonschema.String,
						Description: "The body of the PR.",
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

// handleMessage processes a single message using OpenAI with tools
func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(text, "/status") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			reply := tgbotapi.NewMessage(chatID, "Usage: /status <task_id>")
			reply.ReplyToMessageID = msg.MessageID
			b.api.Send(reply)
			return
		}
		taskID := parts[1]
		if task, ok := b.getTask(taskID); ok {
			status := fmt.Sprintf("Task %s: %s", task.ID, task.Status)
			if task.Status == TaskError && task.Error != "" {
				status += fmt.Sprintf("\nError: %s", task.Error)
			}
			if task.Status == TaskDone && task.Result != "" {
				status += fmt.Sprintf("\nResult: %s", task.Result)
			}
			reply := tgbotapi.NewMessage(chatID, status)
			reply.ReplyToMessageID = msg.MessageID
			b.api.Send(reply)
			return
		}
		reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Task %s not found", taskID))
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
		return
	}

	taskID := b.newTaskID()
	now := time.Now()
	b.taskMu.Lock()
	b.tasks[taskID] = &Task{
		ID:        taskID,
		ChatID:    chatID,
		Request:   msg.Text,
		Status:    TaskQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	b.taskMu.Unlock()

	queuedMsg := fmt.Sprintf("Queued task %s. I'll report back when done. Use /status %s for updates.", taskID, taskID)
	reply := tgbotapi.NewMessage(chatID, queuedMsg)
	reply.ReplyToMessageID = msg.MessageID
	b.api.Send(reply)

	go b.runTask(taskID, msg)
}

func (b *Bot) runTask(taskID string, msg *tgbotapi.Message) {
	defer func() {
		if r := recover(); r != nil {
			b.setTaskStatus(taskID, TaskError, "", fmt.Sprintf("panic: %v", r))
		}
	}()

	b.setTaskStatus(taskID, TaskRunning, "", "")
	result, err := b.processMessage(msg)
	if err != nil {
		b.setTaskStatus(taskID, TaskError, "", err.Error())
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Task %s failed: %v", taskID, err))
		reply.ReplyToMessageID = msg.MessageID
		b.api.Send(reply)
		return
	}

	b.setTaskStatus(taskID, TaskDone, result, "")
	if result == "" {
		result = "Task completed."
	}
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Task %s completed:\n%s", taskID, result))
	reply.ReplyToMessageID = msg.MessageID
	b.api.Send(reply)
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
7. github_create_pr — open a PR from <your_fork_user>:fix-<issue> against the upstream default branch. This step is MANDATORY — never end a task without creating the PR.
8. Report the PR URL.

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
	for {
		resp, err := b.openaiClient.CreateChatCompletion(
			ctx,
			openai.ChatCompletionRequest{
				Model:    b.cfg.Model,
				Messages: sanitizeMessages(fixToolCallSequences(messages)),
				Tools:    openAITools,
			},
		)

		if err != nil {
			log.Printf("ChatCompletion error: %v", err)
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

			log.Printf("Tool call: %s %s", toolCall.Function.Name, toolCall.Function.Arguments)

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
					// Handle number as float64 because JSON
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

			// Collect tool response
			toolMessage := openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    output,
				ToolCallID: toolCall.ID,
			}
			messages = append(messages, toolMessage)
			batchMsgs = append(batchMsgs, toolMessage)
		}
		// Write assistant + all tool responses as one atomic batch.
		b.appendHistoryBatch(chatID, batchMsgs...)
	}

	return finalReply, nil
}
