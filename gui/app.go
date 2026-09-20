package main

import (
	"context"
	"runtime"
)

type App struct {
	ctx      context.Context
	version  string
	stateDir string
}

func NewApp(version, stateDir string) *App {
	return &App{version: version, stateDir: stateDir}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Info is callable from the frontend as window.go.main.App.Info()
func (a *App) Info() map[string]string {
	return map[string]string{
		"version":   a.version,
		"state_dir": a.stateDir,
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
		"go":        runtime.Version(),
	}
}
