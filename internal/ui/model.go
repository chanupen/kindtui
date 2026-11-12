package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/chanupen/kindtui/internal/engine"
)

type screen int

const (
	scrDash screen = iota
	scrCreate
	scrOp
	scrInfo
)

type pane int

const (
	paneClusters pane = iota
	paneNodes
)

type modal int

const (
	modalNone modal = iota
	modalConfirm
	modalType
	modalImage
)

const (
	shapeSingle = iota
	shapeRepo
	shapeCustom
)

const maxOpLines = 5000

type Model struct {
	eng           *engine.Engine
	width, height int
	now           time.Time
	frame         int
	spinning      bool
	ticks         int

	tools      engine.Tools
	toolsReady bool
	toolsBusy  bool
	toolsSeq   int

	clusters   []engine.Cluster
	cursor     int
	filter     textinput.Model
	filtering  bool
	listBusy   bool
	listLoaded bool
	clusterSeq int

	focus      pane
	nodeCursor int

	detail         *engine.Detail
	detailErr      string
	detailName     string
	detailBusy     bool
	detailPending  bool
	detailInflight string
	detailSeq      int

	screen screen

	cmdMode  bool
	cmdInput textinput.Model

	shape        int
	shapeTouched bool
	inputs       []textinput.Model
	formField    int
	formErr      string
	repoPath     string
	repo         engine.ConfigSummary
	nameTouched  bool

	opID      int
	opKind    string
	opTitle   string
	opLines   []string
	opRunning bool
	opErr     string
	opFollow  bool
	opOff     int
	linesCh   <-chan string
	cancel    context.CancelFunc

	modal        modal
	modalTitle   string
	modalBody    string
	modalAct     string
	modalCluster engine.Cluster
	modalNode    string
	modalInput   textinput.Model

	help       bool
	toast      string
	toastErr   bool
	toastUntil time.Time
	lastClick  time.Time
	lastClickY int
}

type clustersMsg struct {
	seq      int
	clusters []engine.Cluster
	err      error
}
type detailMsg struct {
	seq    int
	name   string
	detail engine.Detail
	err    error
}
type configMsg struct {
	path string
	sum  engine.ConfigSummary
	err  error
}
type logLineMsg struct {
	id    int
	lines []string
}
type linesClosedMsg struct{ id int }
type opDoneMsg struct {
	id  int
	err error
}
type clockMsg time.Time
type frameMsg struct{}
type toastMsg struct {
	text string
	err  error
}
type shellDoneMsg struct {
	what string
	err  error
}
type toolsMsgSeq struct {
	seq   int
	tools engine.Tools
}

func New() Model { return NewWith(engine.New()) }

func NewWith(e *engine.Engine) Model {
	m := Model{
		eng:       e,
		now:       time.Now(),
		shape:     shapeRepo,
		toolsBusy: true,
		spinning:  true,
		screen:    scrDash,
		opFollow:  true,
	}
	m.filter = field("", "name or status", 24)
	m.cmdInput = field("", "start · stop · shell NODE · logs · k9s · create · help", 48)
	m.modalInput = field("", "", 40)
	m.inputs = []textinput.Model{
		field("kind", "name", 40),
		field("2", "workers", 6),
		field("", "kind default", 48),
		field("180", "seconds", 6),
	}
	m.inputs[0].CharLimit = 63
	m.inputs[1].CharLimit = 2
	m.inputs[2].CharLimit = 256
	m.inputs[3].CharLimit = 4
	return m
}

func field(value, placeholder string, width int) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.CharLimit = 200
	in.Width = width
	in.PlaceholderStyle = paint(dimC)
	in.TextStyle = paint(textC)
	in.Cursor.Style = paint(accentC)
	if value != "" {
		in.SetValue(value)
	}
	return in
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(probeCmd(m.eng, 0), m.configCmd(), m.clockCmd(), frameCmd())
}

func probeCmd(eng *engine.Engine, seq int) tea.Cmd {
	return func() tea.Msg {
		if eng == nil {
			return toolsMsgSeq{seq: seq}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		return toolsMsgSeq{seq: seq, tools: eng.Probe(ctx)}
	}
}

func (m Model) issueProbe() (Model, tea.Cmd) {
	if m.eng == nil {
		m.toolsBusy = false
		m.toolsReady = true
		return m, nil
	}
	m.toolsSeq++
	m.toolsBusy = true
	seq := m.toolsSeq
	eng := m.eng
	m, spin := m.kick()
	return m, tea.Batch(probeCmd(eng, seq), spin)
}

func (m Model) configCmd() tea.Cmd {
	return func() tea.Msg {
		path, err := engine.FindConfig("")
		if err != nil {
			return configMsg{err: err}
		}
		sum, perr := engine.ParseConfig(path)
		return configMsg{path: path, sum: sum, err: perr}
	}
}

func (m Model) clockCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return clockMsg(t) })
}

func frameCmd() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return frameMsg{} })
}

func (m Model) busy() bool {
	return m.toolsBusy || m.listBusy || m.detailBusy || m.opRunning
}

func (m Model) kick() (Model, tea.Cmd) {
	if m.busy() && !m.spinning {
		m.spinning = true
		return m, frameCmd()
	}
	return m, nil
}

func (m Model) refresh() (Model, tea.Cmd) {
	if m.eng == nil || !m.tools.DaemonOK || m.listBusy {
		return m, nil
	}
	m.listBusy = true
	m.clusterSeq++
	seq := m.clusterSeq
	eng := m.eng
	m, spin := m.kick()
	return m, tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		cs, err := eng.List(ctx)
		return clustersMsg{seq: seq, clusters: cs, err: err}
	}, spin)
}

// requestDetail loads node IPs, readiness, stats, and pods for the selected cluster.
func (m Model) requestDetail() (Model, tea.Cmd) {
	c, ok := m.selected()
	if !ok || m.eng == nil {
		return m, nil
	}
	if c.Status == engine.StatusStopped {
		m.detail = nil
		m.detailErr = ""
		m.detailName = c.Name
		return m, nil
	}
	if m.detailBusy {
		if m.detailInflight != c.Name {
			m.detailPending = true
		}
		return m, nil
	}
	m.detailBusy = true
	m.detailInflight = c.Name
	m.detailSeq++
	seq := m.detailSeq
	name := c.Name
	eng := m.eng
	m, spin := m.kick()
	return m, tea.Batch(func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		d, err := eng.Detail(ctx, name)
		return detailMsg{seq: seq, name: name, detail: d, err: err}
	}, spin)
}

// listenLines drains whatever is buffered so a chatty log does not cost one
// render per line.
func listenLines(ch <-chan string, id int) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return linesClosedMsg{id: id}
		}
		batch := []string{line}
		for len(batch) < 256 {
			select {
			case l, ok := <-ch:
				if !ok {
					return logLineMsg{id: id, lines: batch}
				}
				batch = append(batch, l)
			default:
				return logLineMsg{id: id, lines: batch}
			}
		}
		return logLineMsg{id: id, lines: batch}
	}
}

func waitDone(ch <-chan error, id int) tea.Cmd {
	return func() tea.Msg {
		err := <-ch
		return opDoneMsg{id: id, err: err}
	}
}

// begin runs fn in the background and shows its output on the operation screen.
// follow pins the view to the newest line; otherwise it starts at the top.
func (m Model) begin(kind, title string, timeout time.Duration, follow bool, fn func(context.Context, func(string)) error) (Model, tea.Cmd) {
	if m.eng == nil {
		return m.note("engine is not ready"), nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.opID++
	id := m.opID
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancel = cancel
	m.opKind = kind
	m.opTitle = title
	m.opLines = nil
	m.opRunning = true
	m.opErr = ""
	m.opFollow = follow
	m.opOff = 0
	m.screen = scrOp
	m.modal = modalNone
	m.help = false
	m.filtering = false
	m.cmdMode = false
	m = m.blur()

	lines := make(chan string, 1024)
	done := make(chan error, 1)
	m.linesCh = lines
	go func() {
		err := fn(ctx, func(s string) {
			select {
			case lines <- s:
			case <-ctx.Done():
			}
		})
		done <- err
		close(lines)
	}()
	m, spin := m.kick()
	return m, tea.Batch(listenLines(lines, id), waitDone(done, id), spin)
}

func (m Model) blur() Model {
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
	m.filter.Blur()
	m.cmdInput.Blur()
	m.modalInput.Blur()
	return m
}

func (m Model) note(text string) Model {
	m.toast = text
	m.toastErr = false
	m.toastUntil = m.now.Add(5 * time.Second)
	return m
}

func (m Model) warn(text string) Model {
	m = m.note(text)
	m.toastErr = true
	m.toastUntil = m.now.Add(8 * time.Second)
	return m
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	return m, tea.Quit
}

func (m Model) showBanner() bool {
	return m.toolsReady && !m.tools.DaemonOK
}
