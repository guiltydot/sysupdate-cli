package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	cAccent = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#A99BFF"}
	cOK     = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#5FD787"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#FFC66D"}
	cErr    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#FF7B72"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#7D8590"}
	cSelBg  = lipgloss.AdaptiveColor{Light: "#EAE6FF", Dark: "#2D2A45"}

	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(cAccent).Padding(0, 1)
	sHeader  = lipgloss.NewStyle().Bold(true).Foreground(cMuted)
	sMuted   = lipgloss.NewStyle().Foreground(cMuted)
	sOK      = lipgloss.NewStyle().Foreground(cOK)
	sWarn    = lipgloss.NewStyle().Foreground(cWarn)
	sErr     = lipgloss.NewStyle().Foreground(cErr)
	sAccent  = lipgloss.NewStyle().Foreground(cAccent)
	sBold    = lipgloss.NewStyle().Bold(true)
	sSel     = lipgloss.NewStyle().Background(cSelBg)
	sSection = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sKey     = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMuted)
)

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// pad truncates or right-pads s to exactly w cells.
func pad(s string, w int) string {
	s = trunc(s, w)
	if n := lipgloss.Width(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func dur(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}
