package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileOperationsE2E performs real file system operations
// to verify the tool logic works against the actual OS.
func TestFileOperationsE2E(t *testing.T) {
	// 1. Setup a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "nanoclaw-e2e")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 2. Test WriteFile
	testFileName := "hello.txt"
	testContent := "Hello, E2E!"
	testFilePath := filepath.Join(tempDir, testFileName)

	out, err := WriteFile(testFilePath, testContent, "")
	if err != nil {
		t.Errorf("WriteFile failed: %v", err)
	}
	if out != "File written successfully." {
		t.Errorf("Unexpected output from WriteFile: %s", out)
	}

	// 3. Test ReadFile
	content, err := ReadFile(testFilePath, "")
	if err != nil {
		t.Errorf("ReadFile failed: %v", err)
	}
	if content != testContent {
		t.Errorf("Expected content %q, got %q", testContent, content)
	}

	// 4. Test ListFiles
	listOut, err := ListFiles(tempDir, "")
	if err != nil {
		t.Errorf("ListFiles failed: %v", err)
	}
	if !strings.Contains(listOut, testFileName) {
		t.Errorf("ListFiles output %q does not contain %q", listOut, testFileName)
	}
}

// TestVibecodeE2E prepares a project structure and runs vibecode on it
func TestVibecodeE2E(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nanoclaw-vibe-e2e")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create README.md
	readmeContent := "# Test Project\nThis is a vibe check."
	readmePath := filepath.Join(tempDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	// Create a source file
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to write main.go: %v", err)
	}

	// Run Vibecode
	output, err := Vibecode(tempDir, "")
	if err != nil {
		t.Errorf("Vibecode failed: %v", err)
	}

	if !strings.Contains(output, "Project Structure:") {
		t.Error("Vibecode output missing structure header")
	}
	if !strings.Contains(output, "README.md Content:") {
		t.Error("Vibecode output missing readme header")
	}
	if !strings.Contains(output, "This is a vibe check.") {
		t.Error("Vibecode output missing readme content")
	}
	if !strings.Contains(output, "main.go") {
		t.Error("Vibecode output missing file list")
	}
}

// TestShellCommandE2E tests executing a simple shell command
func TestShellCommandE2E(t *testing.T) {
	out, err := RunShellCommand("echo 'hello shell'", "")
	if err != nil {
		t.Fatalf("RunShellCommand failed: %v", err)
	}
	if strings.TrimSpace(out) != "hello shell" {
		t.Errorf("Expected 'hello shell', got %q", out)
	}
}
