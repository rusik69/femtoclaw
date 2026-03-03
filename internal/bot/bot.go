package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/go-github/v69/github"
	"github.com/rusik69/nanoclaw/internal/config"
	"github.com/rusik69/nanoclaw/internal/tools"
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
		if cfg.GitHubBaseURL != "" {
			ghClient.BaseURL, _ = url.Parse(cfg.GitHubBaseURL)
		}
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

	return append([]openai.ChatCompletionMessage(nil), history...)
}

func (b *Bot) appendHistory(chatID int64, msg openai.ChatCompletionMessage) {
	b.historyMu.Lock()
	defer b.historyMu.Unlock()

	b.history[chatID] = append(b.history[chatID], msg)
	if len(b.history[chatID]) > maxHistoryMessages {
		b.history[chatID] = b.history[chatID][len(b.history[chatID])-maxHistoryMessages:]
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
			Description: "Search for GitHub issues.",
			Parameters: jsonschema.Definition{
				Type: jsonschema.Object,
				Properties: map[string]jsonschema.Definition{
					"query": {
						Type:        jsonschema.String,
						Description: "The search query (e.g., 'is:issue is:open label:\"good first issue\" language:go').",
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
			Description: "Fork a GitHub repository.",
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

	systemPrompt := fmt.Sprintf(`You are NanoClaw, an autonomous AI coding assistant.
	Act as a task agent: keep a short plan, execute tools, and report results back concisely.
	Maintain context across the conversation and use prior tool outputs and decisions.

You can:
1. Search GitHub issues.
2. Fork repositories.
3. Fix bugs by:
   - cloning the repo (run_git_command "clone ...")
   - analyzing code (vibecode, read_file)
   - running tests (run_shell_command "go test", "npm test", etc.)
   - creating a new branch (run_git_command "checkout -b fix-branch")
   - editing files (write_file)
   - committing and pushing changes (run_git_command "add .", "commit...", "push origin fix-branch")
4. Create Pull Requests back to the upstream repo.
5. Comment on PRs/Issues.

When solving an issue, always clone the repo first to a temporary directory if possible or just current dir if empty/allowed.
If the user asks for a full fix + PR, execute the full workflow and report each step with the resulting URLs.
Always provide a brief plan before acting and a concise final report with key outputs/URLs when done.
When running git or shell tools in a cloned repo, pass the repo path via the "cwd" argument.
Default working directory: %s.
Be careful with shell commands.`, b.cfg.WorkDir)

	// 1. Prepare conversation history with system prompt that guides the behavior
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}

	history := b.getHistory(chatID)
	messages = append(messages, history...)

	userMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: msg.Text,
	}
	messages = append(messages, userMessage)
	b.appendHistory(chatID, userMessage)

	// Loop to handle tool calls
	var finalReply string
	for {
		resp, err := b.openaiClient.CreateChatCompletion(
			ctx,
			openai.ChatCompletionRequest{
				Model:    b.cfg.Model,
				Messages: messages,
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
		b.appendHistory(chatID, msgContent)

		// If simple content, send it
		if msgContent.Content != "" {
			finalReply = msgContent.Content
		}

		// If no tool calls, we are done
		if len(msgContent.ToolCalls) == 0 {
			break
		}

		// Handle tool calls
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
					output, err = tools.GithubForkRepo(b.githubClient, owner, repo)
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

			// Append tool response
			toolMessage := openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    output,
				ToolCallID: toolCall.ID,
			}
			messages = append(messages, toolMessage)
			b.appendHistory(chatID, toolMessage)
		}
	}

	return finalReply, nil
}
