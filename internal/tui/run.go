package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// Run 启动交互式界面。initialPath 非空时直接加载该存档。
// 调用方需保证 stdin/stdout 为交互式终端（TTY）。
func Run(initialPath, version string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("tui 需要交互式终端（TTY）运行；非交互场景请使用 decrypt / encrypt / export 子命令")
	}
	m := New(initialPath, version)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetProgram(p)
	_, err := p.Run()
	m.closeStore()
	return err
}
