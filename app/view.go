// Everything drawn on screen.
package main

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	accent    = lipgloss.AdaptiveColor{Light: "#D35400", Dark: "#FF8A3D"} // Flipper orange
	dim       = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6C6C6C"}
	textCol   = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#E4E4E4"}
	green     = lipgloss.AdaptiveColor{Light: "#1E8E3E", Dark: "#5FD787"}
	yellow    = lipgloss.AdaptiveColor{Light: "#B7791F", Dark: "#F2C94C"}
	red       = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#FF6B6B"}
	titleSty  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1A1A1A")).Background(accent).Padding(0, 1)
	dimStyle  = lipgloss.NewStyle().Foreground(dim)
	errStyle  = lipgloss.NewStyle().Foreground(red)
	textStyle = lipgloss.NewStyle().Foreground(textCol)
	boldStyle = lipgloss.NewStyle().Bold(true).Foreground(textCol)
	accStyle  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	palette   = []lipgloss.AdaptiveColor{
		{Light: "#1565C0", Dark: "#6CB6FF"},
		{Light: "#6A1B9A", Dark: "#C792EA"},
		{Light: "#00838F", Dark: "#4DD0E1"},
		{Light: "#AD1457", Dark: "#F48FB1"},
		{Light: "#2E7D32", Dark: "#A5D6A7"},
	}
)

func authorColor(name string) lipgloss.AdaptiveColor {
	h := fnv.New32a()
	h.Write([]byte(name))
	return palette[h.Sum32()%uint32(len(palette))]
}

func (m model) View() string {
	if !m.sized {
		return ""
	}
	if m.screen == setupScreen {
		return m.setupView()
	}
	return m.header() + "\n" + m.vp.View() + "\n" + m.footer()
}

func (m model) setupView() string {
	opt := func(r role, key, name, what, needs string) string {
		cursor, nameSty := "  ", boldStyle
		if m.choice == r && !m.editFreq {
			cursor, nameSty = accStyle.Render("› "), accStyle
		}
		return cursor + dimStyle.Render(key+"  ") + nameSty.Render(name) + "\n" +
			"     " + textStyle.Render(what) + "\n" +
			"     " + dimStyle.Render(needs)
	}
	freqLabel := boldStyle.Render("Frequency  ")
	freq := textStyle.Render(m.freqInput.Value()) + dimStyle.Render(" MHz")
	if m.editFreq {
		freq = m.freqInput.View() + dimStyle.Render(" MHz")
	}
	freqHint := dimStyle.Render("   tab to change · both sides must use the same one")
	if m.editFreq {
		freqLabel = accStyle.Render("Frequency  ")
		freqHint = dimStyle.Render("   enter to start · esc when done")
	}
	body := strings.Join([]string{
		titleSty.Render("flipchat") + "  " + dimStyle.Render("Flipper Zero Sub-GHz chat, read with an RTL-SDR"),
		"",
		boldStyle.Render("What's plugged into this computer?"),
		"",
		opt(sender, "1", "Sender · Flipper Zero", "Type messages; the Flipper transmits them.",
			"Flipper on USB, qFlipper closed."),
		"",
		opt(listener, "2", "Listener · RTL-SDR dongle", "Read the messages sent by Flippers nearby.",
			"RTL-SDR dongle on USB, antenna attached."),
		"",
		freqLabel + freq + freqHint,
	}, "\n")
	if m.setupErr != "" {
		body += "\n\n" + errStyle.Render(m.setupErr)
	}
	help := dimStyle.Render("↑↓ choose · enter start · tab frequency · q quit")
	box := lipgloss.NewStyle().Padding(1, 2).Render(body)
	gap := m.height - lipgloss.Height(box) - 1
	return box + strings.Repeat("\n", max(1, gap)) + " " + help
}

func (m model) header() string {
	left := titleSty.Render("flipchat") + " " + boldStyle.Render(m.role.String()) +
		textStyle.Render(fmt.Sprintf(" · %.3f MHz", m.cfg.freqMHz))
	var sym, word string
	var c lipgloss.AdaptiveColor
	switch m.state {
	case starting:
		sym, c, word = "◌", yellow, "connecting…"
	case up:
		sym, c, word = "●", green, m.detail
	case down:
		sym, c, word = "✕", red, "disconnected"
	case finished:
		sym, c, word = "○", dim, m.detail
	}
	right := lipgloss.NewStyle().Foreground(c).Render(sym) + " " + m.deviceName() + " " + dimStyle.Render(word)
	if m.role == listener {
		n := fmt.Sprintf("%d messages   ", m.received)
		if m.received == 1 {
			n = "1 message   "
		}
		right = dimStyle.Render(n) + right
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) footer() string {
	var help string
	switch {
	case m.stopping:
		help = "closing…"
	case m.role == sender:
		help = "enter send · ↑↓ scroll · ctrl+r reconnect · esc change role · ctrl+c quit"
	default:
		help = "↑↓ scroll · ctrl+r reconnect · esc change role · q quit"
	}
	help = dimStyle.Render(" " + help)
	if m.role == listener {
		return help
	}
	in := m.input
	switch m.state {
	case up:
		in.Placeholder = "Type a message and press Enter"
	case down:
		in.Placeholder = "Flipper not connected — ctrl+r to retry"
	default:
		in.Placeholder = "Connecting to the Flipper…"
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).
		Width(max(1, m.width-2)).Render(in.View())
	return box + "\n" + help
}

func (m model) render() string {
	w := max(20, m.width)
	var b strings.Builder
	for i, e := range m.entries {
		if i > 0 {
			b.WriteString("\n")
		}
		stamp := dimStyle.Render(e.at.Format("15:04:05")) + "  "
		switch e.kind {
		case infoLine, errorLine:
			sty := dimStyle
			if e.kind == errorLine {
				sty = errStyle
			}
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, stamp, sty.Width(w-10).Render(e.text)))
			continue
		case joinLine:
			body := dimStyle.Italic(true).Width(w - 10).Render("→ " + e.author + " " + e.text)
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, stamp, body))
			continue
		}
		author := e.author
		if author == "" {
			author = "?"
		}
		c := authorColor(author)
		if e.mine {
			c = accent
		}
		name := lipgloss.NewStyle().Bold(true).Foreground(c).Render(author)
		name = lipgloss.NewStyle().Width(max(10, lipgloss.Width(name)+1)).Render(name)
		body := textStyle.Render(e.text)
		if e.mine {
			if e.sent {
				body += "  " + dimStyle.Render("sent")
			} else {
				body += "  " + dimStyle.Render("sending…")
			}
		}
		bodyW := max(10, w-10-lipgloss.Width(name))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, stamp, name, lipgloss.NewStyle().Width(bodyW).Render(body)))
	}
	if m.role == listener && m.received == 0 && m.state == up {
		b.WriteString("\n\n" + dimStyle.Render("          Waiting for messages… (on the other computer: flipchat → Sender)"))
	}
	return b.String()
}
