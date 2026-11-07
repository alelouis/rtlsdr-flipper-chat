package main

// Runs the Python `flipcom-chat listen|send --json` processes and turns their
// output into Bubble Tea messages.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type source int

const (
	sdr source = iota
	flipper
)

func (s source) String() string {
	if s == sdr {
		return "RTL-SDR"
	}
	return "Flipper"
}

// event is one JSON line from the Python side.
type event struct {
	Event  string  `json:"event"` // listening, line (RTL-SDR); ready, sent, heard (Flipper)
	Text   string  `json:"text"`
	Name   string  `json:"name"`
	Freq   float64 `json:"freq"`
	Source string  `json:"source"`
	Port   string  `json:"port"`
}

type eventMsg struct {
	src source
	gen int
	ev  event
}

type stderrMsg struct {
	src  source
	gen  int
	line string
}

type exitMsg struct {
	src source
	gen int
	err error
}

// backend is one running Python process. gen tells a restarted process's
// messages apart from those of the one it replaced.
type backend struct {
	src   source
	gen   int
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
}

func startBackend(src source, gen int, argv []string, out chan<- tea.Msg) (*backend, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // so stop can kill its children too
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	var stdin io.WriteCloser
	if src == flipper {
		if stdin, err = cmd.StdinPipe(); err != nil {
			return nil, err
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &backend{src: src, gen: gen, cmd: cmd, stdin: stdin, done: make(chan struct{})}

	readers := make(chan struct{}, 2)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var ev event
			if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Event != "" {
				out <- eventMsg{src, gen, ev}
			} else if line := strings.TrimSpace(sc.Text()); line != "" {
				out <- stderrMsg{src, gen, line}
			}
		}
		readers <- struct{}{}
	}()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				out <- stderrMsg{src, gen, line}
			}
		}
		readers <- struct{}{}
	}()
	go func() {
		<-readers // read everything before Wait closes the pipes
		<-readers
		err := cmd.Wait()
		close(b.done)
		out <- exitMsg{src, gen, err}
	}()
	return b, nil
}

func (b *backend) send(line string) error {
	if b == nil || b.stdin == nil {
		return errors.New("not connected")
	}
	_, err := io.WriteString(b.stdin, line+"\n")
	return err
}

// stop asks the process to finish cleanly (end of input for `send`, Ctrl+C for
// `listen`) and kills it if it hasn't after the timeout.
func (b *backend) stop(timeout time.Duration) {
	if b == nil {
		return
	}
	select {
	case <-b.done:
		return
	default:
	}
	if b.stdin != nil {
		b.stdin.Close() // `send` leaves the chat on end of input
	} else {
		b.cmd.Process.Signal(syscall.SIGINT)
	}
	select {
	case <-b.done:
	case <-time.After(timeout):
		syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL) // the whole process group
		<-b.done
	}
}

// pythonCommand finds how to run the Python `flipcom-chat`: the project's
// .venv first, then `uv run`, then anything on PATH.
func pythonCommand(project string) ([]string, error) {
	if project == "" {
		project = findProject()
	}
	if project != "" {
		exe := filepath.Join(project, ".venv", "bin", "flipcom-chat")
		if _, err := os.Stat(exe); err == nil {
			return []string{exe}, nil
		}
		if uv, err := exec.LookPath("uv"); err == nil {
			return []string{uv, "run", "--quiet", "--project", project, "flipcom-chat"}, nil
		}
	}
	if exe, err := exec.LookPath("flipcom-chat"); err == nil {
		return []string{exe}, nil
	}
	return nil, fmt.Errorf("can't find flipcom-chat, the Python side of flipchat. Install it with:\n\n" +
		"    uv tool install git+https://github.com/alelouis/rtlsdr-flipper-chat\n\n" +
		"or, in a clone of the repo, run `uv sync` (or pass -project /path/to/the/clone)")
}

// findProject looks for this repo's pyproject.toml above the working directory
// and above the executable.
func findProject() string {
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			starts = append(starts, filepath.Dir(exe))
		}
	}
	for _, dir := range starts {
		for {
			data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
			if err == nil && strings.Contains(string(data), `name = "flipcom-chat"`) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}
