package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/logs"
	store "github.com/gmmakoele-ship-it/rafikiclaw/internal/store/sqlite"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Bold(true)
	cardStyle  = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#555555")).Padding(1, 2).Width(36)
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B025"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB800"))
	failStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF3B30"))
	statStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
)

type model struct {
	runs       []store.RunRecord
	logEntries []string
	stats      dashboardStats
	err        error
	quitting   bool
	width      int
	height     int
}

type dashboardStats struct {
	totalRuns  int
	active     int
	succeeded  int
	failed     int
	skillsCount int
	tokensUsed int
}

func initialModel() model {
	m := model{}
	s, err := store.Open(".rafikiclaw")
	if err != nil {
		m.err = fmt.Errorf("open store: %v", err)
		return m
	}
	recs, err := s.ListRuns(50)
	s.Close()
	if err != nil {
		m.err = fmt.Errorf("list runs: %v", err)
		return m
	}
	m.runs = recs

	if len(recs) > 0 {
		events, _ := logs.ReadEvents(".rafikiclaw", recs[0].RunID)
		for i := len(events) - 1; i >= 0 && len(m.logEntries) < 15; i-- {
			if strings.TrimSpace(events[i]) != "" {
				m.logEntries = append(m.logEntries, events[i])
			}
		}
	}

	active, succeeded, failed := 0, 0, 0
	for _, r := range recs {
		switch r.Status {
		case "running", "daemon":
			active++
		case "succeeded":
			succeeded++
		case "failed":
			failed++
		}
	}
	m.stats = dashboardStats{
		totalRuns:   len(recs),
		active:     active,
		succeeded:  succeeded,
		failed:     failed,
		skillsCount: countSkills(),
	}
	return m
}

func countSkills() int {
	entries, err := os.ReadDir("skills")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, func() tea.Msg { return "refresh" }
		}
	case string:
		if msg == "refresh" {
			m = initialModel()
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return dimStyle.Render("RafikiClaw Dashboard — goodbye!\n")
	}
	if m.err != nil {
		return fmt.Sprintf("%s\n\nError: %v\n\nPress q to quit.\n", failStyle.Render("FAILED"), m.err)
	}

	_now := time.Now().Format("2006-01-02 15:04:05 UTC")
	header := titleStyle.Render("RafikiClaw Dashboard") + "  " + dimStyle.Render(_now)

	leftCards := renderStats(m.stats)
	rightCards := renderRunsAndLogs(m.runs, m.logEntries)

	layout := lipgloss.JoinHorizontal(lipgloss.Top, leftCards, rightCards)
	if m.width > 0 && m.width < 90 {
		layout = lipgloss.JoinVertical(lipgloss.Top, leftCards, rightCards)
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s\n", header, layout, dimStyle.Render("r: refresh  q: quit"))
}

func mkCard(title, val, sub string, valStyle lipgloss.Style) string {
	return cardStyle.Render(title + "\n" + valStyle.Render(val) + "\n" + statStyle.Render(sub))
}

func renderStats(s dashboardStats) string {
	col1 := lipgloss.JoinVertical(lipgloss.Top,
		mkCard("Total Runs", fmt.Sprintf("  %d", s.totalRuns), "all-time runs", okStyle),
		mkCard("Active Agents", fmt.Sprintf("  %d", s.active), "running now", warnStyle),
	)
	col2 := lipgloss.JoinVertical(lipgloss.Top,
		mkCard("Succeeded", fmt.Sprintf("  %d", s.succeeded), "completed OK", okStyle),
		mkCard("Failed", fmt.Sprintf("  %d", s.failed), "exited error", failStyle),
	)
	col3 := lipgloss.JoinVertical(lipgloss.Top,
		mkCard("Registered Skills", fmt.Sprintf("  %d", s.skillsCount), ".skill.md files", warnStyle),
		mkCard("LLM Token Use", fmt.Sprintf("  %d", s.tokensUsed), "(session est.)", dimStyle),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, col1, "  ", col2, "  ", col3)
}

func renderRunsAndLogs(runs []store.RunRecord, logEntries []string) string {
	var runLines []string
	for _, r := range runs {
		if len(runLines) >= 8 {
			break
		}
		statusStyle := okStyle
		if r.Status == "failed" {
			statusStyle = failStyle
		} else if r.Status == "running" {
			statusStyle = warnStyle
		}
		runID := r.RunID
		if len(runID) > 14 {
			runID = runID[:14]
		}
		name := r.AgentName
		if name == "" {
			name = "-"
		}
		runLines = append(runLines, fmt.Sprintf("%s  %s  %s",
			statusStyle.Render(fmt.Sprintf("%-12s", r.Status)),
			dimStyle.Render(runID),
			dimStyle.Render(name),
		))
	}
	runsCard := cardStyle.Render(titleStyle.Render("Recent Runs") + "\n" + strings.Join(runLines, "\n"))

	var logLines []string
	for _, l := range logEntries {
		if len(logLines) >= 8 {
			break
		}
		short := l
		if len(short) > 70 {
			short = short[:70] + "..."
		}
		logLines = append(logLines, dimStyle.Render(short))
	}
	logsCard := cardStyle.Render(titleStyle.Render("Recent Events") + "\n" + strings.Join(logLines, "\n"))
	return lipgloss.JoinVertical(lipgloss.Top, runsCard, "\n", logsCard)
}

func main() {
	stateDir := ".rafikiclaw"
	if len(os.Args) > 2 && os.Args[1] == "--state-dir" {
		stateDir = os.Args[2]
	}
	if err := os.Chdir(stateDir); err != nil {
		fmt.Fprintf(os.Stderr, "rafikiclaw dashboard: chdir to %s: %v\n", stateDir, err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		os.Exit(0)
	}()

	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "rafikiclaw dashboard: %v\n", err)
		os.Exit(1)
	}
}