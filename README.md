# FemtoClaw

FemtoClaw is a minimalistic AI assistant service, inspired by OpenClaw, written in Go.
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
    export GITHUB_USER="your-github-username" # Optional: used as fork base and PR head
    export ALLOWED_USERS="user1,user2" # Optional: comma-separated list of allowed telegram usernames
    export WORKDIR="./work" # Optional: base directory for git/shell tools
    ```

3.  Run the bot:

    ```bash
    go run main.go
    ```

## Podman Compose

The container runs in `/work` and mounts `./work` from the host. Set `WORKDIR` if you want a different base path.

The image includes npm, python3, make, gcc, and git. Git identity is configured at startup from `GIT_USER_NAME` and `GIT_USER_EMAIL` (defaults: "FemtoClaw Bot", "femtoclaw@bot.local"). `GITHUB_TOKEN` is used for git push auth.

```bash
podman compose up -d --build
```

Optional env vars for container (add to `.env`):

- `GIT_USER_NAME` — git commit author name (default: FemtoClaw Bot)
- `GIT_USER_EMAIL` — git commit author email (default: femtoclaw@bot.local)
- `PR_POLL_INTERVAL` — seconds between PR comment polls (default: 60)
- `DB_PATH` — path to SQLite database for tracked PRs (default: `$WORKDIR/femtoclaw.db`). Persists across redeploys when `./work` is mounted.

## Features

-   **Minimalistic**: Single file logic.
-   **Local File Management**: List, read, write files.
-   **Git/Shell Integration**: Clone repos, run tests, build projects.
-   **GitHub Integration**:
    -   **Find Issues**: Search for issues by label, language, or any criteria.
    -   **Fork Repos**: Fork interesting projects directly.
    -   **Solve & PR**: Autonomous workflow to clone, fix, push, and create a Pull Request.
    -   **Comment**: Interact with issues and PRs.
    -   **PR comment watcher**: Automatically replies to comments on PRs created by FemtoClaw (requires `GITHUB_USER`). Tracked PRs are persisted in SQLite under `$WORKDIR` so they survive redeploys.
-   **Vibecode**: Analyze project structure and content.

## Usage Examples

- "Find open issues in golang" or "Find bug issues in python"
- "Fork github.com/some/repo"
- "Clone the repo and run tests"
- "Fix the bug in main.go where..."
- "Create a PR with title 'Fix bug' and body 'fixed typo' from branch 'fix-typo' to 'main'"
- "Comment on issue #42 saying 'I am working on this'"
