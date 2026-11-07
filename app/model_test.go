package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeLauncher records launches and hands out backends whose stdin is a buffer.
type fakeLauncher struct {
	argvs [][]string
	stdin bytes.Buffer
	fail  error
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

func (f *fakeLauncher) launch(src source, gen int, argv []string, out chan<- tea.Msg) (*backend, error) {
	f.argvs = append(f.argvs, argv)
	if f.fail != nil {
		return nil, f.fail
	}
	done := make(chan struct{})
	close(done) // already "exited": stop() returns at once
	return &backend{src: src, gen: gen, stdin: nopCloser{&f.stdin}, done: done}, nil
}

func newTestModel(t *testing.T, preset *role) (model, *fakeLauncher) {
	t.Helper()
	fl := &fakeLauncher{}
	cfg := config{freqMHz: 433.92, radio: []string{"flipchat-radio"}}
	m := newModel(cfg, preset, fl.launch)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, fl
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(model)
}

// press sends keys: special names ("enter", "tab", ...) or text typed rune by rune.
func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	special := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "tab": tea.KeyTab, "esc": tea.KeyEsc, "down": tea.KeyDown, "up": tea.KeyUp,
		"ctrl+c": tea.KeyCtrlC, "ctrl+r": tea.KeyCtrlR, "backspace": tea.KeyBackspace,
	}
	var cmd tea.Cmd
	for _, k := range keys {
		if kt, ok := special[k]; ok {
			var next tea.Model
			next, cmd = m.Update(tea.KeyMsg{Type: kt})
			m = next.(model)
			continue
		}
		for _, r := range k {
			var next tea.Model
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			m = next.(model)
		}
	}
	return m, cmd
}

func ev(m model, e event) eventMsg { return eventMsg{src: m.role.source(), gen: m.gen, ev: e} }

func ptr(r role) *role { return &r }

func lastEntry(m model) entry { return m.entries[len(m.entries)-1] }

// runs a command chain until it settles; returns the final message.
func run(t *testing.T, m model, cmd tea.Cmd) (model, tea.Msg) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if _, quit := msg.(tea.QuitMsg); quit {
			return m, msg
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(model)
		if _, stopped := msg.(stoppedMsg); stopped && cmd == nil {
			return m, msg
		}
	}
	return m, nil
}

// ---------------------------------------------------------------- setup screen

func TestStartsOnTheSetupScreenWithSenderSelected(t *testing.T) {
	m, fl := newTestModel(t, nil)
	if m.screen != setupScreen || m.choice != sender {
		t.Fatalf("screen=%v choice=%v, want setup/sender", m.screen, m.choice)
	}
	if len(fl.argvs) != 0 {
		t.Errorf("launched %v before a role was chosen", fl.argvs)
	}
	if v := m.View(); !strings.Contains(v, "What's plugged into this computer?") {
		t.Errorf("setup view missing its question:\n%s", v)
	}
}

func TestChoosingTheRole(t *testing.T) {
	m, _ := newTestModel(t, nil)
	m, _ = press(t, m, "down")
	if m.choice != listener {
		t.Errorf("down: choice=%v, want listener", m.choice)
	}
	m, _ = press(t, m, "1")
	if m.choice != sender {
		t.Errorf("1: choice=%v, want sender", m.choice)
	}
	m, _ = press(t, m, "2")
	if m.choice != listener {
		t.Errorf("2: choice=%v, want listener", m.choice)
	}
}

func TestEnterStartsTheChosenRole(t *testing.T) {
	m, fl := newTestModel(t, nil)
	m, _ = press(t, m, "2", "enter")
	if m.screen != chatScreen || m.role != listener {
		t.Fatalf("screen=%v role=%v, want chat/listener", m.screen, m.role)
	}
	if len(fl.argvs) != 1 || !slices.Contains(fl.argvs[0], "listen") {
		t.Errorf("launched %v, want one listen", fl.argvs)
	}
}

func TestEditingTheFrequency(t *testing.T) {
	m, fl := newTestModel(t, nil)
	m, _ = press(t, m, "tab")
	if !m.editFreq {
		t.Fatal("tab did not start editing the frequency")
	}
	m, _ = press(t, m, "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	m, _ = press(t, m, "868.35", "2") // "2" is typed into the field, not a role choice
	if m.choice != sender {
		t.Error("a digit typed in the frequency field changed the role")
	}
	m, _ = press(t, m, "backspace", "esc", "2", "enter")
	if m.cfg.freqMHz != 868.35 {
		t.Errorf("freq = %v, want 868.35", m.cfg.freqMHz)
	}
	if !slices.Contains(fl.argvs[0], "868350000") {
		t.Errorf("argv %v does not carry 868350000 Hz", fl.argvs[0])
	}
}

func TestInvalidFrequencyStaysOnSetup(t *testing.T) {
	m, fl := newTestModel(t, nil)
	m, _ = press(t, m, "tab", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	m, _ = press(t, m, "12", "enter")
	if m.screen != setupScreen || m.setupErr == "" || !m.editFreq {
		t.Errorf("screen=%v err=%q editing=%v, want setup with an error", m.screen, m.setupErr, m.editFreq)
	}
	if len(fl.argvs) != 0 {
		t.Error("launched with an invalid frequency")
	}
	if !strings.Contains(m.View(), "between 280 and 960") {
		t.Error("the error is not shown")
	}
}

func TestQuitFromSetup(t *testing.T) {
	m, _ := newTestModel(t, nil)
	if _, cmd := press(t, m, "q"); cmd == nil || cmd() != tea.Quit() {
		t.Error("q on the setup screen does not quit")
	}
}

func TestPresetRoleSkipsSetup(t *testing.T) {
	m, fl := newTestModel(t, ptr(sender))
	if m.screen != chatScreen || m.role != sender || len(fl.argvs) != 1 {
		t.Errorf("screen=%v role=%v launches=%d, want chat/sender/1", m.screen, m.role, len(fl.argvs))
	}
}

// ---------------------------------------------------------------- sender

func TestSenderTypesAndSends(t *testing.T) {
	m, fl := newTestModel(t, ptr(sender))
	m = update(t, m, ev(m, event{Event: "ready", Name: "R1kori7h"}))
	if m.state != up || m.detail != "R1kori7h" {
		t.Fatalf("state=%v detail=%q, want up/R1kori7h", m.state, m.detail)
	}
	m, _ = press(t, m, "hello there", "enter")
	if got := fl.stdin.String(); got != "hello there\n" {
		t.Errorf("sent %q to the Python side, want %q", got, "hello there\n")
	}
	e := lastEntry(m)
	if !e.mine || e.sent || e.author != "R1kori7h" || e.text != "hello there" {
		t.Errorf("entry = %+v, want my pending line", e)
	}
	if m.input.Value() != "" {
		t.Error("input not cleared after sending")
	}
	m = update(t, m, ev(m, event{Event: "sent", Text: "hello there"}))
	if !lastEntry(m).sent {
		t.Error("sent event did not mark the line as sent")
	}
	if v := m.View(); !strings.Contains(v, "hello there") || !strings.Contains(v, "sent") {
		t.Errorf("view does not show the sent line:\n%s", v)
	}
}

func TestSenderShowsOtherFlippers(t *testing.T) {
	m, _ := newTestModel(t, ptr(sender))
	m = update(t, m, ev(m, event{Event: "ready", Name: "R1kori7h"}))
	m = update(t, m, ev(m, event{Event: "heard", Text: "Zorkly: hi"}))
	if e := lastEntry(m); e.mine || e.author != "Zorkly" || e.text != "hi" {
		t.Errorf("entry = %+v, want Zorkly's line", e)
	}
}

func TestSenderCannotSendBeforeTheFlipperIsReady(t *testing.T) {
	m, fl := newTestModel(t, ptr(sender))
	m, _ = press(t, m, "too early", "enter")
	if fl.stdin.Len() != 0 {
		t.Error("sent before the Flipper was ready")
	}
	if lastEntry(m).kind != errorLine {
		t.Error("no error shown")
	}
}

func TestEmptyLinesAreNotSent(t *testing.T) {
	m, fl := newTestModel(t, ptr(sender))
	m = update(t, m, ev(m, event{Event: "ready", Name: "x"}))
	press(t, m, "   ", "enter")
	if fl.stdin.Len() != 0 {
		t.Errorf("sent %q", fl.stdin.String())
	}
}

// ---------------------------------------------------------------- listener

func TestListenerShowsAndCountsLines(t *testing.T) {
	m, _ := newTestModel(t, ptr(listener))
	m = update(t, m, ev(m, event{Event: "listening", Source: "rtl-sdr"}))
	m = update(t, m, ev(m, event{Event: "line", Text: "R1kori7h joined chat."}))
	m = update(t, m, ev(m, event{Event: "line", Text: "R1kori7h: Hello"}))
	if m.received != 1 {
		t.Errorf("received = %d, want 1 (join lines don't count)", m.received)
	}
	v := m.View()
	for _, want := range []string{"Listener · 433.920 MHz", "1 message", "RTL-SDR", "listening", "R1kori7h", "Hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestListenerIgnoresTyping(t *testing.T) {
	m, fl := newTestModel(t, ptr(listener))
	m, _ = press(t, m, "abc", "enter")
	if m.input.Value() != "" || fl.stdin.Len() != 0 {
		t.Error("the listener accepted typed text")
	}
}

func TestListenerQuitsWithQ(t *testing.T) {
	m, _ := newTestModel(t, ptr(listener))
	m, cmd := press(t, m, "q")
	if _, msg := run(t, m, cmd); msg != tea.Quit() {
		t.Errorf("q ended with %v, want quit", msg)
	}
}

func TestReplayedRecordingEnds(t *testing.T) {
	fl := &fakeLauncher{}
	m := newModel(config{freqMHz: 433.92, file: "cap.cu8"}, ptr(listener), fl.launch)
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, ev(m, event{Event: "listening", Source: "cap.cu8"}))
	if m.detail != "replaying" {
		t.Errorf("detail = %q, want replaying", m.detail)
	}
	m = update(t, m, exitMsg{src: sdr, gen: m.gen})
	if m.state != finished {
		t.Errorf("state = %v, want finished", m.state)
	}
}

// ---------------------------------------------------------------- both roles

func TestProcessFailureShowsTheErrorAndCtrlRRetries(t *testing.T) {
	m, fl := newTestModel(t, ptr(listener))
	m = update(t, m, stderrMsg{src: sdr, gen: m.gen, line: "error: rtl_sdr stopped: usb_open error -3"})
	m = update(t, m, exitMsg{src: sdr, gen: m.gen, err: errors.New("exit status 1")})
	if m.state != down || m.detail != "rtl_sdr stopped: usb_open error -3" {
		t.Fatalf("state=%v detail=%q", m.state, m.detail)
	}
	if e := lastEntry(m); e.kind != errorLine || !strings.Contains(e.text, "usb_open error") {
		t.Errorf("entry = %+v, want the rtl_sdr error", e)
	}
	gen := m.gen
	m, _ = press(t, m, "ctrl+r")
	if len(fl.argvs) != 2 || m.gen != gen+1 || m.state != starting {
		t.Errorf("launches=%d gen=%d state=%v after ctrl+r", len(fl.argvs), m.gen, m.state)
	}
}

func TestCtrlRDoesNothingWhileConnected(t *testing.T) {
	m, fl := newTestModel(t, ptr(listener))
	m = update(t, m, ev(m, event{Event: "listening", Source: "rtl-sdr"}))
	press(t, m, "ctrl+r")
	if len(fl.argvs) != 1 {
		t.Error("ctrl+r relaunched a running process")
	}
}

func TestLaunchFailureIsShown(t *testing.T) {
	fl := &fakeLauncher{fail: errors.New("exec: no such file")}
	m := newModel(config{freqMHz: 433.92}, ptr(sender), fl.launch)
	if m.state != down || lastEntry(m).kind != errorLine {
		t.Errorf("state=%v last=%+v, want down with an error", m.state, lastEntry(m))
	}
}

func TestMessagesFromAReplacedProcessAreIgnored(t *testing.T) {
	m, _ := newTestModel(t, ptr(listener))
	stale := m.gen
	m, _ = press(t, m, "esc")
	m = update(t, m, eventMsg{src: sdr, gen: stale, ev: event{Event: "line", Text: "ghost: boo"}})
	m = update(t, m, exitMsg{src: sdr, gen: stale, err: errors.New("killed")})
	for _, e := range m.entries {
		if e.text == "boo" || e.kind == errorLine {
			t.Errorf("stale message shown: %+v", e)
		}
	}
}

func TestEscGoesBackToSetup(t *testing.T) {
	m, _ := newTestModel(t, ptr(sender))
	m, cmd := press(t, m, "esc")
	if !m.stopping {
		t.Error("not stopping after esc")
	}
	m, _ = run(t, m, cmd)
	if m.screen != setupScreen || m.stopping {
		t.Errorf("screen=%v stopping=%v, want back on setup", m.screen, m.stopping)
	}
}

func TestKeysAreIgnoredWhileStopping(t *testing.T) {
	m, fl := newTestModel(t, ptr(sender))
	m = update(t, m, ev(m, event{Event: "ready", Name: "x"}))
	m, _ = press(t, m, "esc")
	press(t, m, "late", "enter")
	if fl.stdin.Len() != 0 {
		t.Error("a key was handled while leaving")
	}
}
