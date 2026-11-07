package main

import (
	"slices"
	"testing"
)

func TestSplitLine(t *testing.T) {
	tests := []struct {
		line, author, text string
		kind               kind
	}{
		{"R1kori7h: Hello", "R1kori7h", "Hello", chatLine},
		{"R1kori7h: time: 12:00", "R1kori7h", "time: 12:00", chatLine},
		{"R1kori7h joined chat.", "R1kori7h", "joined chat.", joinLine},
		{"R1kori7h left chat.", "R1kori7h", "left chat.", joinLine},
		{"Zorkly: I joined chat.", "Zorkly", "I joined chat.", chatLine},
		{"no colon here", "", "no colon here", chatLine},
		{"two words: text", "", "two words: text", chatLine},
	}
	for _, tt := range tests {
		author, text, k := splitLine(tt.line)
		if author != tt.author || text != tt.text || k != tt.kind {
			t.Errorf("splitLine(%q) = %q, %q, %v; want %q, %q, %v", tt.line, author, text, k, tt.author, tt.text, tt.kind)
		}
	}
}

func TestArgvSender(t *testing.T) {
	cfg := config{freqMHz: 433.92, port: "/dev/x", device: 1, python: []string{"py", "-m"}}
	want := []string{"py", "-m", "send", "--json", "-f", "433920000", "--device", "1", "--port", "/dev/x"}
	if got := cfg.argv(sender); !slices.Equal(got, want) {
		t.Errorf("argv(sender) = %v, want %v", got, want)
	}
}

func TestArgvListener(t *testing.T) {
	cfg := config{freqMHz: 315, gain: "20", python: []string{"flipcom-chat"}}
	want := []string{"flipcom-chat", "listen", "--json", "-f", "315000000", "-g", "20"}
	if got := cfg.argv(listener); !slices.Equal(got, want) {
		t.Errorf("argv(listener) = %v, want %v", got, want)
	}
	cfg.file = "cap.cu8"
	if got := cfg.argv(listener); !slices.Contains(got, "--realtime") || !slices.Contains(got, "cap.cu8") {
		t.Errorf("argv(listener) with a file = %v, want --file cap.cu8 --realtime", got)
	}
}

func TestArgvDoesNotShareThePythonSlice(t *testing.T) {
	py := make([]string, 1, 10)
	py[0] = "flipcom-chat"
	cfg := config{freqMHz: 433.92, python: py}
	a, b := cfg.argv(sender), cfg.argv(listener)
	if a[1] != "send" || b[1] != "listen" {
		t.Errorf("argv calls overwrote each other: %v / %v", a, b)
	}
}
