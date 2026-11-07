package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	setupScreen screen = iota
	chatScreen
)

type linkState int

const (
	starting linkState = iota
	up
	down
	finished // a replayed recording ended
)

// launcher starts flipchat-radio; tests replace it.
type launcher func(src source, gen int, argv []string, out chan<- tea.Msg) (*backend, error)

type model struct {
	cfg    config
	launch launcher
	events chan tea.Msg
	screen screen

	// setup screen
	choice    role
	freqInput textinput.Model
	editFreq  bool
	setupErr  string

	// chat screen
	role     role
	proc     *backend
	gen      int // bumped for every process, so a replaced one's messages are ignored
	state    linkState
	detail   string // Flipper name, "listening", or why the link is down
	lastErr  string // the process's last "error: ..." line
	entries  []entry
	received int // chat lines received (listener)
	vp       viewport.Model
	input    textinput.Model
	stopping bool // leaving the chat, to quit or to change role

	width, height int
	sized         bool
}

type stoppedMsg struct{ quit bool }

func newModel(cfg config, preset *role, launch launcher) model {
	fi := textinput.New()
	fi.Prompt = ""
	fi.CharLimit = 8
	fi.Width = 8
	fi.SetValue(strconv.FormatFloat(cfg.freqMHz, 'f', 3, 64))

	in := textinput.New()
	in.Prompt = "› "
	in.CharLimit = 200

	m := model{cfg: cfg, launch: launch, events: make(chan tea.Msg, 256), freqInput: fi, input: in}
	if preset != nil {
		m.choice = *preset
		m.startChat(*preset)
	}
	return m
}

func waitEvent(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

func (m model) Init() tea.Cmd { return tea.Batch(textinput.Blink, waitEvent(m.events)) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-6)
		if !m.sized {
			m.vp = viewport.New(msg.Width, 1)
			m.sized = true
		}
		m.vp.Width = msg.Width

	case tea.KeyMsg:
		switch {
		case m.stopping:
		case m.screen == setupScreen:
			cmd = m.updateSetup(msg)
		default:
			cmd = m.updateChat(msg)
		}

	case eventMsg:
		cmd = waitEvent(m.events)
		if msg.gen == m.gen {
			m.handleEvent(msg.ev)
		}

	case stderrMsg:
		cmd = waitEvent(m.events)
		if msg.gen == m.gen && strings.HasPrefix(msg.line, "error: ") {
			m.lastErr = strings.TrimPrefix(msg.line, "error: ")
		}

	case exitMsg:
		cmd = waitEvent(m.events)
		if msg.gen == m.gen {
			m.handleExit(msg.err)
		}

	case stoppedMsg:
		if msg.quit {
			return m, tea.Quit
		}
		m.screen, m.stopping = setupScreen, false
	}

	m.layout()
	return m, cmd
}

// ---------------------------------------------------------------- setup screen

func (m *model) updateSetup(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	switch key {
	case "ctrl+c":
		return tea.Quit
	case "enter":
		m.submitSetup()
		return nil
	}
	if m.editFreq {
		if key == "tab" || key == "esc" {
			m.setEditFreq(false)
			return nil
		}
		var cmd tea.Cmd
		m.freqInput, cmd = m.freqInput.Update(k)
		return cmd
	}
	switch key {
	case "q":
		return tea.Quit
	case "up", "k", "down", "j":
		m.choice = 1 - m.choice
	case "1":
		m.choice = sender
	case "2":
		m.choice = listener
	case "tab", "f":
		m.setEditFreq(true)
	}
	return nil
}

func (m *model) setEditFreq(on bool) {
	m.editFreq = on
	if on {
		m.freqInput.Focus()
		m.freqInput.CursorEnd()
	} else {
		m.freqInput.Blur()
	}
}

func (m *model) submitSetup() {
	f, err := strconv.ParseFloat(strings.TrimSpace(m.freqInput.Value()), 64)
	if err != nil || f < 280 || f > 960 {
		m.setupErr = "Enter a frequency in MHz between 280 and 960, e.g. 433.92"
		m.setEditFreq(true)
		return
	}
	m.setupErr = ""
	m.setEditFreq(false)
	m.cfg.freqMHz = f
	m.startChat(m.choice)
}

// ---------------------------------------------------------------- chat screen

func (m *model) startChat(r role) {
	m.screen, m.role = chatScreen, r
	m.entries, m.received = nil, 0
	switch r {
	case sender:
		m.info(fmt.Sprintf("Sender on %.3f MHz: what you type goes out through the Flipper. "+
			"Anyone listening on this frequency (an RTL-SDR, another Flipper) can read it.", m.cfg.freqMHz))
		m.input.Focus()
	case listener:
		src := "the RTL-SDR"
		if m.cfg.file != "" {
			src = "a recording (" + m.cfg.file + ")"
		}
		m.info(fmt.Sprintf("Listener on %.3f MHz: Flipper chat heard by %s appears here.", m.cfg.freqMHz, src))
		m.input.Blur()
	}
	m.connect()
}

func (m *model) connect() {
	m.gen++
	m.lastErr, m.detail = "", ""
	proc, err := m.launch(m.role.source(), m.gen, m.cfg.argv(m.role), m.events)
	if err != nil {
		m.state, m.detail = down, err.Error()
		m.errorf("%v", err)
		return
	}
	m.proc, m.state = proc, starting
}

// leave stops flipchat-radio, then quits or goes back to the setup screen.
func (m *model) leave(quit bool) tea.Cmd {
	m.stopping = true
	proc := m.proc
	m.proc = nil
	m.gen++ // ignore whatever the old process still says
	return func() tea.Msg {
		proc.stop(3 * time.Second)
		return stoppedMsg{quit}
	}
}

func (m *model) handleEvent(ev event) {
	switch ev.Event {
	case "listening":
		m.state, m.detail = up, "listening"
		if ev.Source != "rtl-sdr" {
			m.detail = "replaying"
		}
	case "ready":
		m.state, m.detail = up, ev.Name
	case "line", "heard": // listener: decoded by the RTL-SDR; sender: other Flippers
		m.addLine(ev.Text)
	case "sent":
		for i := range m.entries {
			if e := &m.entries[i]; e.mine && !e.sent && e.text == ev.Text {
				e.sent = true
				break
			}
		}
	}
}

func (m *model) handleExit(err error) {
	m.proc = nil
	if m.role == listener && m.cfg.file != "" && err == nil {
		m.state, m.detail = finished, "end of recording"
		m.info("End of the recording.")
		return
	}
	why := m.lastErr
	if why == "" && err != nil {
		why = err.Error()
	}
	if why == "" {
		why = "stopped"
	}
	m.state, m.detail = down, why
	m.errorf("%s: %s — press ctrl+r to retry.", m.deviceName(), why)
}

func (m *model) updateChat(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+c", "ctrl+d":
		return m.leave(true)
	case "esc":
		return m.leave(false)
	case "ctrl+r":
		if m.state == down {
			m.info("Reconnecting the " + m.deviceName() + "…")
			m.connect()
		}
		return nil
	case "pgup":
		m.vp.HalfPageUp()
		return nil
	case "pgdown":
		m.vp.HalfPageDown()
		return nil
	case "up":
		m.vp.ScrollUp(1)
		return nil
	case "down":
		m.vp.ScrollDown(1)
		return nil
	case "home":
		m.vp.GotoTop()
		return nil
	case "end":
		m.vp.GotoBottom()
		return nil
	}
	if m.role == listener {
		if k.String() == "q" {
			return m.leave(true)
		}
		return nil
	}
	if k.String() == "enter" {
		m.send()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}

func (m *model) send() {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return
	}
	if m.state != up {
		m.errorf("Can't send: the Flipper is not connected yet.")
		return
	}
	if err := m.proc.send(text); err != nil {
		m.errorf("Flipper: %v", err)
		return
	}
	m.entries = append(m.entries, entry{at: time.Now(), kind: chatLine, author: m.detail, text: text, mine: true})
	m.input.Reset()
}

func (m *model) info(text string) {
	m.entries = append(m.entries, entry{at: time.Now(), kind: infoLine, text: text})
}

func (m *model) errorf(format string, a ...any) {
	m.entries = append(m.entries, entry{at: time.Now(), kind: errorLine, text: fmt.Sprintf(format, a...)})
}

func (m *model) addLine(line string) {
	author, text, k := splitLine(line)
	m.entries = append(m.entries, entry{at: time.Now(), kind: k, author: author, text: text})
	if k == chatLine {
		m.received++
	}
}

func (m model) deviceName() string {
	if m.role == sender {
		return "Flipper"
	}
	return "RTL-SDR"
}

// layout sizes the message pane and refills it, following new lines when
// already at the bottom.
func (m *model) layout() {
	if !m.sized || m.screen != chatScreen {
		return
	}
	m.vp.Height = max(1, m.height-lipgloss.Height(m.header())-lipgloss.Height(m.footer()))
	follow := m.vp.AtBottom() || m.vp.TotalLineCount() <= m.vp.Height
	m.vp.SetContent(m.render())
	if follow {
		m.vp.GotoBottom()
	}
}
