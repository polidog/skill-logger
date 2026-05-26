package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/polidog/skill-logger/internal/projectname"
	"github.com/polidog/skill-logger/internal/store"
)

type tab int

const (
	tabSkills tab = iota
	tabCommands
	tabProjects
	tabHosts
	tabDaily
	tabRecent
)

var tabNames = []string{"Skills", "Commands", "Projects", "Hosts", "Daily", "Recent"}

type rangePreset struct {
	label string
	since func() time.Time
}

var ranges = []rangePreset{
	{"All", func() time.Time { return time.Time{} }},
	{"7d", func() time.Time { return time.Now().Add(-7 * 24 * time.Hour) }},
	{"24h", func() time.Time { return time.Now().Add(-24 * time.Hour) }},
}

type sourceFilter struct {
	label string
	value store.Source
}

var sources = []sourceFilter{
	{"All", ""},
	{"Claude", store.SourceClaude},
	{"Codex", store.SourceCodex},
}

type Model struct {
	store    *store.Store
	tab      tab
	rangeI   int
	sourceI  int
	hostI    int      // 0 = All, 1..N = hosts[i-1]
	hosts    []string // distinct hosts in DB
	width    int
	height   int
	err      error
	total    int64
	skills   []store.Ranking
	commands []store.Ranking
	projects []store.ProjectStat
	hostStat []store.HostStat
	daily    []store.DailyPoint
	recent   []store.Event

	skillTbl   table.Model
	commandTbl table.Model
	projectTbl table.Model
	hostTbl    table.Model
	recentTbl  table.Model
}

func New(s *store.Store) Model {
	m := Model{store: s}
	m.skillTbl = newRankTable()
	m.commandTbl = newRankTable()
	m.projectTbl = newProjectTable()
	m.hostTbl = newHostTable()
	m.recentTbl = newRecentTable()
	return m
}

func newRankTable() table.Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "#", Width: 4},
			{Title: "Name", Width: 50},
			{Title: "Count", Width: 8},
		}),
		table.WithFocused(true),
	)
	t.SetStyles(tableStyles())
	return t
}

func newHostTable() table.Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "#", Width: 4},
			{Title: "Host", Width: 50},
			{Title: "Count", Width: 8},
		}),
		table.WithFocused(true),
	)
	t.SetStyles(tableStyles())
	return t
}

func newProjectTable() table.Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "#", Width: 4},
			{Title: "Project", Width: 50},
			{Title: "Count", Width: 8},
		}),
		table.WithFocused(true),
	)
	t.SetStyles(tableStyles())
	return t
}

func newRecentTable() table.Model {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "When", Width: 19},
			{Title: "Src", Width: 6},
			{Title: "Kind", Width: 8},
			{Title: "Name", Width: 50},
		}),
		table.WithFocused(true),
	)
	t.SetStyles(tableStyles())
	return t
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	return s
}

type dataMsg struct {
	total    int64
	skills   []store.Ranking
	commands []store.Ranking
	projects []store.ProjectStat
	hostStat []store.HostStat
	hosts    []string
	daily    []store.DailyPoint
	recent   []store.Event
	err      error
}

func (m Model) Init() tea.Cmd { return m.load() }

func (m Model) currentHost() string {
	if m.hostI <= 0 || m.hostI > len(m.hosts) {
		return ""
	}
	return m.hosts[m.hostI-1]
}

func (m Model) load() tea.Cmd {
	s := m.store
	since := ranges[m.rangeI].since()
	src := sources[m.sourceI].value
	host := m.currentHost()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		base := func(k store.Kind, limit int) store.Filter {
			return store.Filter{Source: src, Kind: k, Host: host, Since: since, Limit: limit}
		}
		var msg dataMsg
		var err error
		if msg.total, err = s.Total(ctx, base("", 0)); err != nil {
			msg.err = err
			return msg
		}
		if msg.skills, err = s.Ranking(ctx, base("skill", 100)); err != nil {
			msg.err = err
			return msg
		}
		if msg.commands, err = s.Ranking(ctx, base("command", 100)); err != nil {
			msg.err = err
			return msg
		}
		if msg.projects, err = s.ProjectRanking(ctx, base("", 100)); err != nil {
			msg.err = err
			return msg
		}
		if msg.hostStat, err = s.HostRanking(ctx, base("", 100)); err != nil {
			msg.err = err
			return msg
		}
		if msg.hosts, err = s.DistinctHosts(ctx); err != nil {
			msg.err = err
			return msg
		}
		if msg.daily, err = s.Daily(ctx, base("", 0)); err != nil {
			msg.err = err
			return msg
		}
		if msg.recent, err = s.Recent(ctx, base("", 200)); err != nil {
			msg.err = err
			return msg
		}
		return msg
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTables()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "l", "right":
			m.tab = tab((int(m.tab) + 1) % len(tabNames))
			return m, nil
		case "shift+tab", "h", "left":
			m.tab = tab((int(m.tab) - 1 + len(tabNames)) % len(tabNames))
			return m, nil
		case "1":
			m.tab = tabSkills
			return m, nil
		case "2":
			m.tab = tabCommands
			return m, nil
		case "3":
			m.tab = tabProjects
			return m, nil
		case "4":
			m.tab = tabHosts
			return m, nil
		case "5":
			m.tab = tabDaily
			return m, nil
		case "6":
			m.tab = tabRecent
			return m, nil
		case "r":
			return m, m.load()
		case "f":
			m.rangeI = (m.rangeI + 1) % len(ranges)
			return m, m.load()
		case "s":
			m.sourceI = (m.sourceI + 1) % len(sources)
			return m, m.load()
		case "m":
			if n := len(m.hosts); n > 0 {
				m.hostI = (m.hostI + 1) % (n + 1)
				return m, m.load()
			}
			return m, nil
		}
	case dataMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.total = msg.total
		m.skills = msg.skills
		m.commands = msg.commands
		m.projects = msg.projects
		m.hostStat = msg.hostStat
		m.daily = msg.daily
		m.recent = msg.recent
		m.hosts = msg.hosts
		if m.hostI > len(m.hosts) {
			m.hostI = 0
		}
		m.skillTbl.SetRows(rankRows(m.skills))
		m.commandTbl.SetRows(rankRows(m.commands))
		m.projectTbl.SetRows(projectRows(m.projects))
		m.hostTbl.SetRows(hostRows(m.hostStat))
		m.recentTbl.SetRows(recentRows(m.recent))
		return m, nil
	}
	var cmd tea.Cmd
	switch m.tab {
	case tabSkills:
		m.skillTbl, cmd = m.skillTbl.Update(msg)
	case tabCommands:
		m.commandTbl, cmd = m.commandTbl.Update(msg)
	case tabProjects:
		m.projectTbl, cmd = m.projectTbl.Update(msg)
	case tabHosts:
		m.hostTbl, cmd = m.hostTbl.Update(msg)
	case tabRecent:
		m.recentTbl, cmd = m.recentTbl.Update(msg)
	}
	return m, cmd
}

func (m *Model) resizeTables() {
	if m.width == 0 {
		return
	}
	bodyW := m.width - 4
	if bodyW < 30 {
		bodyW = 30
	}
	rankNameW := bodyW - (4 + 8 + 6)
	if rankNameW < 10 {
		rankNameW = 10
	}
	m.skillTbl.SetColumns([]table.Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: rankNameW},
		{Title: "Count", Width: 8},
	})
	m.commandTbl.SetColumns([]table.Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: rankNameW},
		{Title: "Count", Width: 8},
	})
	m.projectTbl.SetColumns([]table.Column{
		{Title: "#", Width: 4},
		{Title: "Project", Width: rankNameW},
		{Title: "Count", Width: 8},
	})
	m.hostTbl.SetColumns([]table.Column{
		{Title: "#", Width: 4},
		{Title: "Host", Width: rankNameW},
		{Title: "Count", Width: 8},
	})
	recentNameW := bodyW - (19 + 6 + 8 + 8)
	if recentNameW < 10 {
		recentNameW = 10
	}
	m.recentTbl.SetColumns([]table.Column{
		{Title: "When", Width: 19},
		{Title: "Src", Width: 6},
		{Title: "Kind", Width: 8},
		{Title: "Name", Width: recentNameW},
	})
	h := m.height - 6
	if h < 5 {
		h = 5
	}
	m.skillTbl.SetHeight(h)
	m.commandTbl.SetHeight(h)
	m.projectTbl.SetHeight(h)
	m.hostTbl.SetHeight(h)
	m.recentTbl.SetHeight(h)
}

func rankRows(rs []store.Ranking) []table.Row {
	rows := make([]table.Row, len(rs))
	for i, r := range rs {
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), r.Name, fmt.Sprintf("%d", r.Count)}
	}
	return rows
}

func projectRows(ps []store.ProjectStat) []table.Row {
	folded := projectname.Fold(ps,
		func(p store.ProjectStat) string { return p.Cwd },
		func(p store.ProjectStat) int64 { return p.Count })
	rows := make([]table.Row, len(folded))
	for i, p := range folded {
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), p.Display, fmt.Sprintf("%d", p.Count)}
	}
	return rows
}

func hostRows(hs []store.HostStat) []table.Row {
	rows := make([]table.Row, len(hs))
	for i, h := range hs {
		name := h.Host
		if name == "" {
			name = "(unknown)"
		}
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), name, fmt.Sprintf("%d", h.Count)}
	}
	return rows
}

func recentRows(es []store.Event) []table.Row {
	rows := make([]table.Row, len(es))
	for i, e := range es {
		rows[i] = table.Row{
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			string(e.Source),
			string(e.Kind),
			e.Name,
		}
	}
	return rows
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	tabActive     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 2)
	tabInactive   = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 2)
	chipStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("236")).Padding(0, 1)
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
	dailyBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
)

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("skill-logger"))
	b.WriteString("  ")
	b.WriteString(chipStyle.Render("range: " + ranges[m.rangeI].label))
	b.WriteString(" ")
	b.WriteString(chipStyle.Render("source: " + sources[m.sourceI].label))
	b.WriteString(" ")
	hostLabel := "All"
	if h := m.currentHost(); h != "" {
		hostLabel = h
	} else if m.hostI > 0 {
		hostLabel = "(unknown)"
	}
	b.WriteString(chipStyle.Render("host: " + hostLabel))
	b.WriteString(" ")
	b.WriteString(chipStyle.Render(fmt.Sprintf("total: %d", m.total)))
	b.WriteString("\n")

	var tabs []string
	for i, name := range tabNames {
		s := tabInactive
		if tab(i) == m.tab {
			s = tabActive
		}
		tabs = append(tabs, s.Render(fmt.Sprintf("%d %s", i+1, name)))
	}
	b.WriteString(strings.Join(tabs, " "))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render("error: " + m.err.Error()))
		b.WriteString("\n")
	} else {
		switch m.tab {
		case tabSkills:
			if len(m.skills) == 0 {
				b.WriteString(subtleStyle.Render("no skill events yet — see README for hook setup"))
			} else {
				b.WriteString(m.skillTbl.View())
			}
		case tabCommands:
			if len(m.commands) == 0 {
				b.WriteString(subtleStyle.Render("no slash-command events yet — see README for hook setup"))
			} else {
				b.WriteString(m.commandTbl.View())
			}
		case tabProjects:
			if len(m.projects) == 0 {
				b.WriteString(subtleStyle.Render("no events yet — see README for hook setup"))
			} else {
				b.WriteString(m.projectTbl.View())
			}
		case tabHosts:
			if len(m.hostStat) == 0 {
				b.WriteString(subtleStyle.Render("no events yet — see README for hook setup"))
			} else {
				b.WriteString(m.hostTbl.View())
			}
		case tabDaily:
			b.WriteString(renderDaily(m.daily, m.width))
		case tabRecent:
			if len(m.recent) == 0 {
				b.WriteString(subtleStyle.Render("no events yet"))
			} else {
				b.WriteString(m.recentTbl.View())
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(footerStyle.Render("tab/← → switch · 1-6 jump · r refresh · f range · s source · m host · q quit"))
	return b.String()
}

func renderDaily(points []store.DailyPoint, width int) string {
	if len(points) == 0 {
		return subtleStyle.Render("no events yet")
	}
	var max int64
	for _, p := range points {
		if p.Count > max {
			max = p.Count
		}
	}
	if max == 0 {
		max = 1
	}
	barWidth := width - 25
	if barWidth < 10 {
		barWidth = 10
	}
	var b strings.Builder
	for _, p := range points {
		filled := int(float64(p.Count) / float64(max) * float64(barWidth))
		if filled < 1 && p.Count > 0 {
			filled = 1
		}
		bar := dailyBarStyle.Render(strings.Repeat("█", filled))
		fmt.Fprintf(&b, "%s  %s %s\n", p.Day, bar, subtleStyle.Render(fmt.Sprintf("%d", p.Count)))
	}
	return b.String()
}
