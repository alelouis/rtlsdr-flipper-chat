// Chat lines as the screens show them.
package main

import (
	"strings"
	"time"
)

type kind int

const (
	chatLine kind = iota
	joinLine      // "<name> joined chat." / "<name> left chat."
	infoLine
	errorLine
)

type entry struct {
	at     time.Time
	kind   kind
	author string
	text   string
	mine   bool
	sent   bool // mine: the Flipper has transmitted it
}

// splitLine turns "name: text" into its parts; join/leave lines have no colon.
func splitLine(line string) (author, text string, k kind) {
	for _, suffix := range []string{" joined chat.", " left chat."} {
		if strings.HasSuffix(line, suffix) && !strings.Contains(line, ": ") {
			return strings.TrimSuffix(line, suffix), strings.TrimPrefix(suffix, " "), joinLine
		}
	}
	if i := strings.Index(line, ": "); i > 0 && !strings.Contains(line[:i], " ") {
		return line[:i], line[i+2:], chatLine
	}
	return "", line, chatLine
}
