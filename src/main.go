package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
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
	filter        string
	filtering     bool
	filterErr     error
	sortBySize    bool
	browsing      bool
	navFilter     string
	navFiltering  bool
	navCursor     int
	navOffset     int
	navFiles      []os.DirEntry
	currentPath   string
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

type directoryDeletedMsg struct {
	info      directoryInfo
	err       error
	remaining []directoryInfo
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

	headerStyle       = lipgloss.NewStyle().Background(lipgloss.Color("#B7BC00")).Foreground(lipgloss.Color("#10141A"))
	rowStyle          = lipgloss.NewStyle().Background(lipgloss.Color(appBg)).Foreground(lipgloss.Color("#D7DEE7"))
	cursorStyle       = lipgloss.NewStyle().Background(lipgloss.Color("#2538B8")).Foreground(lipgloss.Color("#FFFFFF"))
	selectedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0E300")).Bold(true)
	footerStyle       = lipgloss.NewStyle().Background(lipgloss.Color("#202A35")).Foreground(lipgloss.Color("#AAB4C0"))
	shortcutKeyStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#202A35")).Foreground(lipgloss.Color("#7DD3FC")).Bold(true)
	shortcutTextStyle = lipgloss.NewStyle().Background(lipgloss.Color("#202A35")).Foreground(lipgloss.Color("#AAB4C0"))
)

func main() {
	started := time.Now()
	flag.Usage = printUsage

	dirsFlag := flag.String("dirs", defaultTargets, "comma-separated file or directory names to remove")
	rootFlag := flag.String("root", ".", "root directory to scan")
	dryRun := flag.Bool("dry-run", false, "show what would be deleted without removing files")
	yes := flag.Bool("yes", false, "delete all found directories without prompting")
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *version {
		fmt.Println("zap version", appVersion)
		return
	}

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
	fmt.Fprintf(flag.CommandLine.Output(), "  zap --yes --dirs node_modules,venv\n")
	fmt.Fprintf(flag.CommandLine.Output(), "  zap --version\n\n")
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

func isPermissionError(err error) bool {
	err = underlyingError(err)
	return err == syscall.EACCES || err == syscall.EPERM
}

func underlyingError(err error) error {
	for {
		pe := err.(*os.PathError)
		if pe.Err == nil {
			return pe
		}
		err = pe.Err
	}
}

func findDirectories(root string, targets map[string]struct{}) ([]directoryInfo, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root path: %w", err)
	}

	var directories []directoryInfo
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if isPermissionError(walkErr) {
				return nil
			}
			log.Printf("Skipping %s: %v", path, walkErr)
			return nil
		}

		if path != absRoot {
			if _, ok := targets[entry.Name()]; ok {
				info, err := directoryStats(path)
				if err != nil {
					if isPermissionError(err) {
						return nil
					}
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

	absRoot, _ := filepath.Abs(root)
	navFiles, _ := listNavFiles(absRoot)

	model := tuiModel{
		root:        root,
		targets:     targets,
		selected:    make(map[int]struct{}),
		deleting:    make(map[string]struct{}),
		spinner:     spin,
		currentPath: absRoot,
		navFiles:    navFiles,
		width:       100,
		height:      28,
		elapsed:     elapsed,
		dryRun:      dryRun,
		loading:     true,
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
		m.clampNavOffset()
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
		case directoryDeletedMsg:
			if msg.err == nil {
				m.savedSize += msg.info.Size
			}
			if len(msg.remaining) == 0 {
				m.busy = false
				m.startFadeOut(deleteResultMsg{deleted: m.selectedDirectories()})
				return m, fadeOutTickCmd()
			}
			return m, deleteNextCmd(msg.remaining)
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
		if m.browsing {
			if m.navFiltering {
				switch msg.String() {
				case "ctrl+c":
					return m, tea.Quit
				case "enter", "esc":
					m.navFiltering = false
				case "backspace", "ctrl+h":
					if len(m.navFilter) > 0 {
						m.navFilter = m.navFilter[:len(m.navFilter)-1]
						m.navCursor = 0
						m.navOffset = 0
					}
				case "ctrl+u":
					m.navFilter = ""
					m.navCursor = 0
					m.navOffset = 0
				default:
					if len(msg.Runes) > 0 {
						m.navFilter += string(msg.Runes)
						m.navCursor = 0
						m.navOffset = 0
					}
				}
				m.clampNavOffset()
				return m, nil
			}

			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "c":
				m.browsing = false
			case "/":
				m.navFiltering = true
			case "up", "k":
				if m.navCursor > 0 {
					m.navCursor--
				}
				m.clampNavOffset()
			case "down", "j":
				if m.navCursor < m.filteredNavCount()-1 {
					m.navCursor++
				}
				m.clampNavOffset()
			case "enter":
				if m.navFilter != "" {
					target := m.navFilter
					if !filepath.IsAbs(target) {
						target = filepath.Join(m.currentPath, target)
					}
					if info, err := os.Stat(target); err == nil && info.IsDir() {
						m.currentPath = target
						m.navFiles, _ = listNavFiles(target)
						m.navCursor = 0
						m.navOffset = 0
						m.navFilter = ""
						m.navFiltering = false
						return m, nil
					}
				}

				files := m.filteredNavFiles()
				if len(files) == 0 {
					break
				}
				entry := files[m.navCursor]
				var newPath string
				if entry.Name() == ".." {
					newPath = filepath.Dir(m.currentPath)
				} else if entry.IsDir() {
					newPath = filepath.Join(m.currentPath, entry.Name())
				} else {
					break
				}

				m.currentPath = newPath
				m.navFiles, _ = listNavFiles(newPath)
				m.navCursor = 0
				m.navOffset = 0
				m.navFilter = ""
				m.navFiltering = false
			case " ":
				m.root = m.currentPath
				m.browsing = false
				m.loading = true
				m.cursor = 0
				m.offset = 0
				m.selected = make(map[int]struct{})
				return m, scanDirectoriesCmd(m.root, m.targets)
			}
			return m, nil
		}

		if m.filtering {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "enter", "esc":
				m.filtering = false
			case "backspace", "ctrl+h":
				if len(m.filter) > 0 {
					m.updateFilter(m.filter[:len(m.filter)-1])
					m.cursor = 0
					m.offset = 0
				}
			case "ctrl+u":
				m.updateFilter("")
				m.cursor = 0
				m.offset = 0
			default:
				if len(msg.Runes) > 0 {
					m.updateFilter(m.filter + string(msg.Runes))
					m.cursor = 0
					m.offset = 0
				}
			}
			m.clampOffset()
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case ":":
			m.filtering = !m.filtering
		case "c":
			m.browsing = true
			m.navFiles, _ = listNavFiles(m.currentPath)
			m.navCursor = 0
			m.navOffset = 0
		case "s":
			m.sortBySize = !m.sortBySize
			m.cursor = 0
			m.offset = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.clampOffset()
		case "down", "j":
			if m.cursor < m.filteredCount()-1 {
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
			if m.filteredCount() == 0 {
				break
			}
			m.cursor += m.listHeight()
			if m.cursor >= m.filteredCount() {
				m.cursor = m.filteredCount() - 1
			}
			m.clampOffset()
		case " ", "space":
			visible := m.filteredDirectories()
			if len(visible) == 0 {
				break
			}
			m.toggle(visible[m.cursor].index)
		case "a":
			visible := m.filteredDirectories()
			if len(visible) == 0 {
				break
			}
			if m.selectedVisibleCount(visible) == len(visible) {
				for _, directory := range visible {
					delete(m.selected, directory.index)
				}
				break
			}
			for _, directory := range visible {
				m.selected[directory.index] = struct{}{}
			}
		case "enter", "d":
			visible := m.filteredDirectories()
			if len(visible) == 0 {
				m.message = "No matching directories found."
				break
			}
			if len(m.selected) == 0 {
				m.selected[visible[m.cursor].index] = struct{}{}
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
			return m, tea.Batch(m.spinner.Tick, deleteNextCmd(selected))
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
	if m.loading {
		loadingContent := fmt.Sprintf("%s Scanning directories...", m.spinner.View())
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Background(lipgloss.Color(appBg)).
			Foreground(lipgloss.Color("#D7DEE7")).
			Align(lipgloss.Center, lipgloss.Center).
			Render(loadingContent)
	}

	// Always render the main TUI background
	var content strings.Builder
	content.WriteString(m.logo())
	if m.filtering {
		content.WriteString("\n")
		content.WriteString(m.filterLine())
	}
	content.WriteString("\n\n")
	content.WriteString(m.tableHeader())
	content.WriteString("\n")
	content.WriteString(m.rows())
	content.WriteString("\n")
	content.WriteString(m.footer())

	bg := appStyle.Width(m.width).Height(m.height).Render(content.String())

	if m.browsing {
		modalWidth := m.width - 10
		if modalWidth > 100 {
			modalWidth = 100
		}
		modalHeight := m.height - 6
		if modalHeight > 36 {
			modalHeight = 36
		}

		modalStyle := lipgloss.NewStyle().
			Width(modalWidth).
			Height(modalHeight).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7DD3FC")).
			Padding(1, 2).
			Background(lipgloss.Color(appBg))

		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7DD3FC")).Render(" NAVIGATE ")
		pathLine := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAB4C0")).Render(" Path: " + truncateMiddle(m.currentPath, modalWidth-12))

		filterLabel := " / Filter/Jump: "
		filterValue := m.navFilter
		if filterValue == "" && !m.navFiltering {
			filterValue = "press / to filter or type path"
		}
		if m.navFiltering {
			filterValue += "_"
		}
		filterLine := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC")).Render(filterLabel + filterValue)

		header := lipgloss.JoinVertical(lipgloss.Left, title, pathLine, filterLine, "")

		var navRows []string
		files := m.filteredNavFiles()
		listSpace := modalHeight - 10
		visibleFiles := m.navVisibleRange(listSpace)

		for i := visibleFiles.start; i < visibleFiles.end; i++ {
			file := files[i]
			style := rowStyle
			if i == m.navCursor {
				style = cursorStyle
			}
			name := file.Name()
			if file.IsDir() && name != ".." {
				name += "/"
			}
			navRows = append(navRows, style.Width(modalWidth-6).Render(fmt.Sprintf("  %s", name)))
		}

		for len(navRows) < listSpace {
			navRows = append(navRows, "")
		}

		footer := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#69717D")).
			PaddingTop(1).
			Render(shortcuts(
				shortcut("up/down", "Move"),
				shortcut("enter", "Enter/Jump"),
				shortcut("space", "Select Root"),
				shortcut("esc", "Close"),
			))

		modalContent := lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(navRows, "\n"), footer)
		modal := modalStyle.Render(modalContent)

		// Create the overlay effect
		return overlay(bg, modal, m.width, m.height)
	}

	return bg
}

func overlay(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	fgWidth := lipgloss.Width(fgLines[0])
	fgHeight := len(fgLines)

	startY := (height - fgHeight) / 2
	startX := (width - fgWidth) / 2

	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	for i := 0; i < fgHeight; i++ {
		y := startY + i
		if y >= 0 && y < len(bgLines) {
			bgLine := bgLines[y]
			fgLine := fgLines[i]

			// Re-compose line: [bg left] [fg line] [bg right]
			// We use truncate to safely cut ANSI strings
			leftPart := truncate.String(bgLine, uint(startX))
			// To get the right part, we need to know the total visual width.
			// Since bgLine is already padded to 'width', we cut from startX + fgWidth.
			rightPart := truncateLeft(bgLine, startX+fgWidth)

			bgLines[y] = leftPart + fgLine + rightPart
		}
	}

	return strings.Join(bgLines, "\n")
}

// truncateLeft removes 'w' visual cells from the beginning of string 's'.
func truncateLeft(s string, w int) string {
	if w <= 0 {
		return s
	}
	// We use a trick: truncate the string to its full width (no-op but cleans up),
	// then we use a simple character loop that skips visual cells.
	// But lipgloss/truncate doesn't offer "cut from left".
	// We'll use a manual implementation that respects ANSI.

	var (
		currentW int
		inAnsi   bool
		startIdx int
	)

	for i, r := range s {
		if currentW >= w {
			startIdx = i
			break
		}
		if r == '\x1b' {
			inAnsi = true
			continue
		}
		if inAnsi {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == 'm' {
				inAnsi = false
			}
			continue
		}
		// Basic visual width
		currentW++
	}

	if currentW < w {
		return ""
	}

	return s[startIdx:]
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
	visible := m.filteredDirectories()
	directories := make([]directoryInfo, 0, len(visible))
	for _, directory := range visible {
		directories = append(directories, directory.info)
	}

	return []string{
		statLine("Releasable space: ", formatHeaderBytes(totalSize(directories)), mutedStyle, cyanStyle),
		statLine("Selected space:   ", formatHeaderBytes(m.selectedSize()), mutedStyle, yellowStyle),
		statLine("Space saved:      ", formatHeaderBytes(m.savedSize), mutedStyle, greenStyle),
		statLine("Search completed ", formatElapsed(m.elapsed), greenStyle, mutedStyle),
		statLine("version ", appVersion, mutedStyle, cyanStyle),
	}
}

func (m tuiModel) filterLine() string {
	cursor := ""
	if m.filtering {
		cursor = "_"
	}
	value := m.filter
	if value == "" {
		value = "node_modules|dist  or  */.next  or  ^/Users"
		return mutedStyle.Width(m.width).Render(value + cursor)
	}

	style := cyanStyle
	if m.filterErr != nil {
		style = selectedStyle // Use yellow/bold for error
		return style.Width(m.width).Render(value + cursor + " (invalid regex)")
	}

	return style.Width(m.width).Render(value + cursor)
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
	sizeLabel := "SIZE"
	if m.sortBySize {
		sizeLabel = "SIZE ↓"
	}
	header := fmt.Sprintf(" %-2s %-3s %-*s %9s %10s ", "", "#", pathWidth, "PATH", "LAST MOD", sizeLabel)
	return headerStyle.Width(m.width).Render(header)
}

func (m tuiModel) rows() string {
	var rows []string
	filtered := m.filteredDirectories()
	visible := m.visibleRange()
	pathWidth := m.pathWidth()

	for i := visible.start; i < visible.end; i++ {
		filteredDirectory := filtered[i]
		directory := filteredDirectory.info
		index := filteredDirectory.index
		mark := " "
		if _, ok := m.selected[index]; ok {
			mark = "*"
		}

		line := fmt.Sprintf(
			" %-2s %-3d %-*s %9s %10s ",
			mark,
			index+1,
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
				index+1,
				pathWidth,
				truncateMiddle(directory.Path, pathWidth),
				"removing",
				formatBytes(directory.Size),
			)
			style = rowStyle.Inherit(cyanStyle)
		}
		fadeActive := m.fadeOutRows != nil && m.fadeOutRows[index]
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
				index+1,
				pathWidth,
				truncateMiddle(directory.Path, pathWidth),
				"gone",
				formatBytes(directory.Size),
			)
			style = fadeStyle
		}
		if _, ok := m.selected[index]; ok {
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
	if m.browsing {
		return footerStyle.Width(m.width).Render(" " + shortcuts(
			shortcut("up/down", "Move"),
			shortcut("enter", "Enter/Jump"),
			shortcut("space", "Select Root"),
			shortcut("esc", "Close"),
			shortcut("ctrl-c", "Quit"),
		))
	}
	if m.filtering {
		return footerStyle.Width(m.width).Render(" " + shortcuts(
			shortcut("esc", "Close"),
			shortcut("ctrl-u", "Clear"),
			shortcut("enter", "Apply"),
			shortcut("ctrl-c", "Quit"),
		))
	}
	if m.busy {
		return footerStyle.Width(m.width).Render(" " + shortcuts(
			shortcut("ctrl-c", "Abort UI"),
			shortcut(m.spinner.View(), "Removing..."),
		))
	}
	if m.message != "" {
		return footerStyle.Width(m.width).Render(" " + m.message)
	}

	deleteLabel := "Delete"
	if m.dryRun {
		deleteLabel = "Preview Delete"
	}

	return footerStyle.Width(m.width).Render(" " + shortcuts(
		shortcut("j/k", "Move"),
		shortcut("c", "Change Dir"),
		shortcut(":", "Filter"),
		shortcut("s", "Sort Size"),
		shortcut("space", "Select"),
		shortcut("a", "All"),
		shortcut("d", deleteLabel),
		shortcut("q", "Quit"),
	))
}

func shortcut(key, action string) string {
	return shortcutKeyStyle.Render("<"+key+">") + " " + shortcutTextStyle.Render(action)
}

func shortcuts(items ...string) string {
	return strings.Join(items, "   ")
}

func (m tuiModel) listHeight() int {
	available := m.height - 14
	if m.filtering {
		available--
	}
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
	count := m.filteredCount()
	if end > count {
		end = count
	}
	return struct{ start, end int }{start: start, end: end}
}

func (m *tuiModel) clampOffset() {
	listHeight := m.listHeight()
	count := m.filteredCount()
	if count == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= count {
		m.cursor = count - 1
	}
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

func (m tuiModel) filteredNavFiles() []os.DirEntry {
	if m.navFilter == "" {
		return m.navFiles
	}

	var filtered []os.DirEntry
	for _, f := range m.navFiles {
		if f.Name() == ".." {
			filtered = append(filtered, f)
			continue
		}
		if strings.Contains(strings.ToLower(f.Name()), strings.ToLower(m.navFilter)) {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func (m tuiModel) filteredNavCount() int {
	return len(m.filteredNavFiles())
}

func (m tuiModel) navVisibleRange(height int) struct{ start, end int } {
	files := m.filteredNavFiles()
	start := m.navOffset
	end := start + height
	if end > len(files) {
		end = len(files)
	}
	return struct{ start, end int }{start: start, end: end}
}

func (m *tuiModel) clampNavOffset() {
	modalHeight := m.height - 6
	if modalHeight > 36 {
		modalHeight = 36
	}
	listHeight := modalHeight - 10
	count := m.filteredNavCount()

	if count == 0 {
		m.navCursor = 0
		m.navOffset = 0
		return
	}
	if m.navCursor >= count {
		m.navCursor = count - 1
	}
	if m.navCursor < m.navOffset {
		m.navOffset = m.navCursor
	}
	if m.navCursor >= m.navOffset+listHeight {
		m.navOffset = m.navCursor - listHeight + 1
	}
	if m.navOffset < 0 {
		m.navOffset = 0
	}
}

func (m *tuiModel) toggle(index int) {
	if _, ok := m.selected[index]; ok {
		delete(m.selected, index)
		return
	}
	m.selected[index] = struct{}{}
}

type indexedDirectory struct {
	index int
	info  directoryInfo
}

func (m *tuiModel) updateFilter(val string) {
	m.filter = val
	if val == "" {
		m.filterErr = nil
		return
	}
	// Support simple glob-like * by converting it to .* if it's not valid regex
	_, m.filterErr = regexp.Compile(val)
	if m.filterErr != nil && strings.Contains(val, "*") {
		alt := strings.ReplaceAll(val, "*", ".*")
		if _, err := regexp.Compile(alt); err == nil {
			m.filterErr = nil
		}
	}
}

func (m tuiModel) filteredDirectories() []indexedDirectory {
	var re *regexp.Regexp
	if m.filter != "" {
		var err error
		re, err = regexp.Compile(m.filter)
		if err != nil && strings.Contains(m.filter, "*") {
			re, _ = regexp.Compile(strings.ReplaceAll(m.filter, "*", ".*"))
		}
	}

	directories := make([]indexedDirectory, 0, len(m.directories))
	for i, directory := range m.directories {
		if re == nil || re.MatchString(directory.Path) {
			directories = append(directories, indexedDirectory{index: i, info: directory})
		}
	}

	if m.sortBySize {
		sort.SliceStable(directories, func(i, j int) bool {
			return directories[i].info.Size > directories[j].info.Size
		})
	}
	return directories
}

func (m tuiModel) filteredCount() int {
	return len(m.filteredDirectories())
}

func (m tuiModel) selectedVisibleCount(visible []indexedDirectory) int {
	count := 0
	for _, directory := range visible {
		if _, ok := m.selected[directory.index]; ok {
			count++
		}
	}
	return count
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

func deleteNextCmd(remaining []directoryInfo) tea.Cmd {
	if len(remaining) == 0 {
		return nil
	}
	return func() tea.Msg {
		d := remaining[0]
		err := os.RemoveAll(d.Path)
		return directoryDeletedMsg{
			info:      d,
			err:       err,
			remaining: remaining[1:],
		}
	}
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

type dotEntry string

func (d dotEntry) Name() string               { return string(d) }
func (d dotEntry) IsDir() bool                { return true }
func (d dotEntry) Type() fs.FileMode          { return fs.ModeDir }
func (d dotEntry) Info() (fs.FileInfo, error) { return nil, nil }

func listNavFiles(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var dirs []os.DirEntry
	dirs = append(dirs, dotEntry(".."))

	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}

	return dirs, nil
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
