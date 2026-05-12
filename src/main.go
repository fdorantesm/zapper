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
	appBg         = "#111820"
	headerGap     = 6
)

var appVersion = "0.1.0"

const defaultTargets = "node_modules,venv,.venv,__pycache__,dist,build,coverage,.pytest_cache,.mypy_cache,.ruff_cache,.tox,.next,.nuxt,.turbo,.cache,.parcel-cache,target,.gradle,.terraform,.serverless,.DS_Store,Thumbs.db,npm-debug.log,yarn-debug.log,yarn-error.log,pnpm-debug.log"

const cliBanner = `
 _____                              
|__  /__ _ _ __  _ __   ___ _ __   
  / // _` + "`" + ` | '_ \| '_ \ / _ \ '__|  
 / /| (_| | |_) | |_) |  __/ |     
/____\__,_| .__/| .__/ \___|_|     
          |_|   |_|                
`

type directoryInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
}

type tuiModel struct {
	directories   []directoryInfo
	selected      map[int]struct{}
	deleting      map[string]struct{}
	spinner       spinner.Model
	root          string
	targets       map[string]struct{}
	cursor        int
	offset        int
	width         int
	height        int
	elapsed       time.Duration
	savedSize     int64
	dryRun        bool
	busy          bool
	loading       bool
	message       string
	removingPaths map[string]struct{}
	fadeOutRows   map[int]bool
	fadeOutTimer  int
}

type deleteResultMsg struct {
	deleted []directoryInfo
	failed  map[string]error
}

type scanResultMsg struct {
	directories []directoryInfo
	elapsed     time.Duration
	err         error
}

type fadeOutTickMsg struct{}

type fadeOutCompleteMsg struct{}

var (
	appStyle      = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#D7DEE7"))
	headerBgStyle = lipgloss.NewStyle().Background(lipgloss.Color(appBg))

	logoStyle   = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#BFC7D5")).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#69717D"))
	greenStyle  = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#4AF626")).Bold(true)
	yellowStyle = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#E0E300"))
	cyanStyle   = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#7DD3FC"))
	fadeStyle   = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#4A4A5A"))

	headerStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#B7BC00")).Foreground(lipgloss.Color("#10141A"))
	rowStyle      = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#D7DEE7"))
	cursorStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#2538B8")).Foreground(lipgloss.Color("#FFFFFF"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E300")).Bold(true)
	footerStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#202A35")).Foreground(lipgloss.Color("#AAB4C0"))
)

func main() {
	started := time.Now()
	flag.Usage = printUsage

	dirsFlag := flag.String("dirs", defaultTargets, "comma-separated file or directory names to remove")
	rootFlag := flag.String("root", ".", "root directory to scan")
	dryRun := flag.Bool("dry-run", false, "show what would be deleted without removing files")
	yes := flag.Bool("yes", false, "delete all found directories without prompting")
	flag.Parse()

	targets := parseDirectoryNames(*dirsFlag)
	if len(targets) == 0 {
		log.Fatal("no directories specified; provide names with --dirs")
	}

	if *yes && !*dryRun {
		printCLIBanner()
		found, err := findDirectories(*rootFlag, targets)
		if err != nil {
			log.Fatal(err)
		}
		deleteDirectories(found)
		return
	}

	model, err := runTUI(*rootFlag, targets, time.Since(started), *dryRun)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Thank you for using zap, you saved %s\n", formatBytes(model.savedSize))
}

func printUsage() {
	printCLIBanner()
	fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  zap [options]\n\n")
	fmt.Fprintf(flag.CommandLine.Output(), "Examples:\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  zap --dry-run\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  zap --root ~/workspace --dirs node_modules,dist,.next\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  zap --yes --dirs node_modules,venv\n\n")
	fmt.Fprintf(flag.CommandLine.Output(), "Options:\n")
	flag.PrintDefaults()
}

func printCLIBanner() {
	fmt.Fprint(flag.CommandLine.Output(), cliBanner)
	fmt.Fprintln(flag.CommandLine.Output())
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

		if path != absRoot {
			if _, ok := targets[entry.Name()]; ok {
				info, err := directoryStats(path)
				if err != nil {
					log.Printf("Skipping %s: %v", path, err)
					if !entry.IsDir() {
						return nil
					}
					return filepath.SkipDir
				}

				directories = append(directories, info)
				if !entry.IsDir() {
					return nil
				}
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

	size := stat.Size()
	if stat.IsDir() {
		size, err = directorySize(path)
		if err != nil {
			return directoryInfo{}, err
		}
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

func runTUI(root string, targets map[string]struct{}, elapsed time.Duration, dryRun bool) (tuiModel, error) {
	spin := spinner.New()
	spin.Spinner = spinner.Line
	spin.Style = cyanStyle

	model := tuiModel{
		root:     root,
		targets:  targets,
		selected: make(map[int]struct{}),
		deleting: make(map[string]struct{}),
		spinner:  spin,
		width:    100,
		height:   28,
		elapsed:  elapsed,
		dryRun:   dryRun,
		loading:  true,
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
	return tea.Batch(m.spinner.Tick, scanDirectoriesCmd(m.root, m.targets))
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		m.clampOffset()
	}

	if m.loading {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		switch msg := msg.(type) {
		case scanResultMsg:
			m.directories = msg.directories
			m.elapsed = msg.elapsed
			m.loading = false
			if msg.err != nil {
				m.message = fmt.Sprintf("Scan failed: %v", msg.err)
			} else if len(msg.directories) == 0 {
				m.message = "No matching directories found."
			}
			return m, nil
		case tea.KeyMsg:
			if msg.String() == "ctrl+c" || msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
		}

		return m, cmd
	}

	if m.busy {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		switch msg := msg.(type) {
		case deleteResultMsg:
			m.startFadeOut(msg)
			return m, fadeOutTickCmd()
		case tea.KeyMsg:
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.message = "Removing directories. Please wait..."
		}

		return m, cmd
	}

	switch msg := msg.(type) {
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
			if len(m.directories) == 0 {
				break
			}
			m.cursor += m.listHeight()
			if m.cursor >= len(m.directories) {
				m.cursor = len(m.directories) - 1
			}
			m.clampOffset()
		case " ", "space":
			if len(m.directories) == 0 {
				break
			}
			m.toggle(m.cursor)
		case "a":
			if len(m.directories) == 0 {
				break
			}
			if len(m.selected) == len(m.directories) {
				m.selected = make(map[int]struct{})
				break
			}
			for i := range m.directories {
				m.selected[i] = struct{}{}
			}
		case "enter", "d":
			if len(m.directories) == 0 {
				m.message = "No matching directories found."
				break
			}
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
	case fadeOutTickMsg:
		m.fadeOutTimer--
		if m.fadeOutTimer > 0 {
			return m, fadeOutTickCmd()
		}
		return m, func() tea.Msg { return fadeOutCompleteMsg{} }
	case fadeOutCompleteMsg:
		m.completeFadeOut()
	case deleteResultMsg:
		m.startFadeOut(msg)
		return m, fadeOutTickCmd()
	}

	return m, nil
}

func (m tuiModel) View() string {
	content := strings.Builder{}

	if m.loading {
		loadingContent := fmt.Sprintf("%s Scanning directories...", m.spinner.View())
		loadingBox := lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Background(lipgloss.Color(appBg)).
			Foreground(lipgloss.Color("#D7DEE7")).
			Align(lipgloss.Center).
			Render(loadingContent)
		return loadingBox
	}

	content.WriteString(m.logo())
	content.WriteString("\n\n")
	content.WriteString(m.tableHeader())
	content.WriteString("\n")
	content.WriteString(m.rows())
	content.WriteString("\n")
	content.WriteString(m.footer())

	return appStyle.Width(m.width).Height(m.height).Render(content.String())
}

func (m tuiModel) logo() string {
	brand := `░█████████  ░██████   ░████████  ░████████   ░███████  ░██░████ 
     ░███        ░██  ░██    ░██ ░██    ░██ ░██    ░██ ░███     
   ░███     ░███████  ░██    ░██ ░██    ░██ ░█████████ ░██      
 ░███      ░██   ░██  ░███   ░██ ░███   ░██ ░██        ░██      
░█████████  ░█████░██ ░██░█████  ░██░█████   ░███████  ░██      
                      ░██        ░██                            
                      ░██        ░██`

	mode := "LIVE"
	modeStyle := greenStyle
	if m.dryRun {
		mode = "DRY RUN"
		modeStyle = yellowStyle
	}

	modeLine := headerLine("", statLine("mode ", mode, mutedStyle, modeStyle), m.width)
	brandLines := strings.Split(brand, "\n")
	statsLines := m.statsLines()
	lineCount := len(brandLines)
	if len(statsLines)+1 > lineCount {
		lineCount = len(statsLines) + 1
	}

	lines := []string{padRightStyled(modeLine, m.width)}
	for i := 0; i < lineCount; i++ {
		logoPart := ""
		if i < len(brandLines) {
			logoPart = brandLines[i]
		}

		statsPart := ""
		statsIndex := i
		if statsIndex < len(statsLines) {
			statsPart = statsLines[statsIndex]
		}

		lines = append(lines, headerLine(logoPart, statsPart, m.width))
	}

	return "\n" + strings.Join(lines, "\n")
}

func (m tuiModel) statsLines() []string {
	return []string{
		statLine("Releasable space: ", formatHeaderBytes(totalSize(m.directories)), mutedStyle, cyanStyle),
		statLine("Selected space:   ", formatHeaderBytes(m.selectedSize()), mutedStyle, yellowStyle),
		statLine("Space saved:      ", formatHeaderBytes(m.savedSize), mutedStyle, greenStyle),
		statLine("Search completed ", formatElapsed(m.elapsed), greenStyle, mutedStyle),
		statLine("version ", appVersion, mutedStyle, cyanStyle),
	}
}

func statLine(label, value string, labelStyle, valueStyle lipgloss.Style) string {
	return labelStyle.Render(label) + valueStyle.Render(value)
}

func headerLine(left, right string, width int) string {
	if width <= 0 {
		return ""
	}

	rightWidth := lipgloss.Width(right)
	if rightWidth >= width {
		return right
	}

	leftPart := ""
	if left != "" {
		availableLeft := width - rightWidth
		if right != "" {
			availableLeft -= headerGap
		}
		if availableLeft > 0 {
			left = truncateToWidth(left, availableLeft)
			leftPart = logoStyle.Render(padRight(left, availableLeft))
		}
	}

	gap := width - lipgloss.Width(leftPart) - rightWidth
	return leftPart + blankStyled(gap) + right
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
	header := fmt.Sprintf(" %-2s %-3s %-*s %9s %10s ", "", "#", pathWidth, "PATH", "LAST MOD", "SIZE")
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
			" %-2s %-3d %-*s %9s %10s ",
			mark,
			i+1,
			pathWidth,
			truncateMiddle(directory.Path, pathWidth),
			formatAge(directory.ModTime),
			formatBytes(directory.Size),
		)

		style := rowStyle
		if _, ok := m.deleting[directory.Path]; ok {
			line = fmt.Sprintf(
				" %-2s %-3d %-*s %9s %10s ",
				m.spinner.View(),
				i+1,
				pathWidth,
				truncateMiddle(directory.Path, pathWidth),
				"removing",
				formatBytes(directory.Size),
			)
			style = rowStyle.Inherit(cyanStyle)
		}
		fadeActive := m.fadeOutRows != nil && m.fadeOutRows[i]
		if fadeActive {
			dots := ""
			switch m.fadeOutTimer {
			case 3:
				dots = ".  "
			case 2:
				dots = ".. "
			case 1:
				dots = "..."
			}
			line = fmt.Sprintf(
				"%-2s %-3d %-*s %9s %10s ",
				dots,
				i+1,
				pathWidth,
				truncateMiddle(directory.Path, pathWidth),
				"gone",
				formatBytes(directory.Size),
			)
			style = fadeStyle
		}
		if _, ok := m.selected[i]; ok {
			style = style.Inherit(selectedStyle)
		}
		if i == m.cursor && !fadeActive {
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

func (m *tuiModel) startFadeOut(result deleteResultMsg) {
	deletedPaths := pathsFor(result.deleted)
	m.removingPaths = deletedPaths
	m.fadeOutRows = make(map[int]bool)
	m.fadeOutTimer = 3

	for i, directory := range m.directories {
		if _, ok := deletedPaths[directory.Path]; ok {
			m.fadeOutRows[i] = true
		}
	}

	m.selected = make(map[int]struct{})
	m.deleting = make(map[string]struct{})
	m.busy = false
	m.savedSize += totalSize(result.deleted)

	if len(result.failed) > 0 {
		m.message = fmt.Sprintf("Removed %d directories. %d failed.", len(result.deleted), len(result.failed))
	} else {
		m.message = fmt.Sprintf("Removed %d directories. Freed %s.", len(result.deleted), formatBytes(totalSize(result.deleted)))
	}
}

func (m *tuiModel) completeFadeOut() {
	deletedPaths := make(map[string]struct{}, len(m.removingPaths))
	for path := range m.removingPaths {
		deletedPaths[path] = struct{}{}
	}
	m.removingPaths = nil
	m.fadeOutRows = nil

	remaining := make([]directoryInfo, 0, len(m.directories))
	for _, directory := range m.directories {
		if _, ok := deletedPaths[directory.Path]; ok {
			continue
		}
		remaining = append(remaining, directory)
	}

	m.directories = remaining

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

func scanDirectoriesCmd(root string, targets map[string]struct{}) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		directories, err := findDirectories(root, targets)
		return scanResultMsg{
			directories: directories,
			elapsed:     time.Since(started),
			err:         err,
		}
	}
}

func fadeOutTickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return fadeOutTickMsg{}
	})
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
			if value >= 100 || value == float64(int64(value)) {
				return fmt.Sprintf("%.0f %s", value, suffix)
			}
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}

	return fmt.Sprintf("%.1f PB", value/unit)
}

func formatHeaderBytes(bytes int64) string {
	const unit int64 = 1024
	if bytes == 0 {
		return "0.00 GB"
	}
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < unit*unit {
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(unit))
	}
	if bytes < unit*unit*unit {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(unit*unit))
	}

	return fmt.Sprintf("%.2f GB", float64(bytes)/float64(unit*unit*unit))
}

func formatElapsed(elapsed time.Duration) string {
	return fmt.Sprintf("%.2fs", elapsed.Seconds())
}

func truncateToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}

	var result strings.Builder
	used := 0
	for _, char := range value {
		charWidth := lipgloss.Width(string(char))
		if used+charWidth > width {
			break
		}
		result.WriteRune(char)
		used += charWidth
	}

	return result.String()
}

func padRight(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func padRightStyled(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + blankStyled(padding)
}

func blankStyled(width int) string {
	if width <= 0 {
		return ""
	}
	return headerBgStyle.Render(strings.Repeat(" ", width))
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
