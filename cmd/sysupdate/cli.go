package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/term"
)

// Lip Gloss styles for the plain (non-TUI) commands. The default renderer
// detects the terminal, so colours are dropped when output is piped or
// NO_COLOR is set.
var (
	cAccent = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#A99BFF"}
	cOK     = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#5FD787"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#FFC66D"}
	cErr    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#FF7B72"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#7D8590"}

	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(cAccent).Padding(0, 1)
	stStep   = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stCmd    = lipgloss.NewStyle().Foreground(cAccent)
	stOK     = lipgloss.NewStyle().Foreground(cOK)
	stWarn   = lipgloss.NewStyle().Foreground(cWarn)
	stErr    = lipgloss.NewStyle().Foreground(cErr)
	stMuted  = lipgloss.NewStyle().Foreground(cMuted)
	stHeader = lipgloss.NewStyle().Bold(true).Foreground(cAccent).Padding(0, 1)
	stCell   = lipgloss.NewStyle().Padding(0, 1)
)

// isTTY reports whether stdout is a terminal (progress lines are redrawn in place).
func isTTY() bool { return term.IsTerminal(os.Stdout.Fd()) }

// printTable renders rows as a rounded Lip Gloss table. cell may restyle a
// cell (row/col are 0-based data indexes); return nil to keep the default.
func printTable(headers []string, rows [][]string, cell func(row, col int) *lipgloss.Style) {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(stMuted).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return stHeader
			}
			if cell != nil {
				if s := cell(row, col); s != nil {
					return s.Padding(0, 1)
				}
			}
			return stCell
		})
	fmt.Println(t)
}
