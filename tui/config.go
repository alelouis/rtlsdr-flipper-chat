// Roles and the command line each one runs.
package main

import "strconv"

type role int

const (
	sender role = iota
	listener
)

func (r role) source() source {
	if r == sender {
		return flipper
	}
	return sdr
}

func (r role) String() string {
	if r == sender {
		return "Sender"
	}
	return "Listener"
}

type config struct {
	freqMHz float64
	gain    string
	port    string
	device  int
	file    string
	python  []string
}

func (c config) argv(r role) []string {
	a := append([]string{}, c.python...)
	hz := strconv.FormatFloat(c.freqMHz*1e6, 'f', 0, 64)
	if r == sender {
		a = append(a, "send", "--json", "-f", hz, "--device", strconv.Itoa(c.device))
		if c.port != "" {
			a = append(a, "--port", c.port)
		}
		return a
	}
	a = append(a, "listen", "--json", "-f", hz)
	if c.gain != "" {
		a = append(a, "-g", c.gain)
	}
	if c.file != "" {
		a = append(a, "--file", c.file, "--realtime")
	}
	return a
}
