package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolvePath(path, cwd string) string {
	if path == "" {
		if cwd == "" {
			return "."
		}
		return cwd
	}
	if filepath.IsAbs(path) || cwd == "" {
		return path
	}
	return filepath.Join(cwd, path)
}

// ListFiles lists files in the current directory or a subdirectory.
func ListFiles(path, cwd string) (string, error) {
	path = resolvePath(path, cwd)
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.IsDir() {
			names = append(names, e.Name()+"/")
		} else {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, "\n"), nil
}

// ReadFile reads the content of a file.
func ReadFile(path, cwd string) (string, error) {
	path = resolvePath(path, cwd)
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteFile writes content to a file. Creates or overwrites the file.
func WriteFile(path, content, cwd string) (string, error) {
	// Ensure directory exists if path implies one is needed (simple check)
	// But let's stick to simple implementation from main.go
	path = resolvePath(path, cwd)
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return "", err
	}
	return "File written successfully.", nil
}

// RunGitCommand runs a git command (e.g., status, add, commit, push).
func RunGitCommand(args, cwd string) (string, error) {
	parts := strings.Fields(args)
	cmd := exec.Command("git", parts...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git command failed: %s, %w", string(output), err)
	}
	return string(output), nil
}

// RunShellCommand runs an arbitrary shell command.
func RunShellCommand(command, cwd string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %s, %w", string(output), err)
	}
	return string(output), nil
}

// Vibecode analyzes the project structure and key files to 'vibe check' the code.
func Vibecode(path, cwd string) (string, error) {
	path = resolvePath(path, cwd)
	out := "Project Structure:\n"
	files, err := ListFiles(path, "")
	if err != nil {
		return "", err
	}
	out += files + "\n\n"

	readmePath := path + "/README.md"
	content, err := ReadFile(readmePath, "")
	if err == nil {
		out += "README.md Content:\n" + content
	} else {
		readmePath = path + "/README"
		content, err = ReadFile(readmePath, "")
		if err == nil {
			out += "README Content:\n" + content
		}
	}
	return out, nil
}
