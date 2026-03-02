package tui

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sakvarelidze/kradar/internal/helm"
	"github.com/sakvarelidze/kradar/internal/kube"
	"github.com/sakvarelidze/kradar/internal/scan"
)

type viewMode int

const (
	modeSplash viewMode = iota
	modeTable
	modeDetails
	modeDegraded
	modeError
)

type connStage int

const (
	stageConfigLoaded connStage = iota
	stageProbing
	stageConnected
	stageDegraded
	stageError
)

type sortMode int

const (
	sortStatus sortMode = iota
	sortNamespace
	sortRelease
	sortPodsDesc
)

type scanProgressMsg struct {
	done  int
	total int
}

type scanDoneMsg struct {
	rows []helm.ServiceRow
	err  error
}

type probeDoneMsg struct {
	err error
}

type probeWatchdogMsg struct {
	attempt time.Time
}

type tickMsg time.Time
type splashDoneMsg struct{}

type Meta struct {
	ConfigPath     string
	CheckerEnabled bool
}

type Model struct {
	scanner         *scan.Scanner
	kubeClient      *kube.Client
	scanOpts        scan.Options
	refreshInterval time.Duration
	rows            []helm.ServiceRow
	loading         bool
	statusLine      string
	lastRefresh     time.Time
	lastErr         error
	detailPods      []kube.PodInfo
	detailRollout   kube.RolloutHistory
	scanEvents      chan tea.Msg

	mode           viewMode
	splashStart    time.Time
	splashDuration time.Duration
	splashDone     bool
	firstRun       bool
	helpVisible    bool
	sortBy         sortMode

	table  table.Model
	width  int
	height int

	headerStyle        lipgloss.Style
	footerStyle        lipgloss.Style
	splashTitleStyle   lipgloss.Style
	splashSubStyle     lipgloss.Style
	splashHintStyle    lipgloss.Style
	overlayStyle       lipgloss.Style
	headerAppNameStyle lipgloss.Style
	releaseColWidth    int
	imagesColWidth     int
	configPath         string
	checkerEnabled     bool
	showConfigModal    bool

	connStage        connStage
	connErrMsg       string
	connErrDetail    string
	connHint         string
	lastAttempt      time.Time
	clusterName      string
	apiserver        string
	probeCancel      context.CancelFunc
	detailKey        string
	detailPodsErr    error
	detailRolloutErr error
}

func New(scanner *scan.Scanner, kubeClient *kube.Client, scanOpts scan.Options, refreshInterval time.Duration, meta Meta) *Model {
	releaseColWidth := 34
	imagesColWidth := 50
	cols := []table.Column{
		{Title: "NAMESPACE", Width: 14},
		{Title: "RELEASE", Width: releaseColWidth},
		{Title: "CHART@VER", Width: 22},
		{Title: "APPVER", Width: 10},
		{Title: "PODS", Width: 5},
		{Title: "STATUS", Width: 10},
		{Title: "IMAGES", Width: imagesColWidth},
	}
	tbl := table.New(table.WithColumns(cols), table.WithRows([]table.Row{}), table.WithFocused(true), table.WithHeight(12))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true)
	styles.Selected = styles.Selected.Bold(true)
	tbl.SetStyles(styles)

	return &Model{
		scanner:         scanner,
		kubeClient:      kubeClient,
		scanOpts:        scanOpts,
		refreshInterval: refreshInterval,
		statusLine:      "initializing scan...",
		mode:            modeSplash,
		splashStart:     time.Now(),
		splashDuration:  1500 * time.Millisecond,
		splashDone:      false,
		firstRun:        true,
		sortBy:          sortStatus,
		table:           tbl,
		headerStyle: lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255")).
			Padding(0, 1),
		footerStyle:      lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1),
		splashTitleStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")),
		splashSubStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true),
		splashHintStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		overlayStyle: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).
			Padding(1, 2).Background(lipgloss.Color("235")).Foreground(lipgloss.Color("252")),
		headerAppNameStyle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
		releaseColWidth:    releaseColWidth,
		imagesColWidth:     imagesColWidth,
		configPath:         meta.ConfigPath,
		checkerEnabled:     meta.CheckerEnabled,
		connStage:          stageConfigLoaded,
		clusterName:        kubeClient.ContextName,
		apiserver:          kubeClient.APIServerURL,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.startScanCmd(), tickCmd(m.refreshInterval), tickSplashDone(m.splashDuration))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTable()
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case scanProgressMsg:
		m.statusLine = fmt.Sprintf("scanning namespaces %d/%d", msg.done, msg.total)
		if m.scanEvents != nil {
			return m, waitScanEventCmd(m.scanEvents)
		}
	case scanDoneMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		m.lastErr = msg.err
		if msg.err == nil {
			m.connStage = stageConnected
			m.rows = msg.rows
			if !m.checkerEnabled {
				for i := range m.rows {
					m.rows[i].ChartStatusReason = "no_config_loaded"
					m.rows[i].Reason = "no_config_loaded"
				}
			}
			m.applySort()
			m.refreshTableRows()
			m.statusLine = fmt.Sprintf("scan complete (%d releases)", len(msg.rows))
			m.transitionModeAfterStageUpdate()
		} else {
			friendly := kube.ClassifyKubeError(msg.err)
			m.connErrMsg = friendly.Short
			m.connErrDetail = friendly.Detail
			m.connHint = friendly.Hint
			if isConfigError(msg.err) {
				m.connStage = stageError
				m.statusLine = "scan failed due to config error"
			} else {
				m.connStage = stageDegraded
				m.statusLine = "scan failed while cluster is unreachable"
			}
			m.transitionModeAfterStageUpdate()
		}
		m.scanEvents = nil
	case probeDoneMsg:
		m.probeCancel = nil
		if msg.err != nil {
			m.loading = false
			friendly := kube.ClassifyKubeError(msg.err)
			m.connErrMsg = friendly.Short
			m.connErrDetail = friendly.Detail
			m.connHint = friendly.Hint
			m.scanEvents = nil
			if isConfigError(msg.err) {
				m.connStage = stageError
				m.statusLine = "kubeconfig is invalid"
			} else {
				m.connStage = stageDegraded
				m.statusLine = "API probe failed"
			}
			m.transitionModeAfterStageUpdate()
			return m, nil
		}
		m.connStage = stageConnected
		m.statusLine = "scanning namespaces..."
		m.transitionModeAfterStageUpdate()
		if m.scanEvents != nil {
			return m, waitScanEventCmd(m.scanEvents)
		}
	case probeWatchdogMsg:
		if m.loading && m.connStage == stageProbing && m.lastAttempt.Equal(msg.attempt) {
			if m.probeCancel != nil {
				m.probeCancel()
				m.probeCancel = nil
			}
			m.loading = false
			m.connStage = stageDegraded
			m.connErrMsg = "Kubernetes API probe timed out"
			m.connErrDetail = "probe exceeded expected time window"
			m.connHint = "Check network connectivity and retry"
			m.statusLine = "probe timed out (watchdog)"
			m.scanEvents = nil
			m.transitionModeAfterStageUpdate()
		}
	case tickMsg:
		if m.mode == modeDetails {
			return m, tickCmd(m.refreshInterval)
		}
		if m.loading {
			return m, tickCmd(m.refreshInterval)
		}
		if m.connStage == stageError {
			return m, tickCmd(m.refreshInterval)
		}
		return m, tea.Batch(m.startScanCmd(), tickCmd(m.refreshInterval))
	case splashDoneMsg:
		m.splashDone = true
		m.firstRun = false
		m.transitionModeAfterStageUpdate()
	}
	if m.mode == modeTable {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) View() string {
	var base string
	switch m.mode {
	case modeSplash:
		base = m.splashView()
	case modeDetails:
		base = m.detailView()
	case modeDegraded:
		base = m.degradedView()
	case modeError:
		base = m.errorView()
	default:
		base = m.tableView()
	}
	if m.helpVisible {
		return m.renderHelpOverlay(base)
	}
	if m.showConfigModal {
		return m.renderConfigOverlay(base)
	}
	return base
}

func (m *Model) tableView() string {
	header := m.buildHeader()
	footer := m.buildFooter()
	const (
		headerHeight = 1
		footerHeight = 1
	)
	contentHeight := maxInt(3, m.height-headerHeight-footerHeight)
	if m.height <= 0 {
		contentHeight = 20
	}
	m.table.SetHeight(maxInt(3, contentHeight))
	body := m.table.View()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" || key == "q" {
		return m, tea.Quit
	}

	if m.helpVisible {
		if key == "?" || key == "esc" {
			m.helpVisible = false
		}
		return m, nil
	}

	if m.showConfigModal {
		if key == "c" || key == "C" || key == "esc" {
			m.showConfigModal = false
		}
		return m, nil
	}

	switch m.mode {
	case modeError, modeDegraded:
		switch key {
		case "r":
			if m.probeCancel != nil {
				m.probeCancel()
				m.probeCancel = nil
			}
			if m.loading {
				return m, nil
			}
			return m, m.startScanCmd()
		case "c", "C":
			m.showConfigModal = !m.showConfigModal
			return m, nil
		case "?":
			m.helpVisible = !m.helpVisible
			return m, nil
		default:
			return m, nil
		}
	case modeDetails:
		if key == "r" {
			if m.probeCancel != nil {
				m.probeCancel()
				m.probeCancel = nil
			}
			if m.loading {
				m.statusLine = "already refreshing..."
				return m, nil
			}
			return m, m.startScanCmd()
		}
		if key == "esc" {
			if m.splashDone {
				m.mode = modeTable
			} else {
				m.mode = modeSplash
			}
			m.table.Focus()
			return m, nil
		}
	case modeTable:
		switch key {
		case "r":
			if m.probeCancel != nil {
				m.probeCancel()
				m.probeCancel = nil
			}
			if m.loading {
				m.statusLine = "already refreshing..."
				return m, nil
			}
			return m, m.startScanCmd()
		case "c", "C":
			m.showConfigModal = !m.showConfigModal
			return m, nil
		case "?":
			m.helpVisible = !m.helpVisible
			return m, nil
		case "s":
			m.sortBy = (m.sortBy + 1) % 4
			m.applySort()
			m.refreshTableRows()
			return m, nil
		case "enter":
			if len(m.rows) == 0 {
				return m, nil
			}
			selected, ok := m.selectedServiceRow()
			if !ok {
				return m, nil
			}
			m.mode = modeDetails
			m.detailKey = detailKey(selected.Namespace, selected.Release)
			m.detailPods = nil
			m.detailPodsErr = nil
			m.detailRollout = kube.RolloutHistory{}
			m.detailRolloutErr = nil
			pods, err := m.kubeClient.ListPodsByRelease(context.Background(), selected.Namespace, selected.Release)
			if err != nil {
				m.detailPodsErr = err
			} else {
				m.detailPods = pods
			}
			rollout, err := m.kubeClient.GetRolloutHistoryByRelease(context.Background(), selected.Namespace, selected.Release)
			if err != nil {
				m.detailRolloutErr = err
			} else {
				m.detailRollout = rollout
			}
			return m, nil
		default:
			if m.connStage == stageConnected {
				var cmd tea.Cmd
				m.table, cmd = m.table.Update(msg)
				return m, cmd
			}
			return m, nil
		}
	case modeSplash:
		if key == "?" {
			m.helpVisible = !m.helpVisible
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) degradedView() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	h := maxInt(5, m.height-2)
	lines := []string{
		"⚠ Kubernetes API unreachable",
		"",
		"Config loaded, but API probe failed.",
		fmt.Sprintf("Last attempt: %s", formatTime(m.lastAttempt)),
		fmt.Sprintf("Error: %s", emptyDash(m.connErrMsg)),
		"",
		"Press r to retry, c for connection info, q to quit",
	}
	card := m.overlayStyle.Width(maxInt(60, minInt(110, w-8))).Render(strings.Join(lines, "\n"))
	body := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, card)
	return lipgloss.JoinVertical(lipgloss.Left, m.buildHeader(), body, m.buildFooter())
}

func (m *Model) errorView() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	h := maxInt(5, m.height-2)
	lines := []string{
		"❌ Kubernetes configuration error",
		"",
		fmt.Sprintf("Context: %s", emptyDash(m.clusterName)),
	}
	if strings.TrimSpace(m.apiserver) != "" {
		lines = append(lines, fmt.Sprintf("API Server: %s", m.apiserver))
	}
	lines = append(lines,
		fmt.Sprintf("Error: %s", emptyDash(m.connErrMsg)),
		fmt.Sprintf("Details: %s", Truncate(emptyDash(m.connErrDetail), 120)),
		fmt.Sprintf("Hint: %s", emptyDash(m.connHint)),
		"",
		"Press r to retry, c for connection info, q to quit",
	)
	card := m.overlayStyle.Width(maxInt(60, minInt(110, w-8))).Render(strings.Join(lines, "\n"))
	body := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, card)
	return lipgloss.JoinVertical(lipgloss.Left, m.buildHeader(), body, m.buildFooter())
}

func (m *Model) splashView() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	h := m.height
	if h <= 0 {
		h = 28
	}
	banner := m.splashTitleStyle.Render(kradarBanner)
	sub := m.splashSubStyle.Render("Kubernetes Release Radar")
	state := m.splashHintStyle.Render(m.statusLine)
	hint := m.splashHintStyle.Render("press q to quit")
	stack := lipgloss.JoinVertical(lipgloss.Center, banner, "", sub, state, hint)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, stack)
}

func (m *Model) detailView() string {
	r, ok := m.detailServiceRow()
	if !ok {
		return "This release is no longer present\n\nesc back • q quit"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Release: %s/%s\n", r.Namespace, r.Release)
	fmt.Fprintf(&b, "Chart: %s@%s appVersion=%s\n\n", r.Chart, r.ChartVer, emptyDash(r.AppVer))
	b.WriteString("Chart Status\n")
	status := strings.TrimSpace(r.ChartStatus)
	if status == "" {
		status = "unknown"
	}
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	switch status {
	case "up_to_date":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	case "outdated":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	case "unknown":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	fmt.Fprintf(&b, "  Status: %s\n", statusStyle.Render(status))
	fmt.Fprintf(&b, "  Current: %s\n", emptyDash(r.ChartVer))
	latest := strings.TrimSpace(r.LatestVersion)
	if latest == "" {
		latest = "-"
	}
	fmt.Fprintf(&b, "  Latest: %s\n", latest)
	fmt.Fprintf(&b, "  Chart (raw): %s\n", emptyDash(r.ChartNameRaw))
	fmt.Fprintf(&b, "  Chart (normalized): %s\n", emptyDash(r.ChartNameNormalized))
	repoLabel := strings.TrimSpace(strings.TrimSpace(r.RepoName) + " " + strings.TrimSpace(r.RepoURL))
	if repoLabel == "" {
		repoLabel = strings.TrimSpace(strings.TrimSpace(r.ChartSourceName) + " " + strings.TrimSpace(r.ChartSourceURL))
	}
	fmt.Fprintf(&b, "  Repo: %s\n", emptyDash(repoLabel))
	fmt.Fprintf(&b, "  Index key tried: %s\n", emptyDash(r.IndexChartKeyTried))
	if status == "unknown" {
		reason := strings.TrimSpace(r.Reason)
		if reason == "" {
			reason = strings.TrimSpace(r.ChartStatusReason)
		}
		if reason == "" && !m.checkerEnabled {
			reason = "no_config_loaded"
		}
		fmt.Fprintf(&b, "  Reason: %s\n", muted.Render(emptyDash(reason)))
	}
	if strings.TrimSpace(r.FetchError) != "" {
		fmt.Fprintf(&b, "  Error: %s\n", muted.Render(r.FetchError))
	}
	b.WriteString("\n")
	b.WriteString("Rollout History\n")
	if m.detailRollout.WorkloadKind == "" {
		b.WriteString("  Workload: -\n")
		b.WriteString("  Previous: - (no Deployment workloads)\n\n")
	} else {
		fmt.Fprintf(&b, "  Workload: %s/%s\n", m.detailRollout.WorkloadKind, m.detailRollout.WorkloadName)
		printReplicaSetRevision(&b, "  Current revision", m.detailRollout.Current)
		if m.detailRollout.Previous == nil {
			b.WriteString("  Previous revision: - (no previous ReplicaSet)\n\n")
		} else {
			printReplicaSetRevision(&b, "  Previous revision", m.detailRollout.Previous)
			b.WriteString("\n")
		}
	}
	if m.detailRolloutErr != nil {
		fmt.Fprintf(&b, "error rollout history: %v\n\n", m.detailRolloutErr)
	}

	b.WriteString("Images:\n")
	if len(r.Images) == 0 {
		b.WriteString("  -\n")
	} else {
		for _, img := range r.Images {
			fmt.Fprintf(&b, "  - %s\n", img)
		}
	}
	b.WriteString("\nPods:\n")
	if len(m.detailPods) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, p := range m.detailPods {
			fmt.Fprintf(&b, "  - %s  status=%s restarts=%d\n", p.Name, p.Status, p.Restarts)
		}
	}
	if m.detailPodsErr != nil {
		fmt.Fprintf(&b, "\nerror: %v\n", m.detailPodsErr)
	}
	b.WriteString("\nr refresh • esc back • q quit")
	return b.String()
}

func printReplicaSetRevision(b *strings.Builder, label string, rs *kube.ReplicaSetRevision) {
	if rs == nil {
		fmt.Fprintf(b, "%s: -\n", label)
		return
	}
	rev := rs.Revision
	if rev == "" {
		rev = "-"
	}
	created := "-"
	if !rs.CreatedAt.IsZero() {
		created = rs.CreatedAt.Format(time.RFC3339)
	}
	fmt.Fprintf(b, "%s: %s (rs: %s, created: %s)\n", label, rev, rs.Name, created)
	if len(rs.Images) == 0 {
		b.WriteString("    -\n")
		return
	}
	for _, img := range rs.Images {
		fmt.Fprintf(b, "    - %s\n", img)
	}
}

func (m *Model) buildHeader() string {
	w := m.width
	if w <= 0 {
		w = 120
	}
	left := m.headerAppNameStyle.Render("KRADAR")
	center := m.connectionSummary()
	right := fmt.Sprintf("releases: %d • last refresh: %s", len(m.rows), formatTime(m.lastRefresh))

	available := w - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if available < 8 {
		right = Truncate(right, maxInt(12, w/3))
		available = w - lipgloss.Width(left) - lipgloss.Width(right) - 4
	}
	center = Truncate(center, maxInt(0, available))

	segments := []string{left, center, right}
	line := lipgloss.JoinHorizontal(lipgloss.Top,
		segments[0],
		strings.Repeat(" ", maxInt(1, (w-lipgloss.Width(segments[0])-lipgloss.Width(segments[1])-lipgloss.Width(segments[2]))/2)),
		segments[1],
		strings.Repeat(" ", maxInt(1, w-lipgloss.Width(segments[0])-lipgloss.Width(segments[1])-lipgloss.Width(segments[2])-maxInt(1, (w-lipgloss.Width(segments[0])-lipgloss.Width(segments[1])-lipgloss.Width(segments[2]))/2))),
		segments[2],
	)
	line = fitToWidth(line, w)
	return m.headerStyle.Width(w).Render(line)
}

func (m *Model) buildFooter() string {
	w := m.width
	if w <= 0 {
		w = 120
	}
	hint := "q quit • r refresh • ↑/↓ move • enter details • s sort • c conn • ? help"
	switch m.mode {
	case modeDetails:
		hint = "r refresh • esc back • q quit"
	case modeError, modeDegraded:
		hint = "q quit • r retry • c conn • ? help"
	}
	hint = fitToWidth(hint, w)
	return m.footerStyle.Width(w).Render(hint)
}

func (m *Model) renderHelpOverlay(base string) string {
	w := m.width
	if w <= 0 {
		w = 120
	}
	h := m.height
	if h <= 0 {
		h = 28
	}
	help := strings.Join([]string{
		"KRADAR Help",
		"",
		"Keys:",
		"  q           quit",
		"  r           refresh",
		"  ↑/↓, j/k    move selection",
		"  enter       details",
		"  s           cycle sort (status/namespace/release/pods)",
		"  ? or esc    close help",
		"",
		"Columns:",
		"  CHART@VER   installed chart and version",
		"  APPVER      application version",
		"  STATUS      chart update status",
		"  IMAGES      sampled workload images",
		"",
		"unknown status means latest chart version could not be determined.",
	}, "\n")
	overlay := m.overlayStyle.Render(help)
	modal := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
	return lipgloss.JoinVertical(lipgloss.Left, base, "", modal)
}

func (m *Model) renderConfigOverlay(base string) string {
	w := m.width
	if w <= 0 {
		w = 120
	}
	h := m.height
	if h <= 0 {
		h = 28
	}
	details := Truncate(emptyDash(m.connErrDetail), 220)
	body := strings.Join([]string{
		"Connection Info",
		"",
		fmt.Sprintf("Config: %s", emptyDash(truncateMiddle(m.configPath, 110))),
		fmt.Sprintf("Context: %s", emptyDash(m.clusterName)),
		fmt.Sprintf("API Server: %s", emptyDash(m.apiserver)),
		fmt.Sprintf("Stage: %s", m.connStage.String()),
		fmt.Sprintf("Last error: %s", emptyDash(m.connErrMsg)),
		fmt.Sprintf("Details: %s", details),
		"",
		"Press c or esc to close",
	}, "\n")
	overlay := m.overlayStyle.Width(maxInt(60, minInt(120, w-8))).Render(body)
	modal := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
	return lipgloss.JoinVertical(lipgloss.Left, base, "", modal)
}

func (m *Model) resizeTable() {
	if m.width <= 0 {
		return
	}
	const (
		namespaceW = 14
		chartW     = 22
		appVerW    = 10
		podsW      = 5
		statusW    = 10
		releaseMin = 24
		releaseMax = 36
		imagesMin  = 20
		chromePad  = 12
	)

	releaseW := clampInt(releaseMin, releaseMax, m.width/4)
	fixedNoDynamic := namespaceW + chartW + appVerW + podsW + statusW
	availableForReleaseAndImages := m.width - fixedNoDynamic - chromePad
	if availableForReleaseAndImages < imagesMin+releaseW {
		releaseW = maxInt(releaseMin, availableForReleaseAndImages-imagesMin)
	}
	imagesW := maxInt(imagesMin, availableForReleaseAndImages-releaseW)
	if imagesW < imagesMin {
		imagesW = imagesMin
	}
	m.releaseColWidth = releaseW
	m.imagesColWidth = imagesW

	cols := []table.Column{
		{Title: "NAMESPACE", Width: namespaceW},
		{Title: "RELEASE", Width: releaseW},
		{Title: "CHART@VER", Width: chartW},
		{Title: "APPVER", Width: appVerW},
		{Title: "PODS", Width: podsW},
		{Title: "STATUS", Width: statusW},
		{Title: "IMAGES", Width: imagesW},
	}
	m.table.SetColumns(cols)
	m.table.SetWidth(maxInt(40, m.width))
	m.refreshTableRows()
}

func (m *Model) applySort() {
	sort.SliceStable(m.rows, func(i, j int) bool {
		a := m.rows[i]
		b := m.rows[j]
		switch m.sortBy {
		case sortNamespace:
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Release < b.Release
		case sortRelease:
			if a.Release != b.Release {
				return a.Release < b.Release
			}
			return a.Namespace < b.Namespace
		case sortPodsDesc:
			ap := podValue(a.Pods)
			bp := podValue(b.Pods)
			if ap != bp {
				return ap > bp
			}
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Release < b.Release
		default:
			as := statusRank(a.ChartStatus)
			bs := statusRank(b.ChartStatus)
			if as != bs {
				return as < bs
			}
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Release < b.Release
		}
	})
}

func (m *Model) refreshTableRows() {
	selectedKey := ""
	if selected, ok := m.selectedServiceRow(); ok {
		selectedKey = detailKey(selected.Namespace, selected.Release)
	}

	rows := make([]table.Row, 0, len(m.rows))
	for _, r := range m.rows {
		pods := "-"
		if r.Pods != nil {
			pods = fmt.Sprintf("%d", *r.Pods)
		}
		statusRaw := strings.TrimSpace(r.ChartStatus)
		if statusRaw == "" {
			if m.scanOpts.Debug {
				log.Printf("debug: empty chart status for %s/%s, defaulting to unknown", r.Namespace, r.Release)
			}
			statusRaw = "unknown"
		}
		imagesSummary := strings.TrimSpace(r.ImagesSummary)
		if imagesSummary == "" {
			imagesSummary = formatImages(r.Images)
		}
		imagesSummary = strings.ReplaceAll(imagesSummary, "\n", " ")
		imagesSummary = strings.ReplaceAll(imagesSummary, "\t", " ")
		statusCell := Truncate(statusRaw, 10)
		imagesCell := Truncate(imagesSummary, m.imagesColWidth)
		rows = append(rows, table.Row{
			r.Namespace,
			r.Release,
			Truncate(fmt.Sprintf("%s@%s", r.Chart, r.ChartVer), 22),
			Truncate(emptyDash(r.AppVer), 10),
			Truncate(pods, 5),
			statusCell,
			imagesCell,
		})
	}
	m.table.SetRows(rows)
	if selectedKey != "" {
		m.selectRowByKey(selectedKey)
	}
}

func (m *Model) selectedServiceRow() (helm.ServiceRow, bool) {
	selected := m.table.SelectedRow()
	if len(selected) < 2 {
		return helm.ServiceRow{}, false
	}
	namespace := selected[0]
	release := selected[1]
	for _, r := range m.rows {
		if r.Namespace == namespace && r.Release == release {
			return r, true
		}
	}
	return helm.ServiceRow{}, false
}

func (m *Model) transitionModeAfterStageUpdate() {
	if m.mode == modeDetails && m.connStage != stageError {
		return
	}
	if !m.splashDone {
		m.mode = modeSplash
		return
	}
	switch m.connStage {
	case stageError:
		m.mode = modeError
	case stageDegraded:
		m.mode = modeDegraded
	default:
		m.mode = modeTable
		m.table.Focus()
	}
}

func (m *Model) startScanCmd() tea.Cmd {
	if m.loading {
		return nil
	}
	if m.probeCancel != nil {
		m.probeCancel()
		m.probeCancel = nil
	}
	m.loading = true
	m.lastAttempt = time.Now()
	attempt := m.lastAttempt
	m.statusLine = "probing Kubernetes API..."
	m.connStage = stageProbing
	if m.mode == modeDetails {
		// Keep details mode during explicit refresh while updating backing data.
	} else if m.splashDone {
		m.mode = modeTable
	} else {
		m.mode = modeSplash
	}
	m.connErrMsg = ""
	m.connErrDetail = ""
	m.connHint = ""
	ch := make(chan tea.Msg, 1)
	m.scanEvents = ch
	opts := m.scanOpts
	probeParentCtx, probeParentCancel := context.WithCancel(context.Background())
	m.probeCancel = probeParentCancel

	probeAndScanCmd := func() tea.Msg {
		go func() {
			probeCtx, probeTimeoutCancel := context.WithTimeout(probeParentCtx, 5*time.Second)
			probeErr := m.kubeClient.Probe(probeCtx)
			probeTimeoutCancel()
			if probeErr != nil {
				ch <- probeDoneMsg{err: probeErr}
				close(ch)
				return
			}
			ch <- probeDoneMsg{}
			ctx, cancel := kube.TimeoutContext(120 * time.Second)
			defer cancel()
			opts.Progress = func(done, total int) {
				ch <- scanProgressMsg{done: done, total: total}
			}
			rows, err := m.scanner.Scan(ctx, opts)
			ch <- scanDoneMsg{rows: rows, err: err}
			close(ch)
		}()
		return <-ch
	}

	return tea.Batch(probeAndScanCmd, probeWatchdogCmd(6*time.Second, attempt))
}

func detailKey(namespace, release string) string {
	return namespace + "/" + release
}

func (m *Model) detailServiceRow() (helm.ServiceRow, bool) {
	if strings.TrimSpace(m.detailKey) == "" {
		return m.selectedServiceRow()
	}
	for _, r := range m.rows {
		if detailKey(r.Namespace, r.Release) == m.detailKey {
			return r, true
		}
	}
	return helm.ServiceRow{}, false
}

func (m *Model) selectRowByKey(key string) {
	for idx, r := range m.rows {
		if detailKey(r.Namespace, r.Release) == key {
			m.table.SetCursor(idx)
			return
		}
	}
}

func waitScanEventCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func probeWatchdogCmd(d time.Duration, attempt time.Time) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return probeWatchdogMsg{attempt: attempt}
	})
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func tickSplashDone(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return splashDoneMsg{} })
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("15:04:05")
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func formatImages(images []string) string {
	if len(images) == 0 {
		return "-"
	}
	if len(images) <= 2 {
		return strings.Join(images, ", ")
	}
	return fmt.Sprintf("%s, %s, +%d more", images[0], images[1], len(images)-2)
}

func statusRank(status string) int {
	switch status {
	case "outdated":
		return 0
	case "unknown":
		return 1
	case "up_to_date":
		return 2
	default:
		return 3
	}
}

func fitToWidth(s string, width int) string {
	trimmed := Truncate(s, width)
	pad := width - lipgloss.Width(trimmed)
	if pad > 0 {
		return trimmed + strings.Repeat(" ", pad)
	}
	return trimmed
}

func podValue(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func (s connStage) String() string {
	switch s {
	case stageConfigLoaded:
		return "config loaded"
	case stageProbing:
		return "probing"
	case stageConnected:
		return "connected"
	case stageDegraded:
		return "degraded"
	case stageError:
		return "error"
	default:
		return "unknown"
	}
}

func (m *Model) connectionSummary() string {
	switch m.connStage {
	case stageProbing:
		frames := []string{"⠋", "⠙", "⠸", "⠴"}
		return fmt.Sprintf("%s Probing API…", frames[(time.Now().UnixNano()/1e8)%int64(len(frames))])
	case stageConnected:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Ready")
	case stageDegraded:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("API unreachable")
	case stageError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("Configuration error")
	default:
		return "Loaded config"
	}
}

func isConfigError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	patterns := []string{"kubeconfig", "invalid configuration", "no context", "context \"\"", "cannot unmarshal", "yaml"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func truncateMiddle(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return Truncate(s, width)
	}
	left := (width - 1) / 2
	right := width - left - 1
	return Truncate(s, left) + "…" + TruncateRight(s, right)
}

func TruncateRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[len(runes)-width:])
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampInt(minV, maxV, v int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
