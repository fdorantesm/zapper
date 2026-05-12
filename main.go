package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	minListHeight = 4
)

var appVersion = "0.1.0"

type directoryInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
}

type tuiModel struct {
	directories []directoryInfo
	selected    map[int]struct{}
	deleting    map[string]struct{}
	spinner     spinner.Model
	cursor      int
	offset      int
	width       int
	height      int
	elapsed     time.Duration
	savedSize   int64
	dryRun      bool
	busy        bool
	message     string
}

type deleteResultMsg struct {
	deleted []directoryInfo
	failed  map[string]error
}

var (
	appStyle = lipgloss.NewStyle().Background(lipgloss.Color("#111820")).Foreground(lipgloss.Color("#D7DEE7"))

	logoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#BFC7D5")).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#69717D"))
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4AF626")).Bold(true)
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E300"))
	cyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC"))

	headerStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#B7BC00")).Foreground(lipgloss.Color("#10141A"))
	rowStyle      = lipgloss.NewStyle().Background(lipgloss.Color("#111820")).Foreground(lipgloss.Color("#D7DEE7"))
	cursorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#2538B8")).Foreground(lipgloss.Color("#FFFFFF"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E300")).Bold(true)
	footerStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#202A35")).Foreground(lipgloss.Color("#AAB4C0"))
)

func main() {
	started := time.Now()

	dirsFlag := flag.String("dirs", "node_modules,venv,.venv,__pycache__", "comma-separated directory names to remove")
	rootFlag := flag.String("root", ".", "root directory to scan")
	dryRun := flag.Bool("dry-run", false, "show what would be deleted without removing files")
	yes := flag.Bool("yes", false, "delete all found directories without prompting")
	flag.Parse()

	targets := parseDirectoryNames(*dirsFlag)
	if len(targets) == 0 {
		log.Fatal("no directories specified; provide names with --dirs")
	}

	found, err := findDirectories(*rootFlag, targets)
	if err != nil {
		log.Fatal(err)
	}

	if *dryRun || *yes {
		if _, err := runTUI(found, time.Since(started), *dryRun); err != nil {
			log.Fatal(err)
		}
	} else {
		// If not dry-run and not forced delete, proceed with actual deletion
		deleteDirectories(found)
	}
}

func parseDirectoryNames(value string) map[string]struct{} {
	targets := make(map[string]struct{})
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name != "" {
			targets[name] = struct{}{}
		}
	}
	return targets
}

func findDirectories(root string, targets map[string]struct{}) ([]directoryInfo, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root path: %w", err)
	}

	var directories []directoryInfo
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("Skipping %s: %v", path, walkErr)
			return nil
		}

		if !entry.IsDir() {
			return nil
		}

		if path != absRoot {
			if _, ok := targets[entry.Name()]; ok {
				info, err := directoryStats(path)
				if err != nil {
					log.Printf("Skipping %s: %v", path, err)
					return filepath.SkipDir
				}

				directories = append(directories, info)
				return filepath.SkipDir
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan directories: %w", err)
	}

	return directories, nil
}

func directoryStats(path string) (directoryInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return directoryInfo{}, err
	}

	size, err := directorySize(path)
	if err != nil {
		return directoryInfo{}, err
	}

	return directoryInfo{
		Path:    path,
		ModTime: stat.ModTime(),
		Size:    size,
	}, nil
}

func directorySize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		size += info.Size()
		return nil
	})

	return size, err
}

func runTUI(directories []directoryInfo, elapsed time.Duration, dryRun bool) (tuiModel, error) {
	spin := spinner.New()
	spin.Spinner = spinner.Line
	spin.Style = cyanStyle

	model := tuiModel{
		directories: directories,
		selected:    make(map[int]struct{}),
		deleting:    make(map[string]struct{}),
		spinner:     spin,
		width:       100,
		height:      28,
		elapsed:     elapsed,
		dryRun:      dryRun,
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return model, err
	}

	finalModel, ok := result.(tuiModel)
	if !ok {
		return model, nil
	}

	return finalModel, nil
}

func (m tuiModel) Init() tea.Cmd {
	return nil
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.busy {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		switch msg := msg.(type) {
		case deleteResultMsg:
			m.applyDeleteResult(msg)
			return m, nil
		case tea.KeyMsg:
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.message = "Removing directories. Please wait..."
		}

		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampOffset()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.clampOffset()
		case "down", "j":
			if m.cursor < len(m.directories)-1 {
				m.cursor++
			}
			m.clampOffset()
		case "pgup", "b":
			m.cursor -= m.listHeight()
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.clampOffset()
		case "pgdown", "f":
			m.cursor += m.listHeight()
			if m.cursor >= len(m.directories) {
				m.cursor = len(m.directories) - 1
			}
			m.clampOffset()
		case " ":
			m.toggle(m.cursor)
		case "a":
			if len(m.selected) == len(m.directories) {
				m.selected = make(map[int]struct{})
				break
			}
			for i := range m.directories {
				m.selected[i] = struct{}{}
			}
		case "enter", "d":
			if len(m.selected) == 0 {
				m.selected[m.cursor] = struct{}{}
			}
			selected := m.selectedDirectories()
			if len(selected) == 0 {
				m.message = "Nothing selected."
				break
			}
			if m.dryRun {
				m.message = fmt.Sprintf("Dry run: would delete %d directories and free %s.", len(selected), formatBytes(totalSize(selected)))
				break
			}

			m.busy = true
			m.deleting = pathsFor(selected)
			m.message = fmt.Sprintf("Removing %d directories...", len(selected))
			return m, tea.Batch(m.spinner.Tick, deleteSelectedCmd(selected))
		}
	case deleteResultMsg:
		m.applyDeleteResult(msg)
	}

	return m, nil
}

func (m tuiModel) View() string {
	if len(m.directories) == 0 {
		content := strings.Builder{}
		content.WriteString(m.logo())
		content.WriteString("\n")
		content.WriteString(greenStyle.Render("All matching directories have been removed."))
		content.WriteString("\n\n")
		content.WriteString(m.footer())
		return appStyle.Width(m.width).Height(m.height).Render(content.String())
	}

	content := strings.Builder{}
	content.WriteString(m.logo())
	content.WriteString("\n")
	content.WriteString(m.summary())
	content.WriteString("\n\n")
	content.WriteString(m.tableHeader())
	content.WriteString("\n")
	content.WriteString(m.rows())
	content.WriteString("\n")
	content.WriteString(m.footer())

	return appStyle.Width(m.width).Height(m.height).Render(content.String())
}

func (m tuiModel) logo() string {
	brand := "  _ __ ___   ___  _ __  \n | '_ ` _ \\ / _ \\| '_ \\ \n | | | | | | (_) | |_) |\n |_| |_| |_|\\___/| .__/ \n                 |_|    "
	if m.width < 72 {
		brand = "zap"
	}

	mode := greenStyle.Render("LIVE")
	if m.dryRun {
		mode = yellowStyle.Render("DRY RUN")
	}

	modeLine := lipgloss.PlaceHorizontal(m.width, lipgloss.Right, mutedStyle.Render("mode ")+mode)
	versionLine := mutedStyle.Render("version ") + cyanStyle.Render(appVersion)

	return "\n" + modeLine + "\n" + logoStyle.Render(brand) + "\n" + versionLine
}

func (m tuiModel) summary() string {
	releasable := formatBytes(totalSize(m.directories))
	selected := formatBytes(m.selectedSize())
	status := greenStyle.Render("Search completed") + mutedStyle.Render(" "+m.elapsed.Round(time.Millisecond).String())
	if m.busy {
		status = cyanStyle.Render(m.spinner.View() + " Removing directories")
	}

	left := lipgloss.JoinVertical(
		lipgloss.Left,
		mutedStyle.Render("Releasable space: ")+cyanStyle.Render(releasable),
		mutedStyle.Render("Selected space:   ")+yellowStyle.Render(selected),
		mutedStyle.Render("Space saved:      ")+greenStyle.Render(formatBytes(m.savedSize)),
		status,
	)

	count := fmt.Sprintf("%d items", len(m.directories))
	selectedCount := fmt.Sprintf("%d selected", len(m.selected))
	right := lipgloss.JoinVertical(
		lipgloss.Left,
		mutedStyle.Render("Target dirs: ")+cyanStyle.Render(count),
		mutedStyle.Render("Marked:      ")+yellowStyle.Render(selectedCount),
		mutedStyle.Render("Root scan complete"),
	)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 2 {
		gap = 2
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m tuiModel) tableHeader() string {
	pathWidth := m.pathWidth()
	header := fmt.Sprintf(" %-3s %-2s %-*s %9s %10s ", "IDX", "S", pathWidth, "PATH", "LAST MOD", "SIZE")
	return headerStyle.Width(m.width).Render(header)
}

func (m tuiModel) rows() string {
	var rows []string
	visible := m.visibleRange()
	pathWidth := m.pathWidth()

	for i := visible.start; i < visible.end; i++ {
		directory := m.directories[i]
		mark := " "
		if _, ok := m.selected[i]; ok {
			mark = "*"
		}

		line := fmt.Sprintf(
			" %-3d %-2s %-*s %9s %10s ",
			i+1,
			mark,
			pathWidth,
			truncateMiddle(directory.Path, pathWidth),
			formatAge(directory.ModTime),
			formatBytes(directory.Size),
		)

		style := rowStyle
		if _, ok := m.deleting[directory.Path]; ok {
			line = fmt.Sprintf(
				" %-3d %-2s %-*s %9s %10s ",
				i+1,
				m.spinner.View(),
				pathWidth,
				truncateMiddle(directory.Path, pathWidth),
				"removing",
				formatBytes(directory.Size),
			)
			style = rowStyle.Inherit(cyanStyle)
		}
		if _, ok := m.selected[i]; ok {
			style = style.Inherit(selectedStyle)
		}
		if i == m.cursor {
			style = cursorStyle
		}

		rows = append(rows, style.Width(m.width).Render(line))
	}

	for len(rows) < m.listHeight() {
		rows = append(rows, rowStyle.Width(m.width).Render(""))
	}

	return strings.Join(rows, "\n")
}

func (m tuiModel) footer() string {
	message := "up/k down/j move  space select  a all  d/enter delete  q quit"
	if m.dryRun {
		message = "up/k down/j move  space select  a all  d/enter preview delete  q quit"
	}
	if m.busy {
		message = "removing selected directories... ctrl+c aborts the UI"
	}
	if m.message != "" {
		message = m.message
	}

	return footerStyle.Width(m.width).Render(" " + message)
}

func (m tuiModel) listHeight() int {
	available := m.height - 14
	if available < minListHeight {
		return minListHeight
	}
	return available
}

func (m tuiModel) pathWidth() int {
	width := m.width - 34
	if width < 24 {
		return 24
	}
	return width
}

func (m tuiModel) visibleRange() struct{ start, end int } {
	listHeight := m.listHeight()
	start := m.offset
	end := start + listHeight
	if end > len(m.directories) {
		end = len(m.directories)
	}
	return struct{ start, end int }{start: start, end: end}
}

func (m *tuiModel) clampOffset() {
	listHeight := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+listHeight {
		m.offset = m.cursor - listHeight + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *tuiModel) toggle(index int) {
	if _, ok := m.selected[index]; ok {
		delete(m.selected, index)
		return
	}
	m.selected[index] = struct{}{}
}

func (m tuiModel) selectedDirectories() []directoryInfo {
	if len(m.selected) == 0 {
		return nil
	}

	selected := make([]directoryInfo, 0, len(m.selected))
	for i, directory := range m.directories {
		if _, ok := m.selected[i]; ok {
			selected = append(selected, directory)
		}
	}
	return selected
}

func (m tuiModel) selectedSize() int64 {
	return totalSize(m.selectedDirectories())
}

func (m *tuiModel) applyDeleteResult(result deleteResultMsg) {
	deletedPaths := pathsFor(result.deleted)
	remaining := make([]directoryInfo, 0, len(m.directories)-len(deletedPaths))
	for _, directory := range m.directories {
		if _, ok := deletedPaths[directory.Path]; ok {
			continue
		}
		remaining = append(remaining, directory)
	}

	m.directories = remaining
	m.selected = make(map[int]struct{})
	m.deleting = make(map[string]struct{})
	m.busy = false
	m.savedSize += totalSize(result.deleted)

	if len(result.failed) > 0 {
		m.message = fmt.Sprintf("Removed %d directories. %d failed.", len(result.deleted), len(result.failed))
	} else {
		m.message = fmt.Sprintf("Removed %d directories. Freed %s.", len(result.deleted), formatBytes(totalSize(result.deleted)))
	}

	if len(m.directories) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= len(m.directories) {
		m.cursor = len(m.directories) - 1
	}
	m.clampOffset()
}

func deleteSelectedCmd(directories []directoryInfo) tea.Cmd {
	return func() tea.Msg {
		result := deleteResultMsg{failed: make(map[string]error)}
		for _, directory := range directories {
			if err := os.RemoveAll(directory.Path); err != nil {
				result.failed[directory.Path] = err
				continue
			}
			result.deleted = append(result.deleted, directory)
		}
		return result
	}
}

func pathsFor(directories []directoryInfo) map[string]struct{} {
	paths := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		paths[directory.Path] = struct{}{}
	}
	return paths
}

func totalSize(directories []directoryInfo) int64 {
	total := int64(0)
	for _, directory := range directories {
		total += directory.Size
	}
	return total
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	value := float64(bytes)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}

	return fmt.Sprintf("%.1f PB", value/unit)
}

func formatAge(modTime time.Time) string {
	age := time.Since(modTime)
	if age < time.Minute {
		return "now"
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	if age < 30*24*time.Hour {
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
	if age < 365*24*time.Hour {
		return fmt.Sprintf("%dmo", int(age.Hours()/(24*30)))
	}

	return fmt.Sprintf("%dy", int(age.Hours()/(24*365)))
}

func truncateMiddle(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}

	left := (width - 3) / 2
	right := width - 3 - left
	return value[:left] + "..." + value[len(value)-right:]
}

func deleteDirectories(directories []directoryInfo) {
	for _, directory := range directories {
		if err := os.RemoveAll(directory.Path); err != nil {
			log.Printf("Error deleting %s: %v", directory.Path, err)
			continue
		}

		log.Printf("Deleted: %s", directory.Path)
	}
}
