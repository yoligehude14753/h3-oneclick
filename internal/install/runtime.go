package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const comfySourceURL = "https://github.com/comfyanonymous/ComfyUI.git"

func bootstrapComfy(ctx context.Context, root string) error {
	if root == "" {
		return fmt.Errorf("runtime root is empty")
	}
	if info, err := os.Stat(filepath.Join(root, "main.py")); err == nil && !info.IsDir() {
		return nil
	}
	if _, err := os.Stat(root); err == nil {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return readErr
		}
		if len(entries) > 0 {
			return fmt.Errorf("runtime target exists and is not an empty ComfyUI directory: %s", root)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return err
	}
	if err := runCommand(ctx, filepath.Dir(root), "git", "clone", "--depth", "1", comfySourceURL, filepath.Base(root)); err != nil {
		return fmt.Errorf("clone ComfyUI: %w", err)
	}
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python"
	}
	venv := filepath.Join(root, "venv")
	if err := runCommand(ctx, root, python, "-m", "venv", venv); err != nil {
		return fmt.Errorf("create ComfyUI venv: %w", err)
	}
	venvPython := filepath.Join(venv, "bin", "python")
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(venv, "Scripts", "python.exe")
	}
	if _, err := os.Stat(filepath.Join(root, "requirements.txt")); err == nil {
		if err := runCommand(ctx, root, venvPython, "-m", "pip", "install", "-r", "requirements.txt"); err != nil {
			return fmt.Errorf("install ComfyUI requirements: %w", err)
		}
	}
	return nil
}

func runCommand(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, trimOutput(string(output)))
	}
	return nil
}

func trimOutput(value string) string {
	const max = 1600
	if len(value) > max {
		return value[len(value)-max:]
	}
	return value
}
