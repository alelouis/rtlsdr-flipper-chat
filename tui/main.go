// flipchat: a terminal app for the Flipper Zero's Sub-GHz chat.
//
// Each computer takes one role: the sender types through a Flipper Zero, the
// listener reads the chat with an RTL-SDR dongle. The app drives the Python
// `flipcom-chat send` or `flipcom-chat listen` in the background.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	var cfg config
	var project, roleFlag string
	flag.StringVar(&roleFlag, "role", "", "skip the setup screen: sender (Flipper) or listener (RTL-SDR)")
	flag.Float64Var(&cfg.freqMHz, "f", 433.92, "frequency in MHz")
	flag.StringVar(&cfg.gain, "g", "", "listener: RTL-SDR gain in dB (default: automatic)")
	flag.StringVar(&cfg.file, "file", "", "listener: replay a .cu8 recording instead of the live RTL-SDR")
	flag.StringVar(&cfg.port, "port", "", "sender: Flipper serial port (default: autodetect)")
	flag.IntVar(&cfg.device, "device", 0, "sender: Flipper radio, 0 = internal, 1 = external module")
	flag.StringVar(&project, "project", "", "path to the rtlsdr-flipper-chat folder (default: found automatically)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "flipchat: Flipper Zero Sub-GHz chat. One computer sends with a Flipper,\n"+
			"another listens with an RTL-SDR.\n\nUsage: flipchat [options]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var preset *role
	switch strings.ToLower(roleFlag) {
	case "":
	case "sender", "send", "flipper":
		r := sender
		preset = &r
	case "listener", "listen", "rtl-sdr", "sdr":
		r := listener
		preset = &r
	default:
		fmt.Fprintf(os.Stderr, "flipchat: -role must be sender or listener, not %q\n", roleFlag)
		os.Exit(2)
	}
	if cfg.file != "" && preset == nil {
		r := listener
		preset = &r
	}

	py, err := pythonCommand(project)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flipchat:", err)
		os.Exit(1)
	}
	cfg.python = py

	if _, err := tea.NewProgram(newModel(cfg, preset, startBackend), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "flipchat:", err)
		os.Exit(1)
	}
}
