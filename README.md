# NanoClaw

NanoClaw is a minimalistic AI assistant service, inspired by OpenClaw, written in Go.
It integrates with Telegram and OpenAI to provide a chat interface capable of managing local files, git repositories, and interacting with GitHub.

## Prerequisites

- Go 1.25+
- Telegram Bot Token
- OpenAI API Key
- GitHub Personal Access Token

## Setup

1.  Clone the repository.
2.  Set the following environment variables:

    ```bash
    export TELEGRAM_API_KEY="your-telegram-bot-token"
    export OPENAI_API_KEY="your-openai-api-key"
    export OPENAI_BASE_URL="https://api.openai.com/v1" # Optional: custom OpenAI-compatible base URL (e.g., http://172.19.12.109:1234/v1 for LMCloud)
    export OPENAI_MODEL="gpt-4-turbo-preview" # Optional: model name (e.g., qwen/qwen3-coder-next)
    export GITHUB_TOKEN="your-github-token"
    export GITHUB_BASE_URL="" # Optional: GitHub Enterprise API URL (e.g., https://github.example.com/api/v3)
    export ALLOWED_USERS="user1,user2" # Optional: comma-separated list of allowed telegram usernames
    export WORKDIR="./work" # Optional: base directory for git/shell tools
    ```

3.  Run the bot:

    ```bash
    go run main.go
    ```

## Docker Compose

The container runs in `/work` and mounts `./work` from the host. Set `WORKDIR` if you want a different base path.

```bash
docker compose up -d --build
```

## Features

-   **Minimalistic**: Single file logic.
-   **Local File Management**: List, read, write files.
-   **Git/Shell Integration**: Clone repos, run tests, build projects.
-   **GitHub Integration**:
    -   **Find Issues**: Search for "good first issue" or any other criteria.
    -   **Fork Repos**: Fork interesting projects directly.
    -   **Solve & PR**: Autonomous workflow to clone, fix, push, and create a Pull Request.
    -   **Comment**: Interact with issues and PRs.
-   **Vibecode**: Analyze project structure and content.

## Usage Examples

- "Find open issues labeled 'good first issue' in golang"
- "Fork github.com/some/repo"
- "Clone the repo and run tests"
- "Fix the bug in main.go where..."
- "Create a PR with title 'Fix bug' and body 'fixed typo' from branch 'fix-typo' to 'main'"
- "Comment on issue #42 saying 'I am working on this'"
