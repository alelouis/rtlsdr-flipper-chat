package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// script writes an executable shell script standing in for `flipcom-chat`.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-flipcom-chat")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func next(t *testing.T, out chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-out:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no message from the backend within 5 s")
		return nil
	}
}

func TestSenderBackendRoundTrip(t *testing.T) {
	path := script(t, `
echo '{"event": "ready", "name": "R1kori7h"}'
while read line; do echo "{\"event\": \"sent\", \"text\": \"$line\"}"; done
`)
	out := make(chan tea.Msg, 16)
	b, err := startBackend(flipper, 7, []string{path}, out)
	if err != nil {
		t.Fatal(err)
	}
	if msg := next(t, out).(eventMsg); msg.gen != 7 || msg.src != flipper || msg.ev.Event != "ready" || msg.ev.Name != "R1kori7h" {
		t.Errorf("first message = %+v", msg)
	}
	if err := b.send("hello"); err != nil {
		t.Fatal(err)
	}
	if msg := next(t, out).(eventMsg); msg.ev.Event != "sent" || msg.ev.Text != "hello" {
		t.Errorf("after send: %+v", msg)
	}
	start := time.Now()
	b.stop(3 * time.Second) // closing stdin ends the loop
	if time.Since(start) > 2*time.Second {
		t.Error("stop waited for the timeout instead of ending on end of input")
	}
	if msg := next(t, out).(exitMsg); msg.err != nil {
		t.Errorf("exit = %v, want a clean exit", msg.err)
	}
}

func TestListenerBackendStopsOnInterrupt(t *testing.T) {
	path := script(t, `
trap 'exit 0' INT
echo '{"event": "listening", "source": "rtl-sdr"}'
while :; do sleep 0.05; done
`)
	out := make(chan tea.Msg, 16)
	b, err := startBackend(sdr, 1, []string{path}, out)
	if err != nil {
		t.Fatal(err)
	}
	if msg := next(t, out).(eventMsg); msg.ev.Event != "listening" {
		t.Errorf("first message = %+v", msg)
	}
	if b.send("x") == nil {
		t.Error("send on a listener backend should fail: it has no input")
	}
	start := time.Now()
	b.stop(3 * time.Second)
	if time.Since(start) > 2*time.Second {
		t.Error("the listener did not stop on SIGINT")
	}
}

func TestStopKillsAProcessThatIgnoresInterrupts(t *testing.T) {
	path := script(t, `
trap '' INT
echo '{"event": "listening"}'
sleep 30 & wait
`)
	out := make(chan tea.Msg, 16)
	b, err := startBackend(sdr, 1, []string{path}, out)
	if err != nil {
		t.Fatal(err)
	}
	next(t, out)
	start := time.Now()
	b.stop(200 * time.Millisecond) // the child `sleep` holds the pipes: the whole group must die
	if time.Since(start) > 3*time.Second {
		t.Error("stop hung on a process that ignores SIGINT")
	}
}

func TestErrorsAndExitStatusReachTheModel(t *testing.T) {
	path := script(t, `
echo 'not json, printed by mistake'
echo 'error: rtl_sdr stopped: usb_open error -3' >&2
exit 1
`)
	out := make(chan tea.Msg, 16)
	if _, err := startBackend(sdr, 3, []string{path}, out); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for {
		switch msg := next(t, out).(type) {
		case stderrMsg:
			lines = append(lines, msg.line)
			continue
		case exitMsg:
			if msg.err == nil {
				t.Error("exit status 1 reported as success")
			}
		}
		break
	}
	if len(lines) != 2 { // the stray stdout line and the error, both before the exit
		t.Errorf("lines before exit = %q", lines)
	}
}

func TestStartBackendReportsAMissingProgram(t *testing.T) {
	if _, err := startBackend(sdr, 1, []string{"/nonexistent/flipcom-chat"}, make(chan tea.Msg)); err == nil {
		t.Error("no error for a missing program")
	}
}

func TestPythonCommandFindsTheProjectVenv(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"flipcom-chat\"\n"), 0o644)
	exe := filepath.Join(root, ".venv", "bin", "flipcom-chat")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)

	sub := filepath.Join(root, "tui")
	os.Mkdir(sub, 0o755)
	t.Chdir(sub) // found by walking up from the working directory
	got, err := pythonCommand("")
	if err != nil || len(got) != 1 || got[0] != exe {
		t.Errorf("pythonCommand(\"\") = %v, %v; want [%s]", got, err, exe)
	}

	t.Chdir(t.TempDir()) // or given explicitly
	if got, err := pythonCommand(root); err != nil || got[0] != exe {
		t.Errorf("pythonCommand(root) = %v, %v", got, err)
	}
}

func TestFindProjectIgnoresOtherPythonProjects(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"something-else\"\n"), 0o644)
	t.Chdir(root)
	if got := findProject(); got == root {
		t.Error("took another project's pyproject.toml")
	}
}

func TestPythonCommandFallsBackToPath(t *testing.T) {
	bin := t.TempDir()
	exe := filepath.Join(bin, "flipcom-chat")
	os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", bin)
	t.Chdir(t.TempDir()) // outside any clone: `uv tool install` puts flipcom-chat on the PATH
	if got, err := pythonCommand(""); err != nil || len(got) != 1 || got[0] != exe {
		t.Errorf("pythonCommand(\"\") = %v, %v; want [%s]", got, err, exe)
	}
}

func TestPythonCommandExplainsHowToInstall(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	_, err := pythonCommand("")
	if err == nil || !strings.Contains(err.Error(), "uv tool install") {
		t.Errorf("err = %v, want install instructions", err)
	}
}
