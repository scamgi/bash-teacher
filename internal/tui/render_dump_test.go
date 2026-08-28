package tui

import (
	"os"
	"testing"
)

// TestDumpFrames is a development aid: BT_DUMP=1 go test ./internal/tui -run DumpFrames
func TestDumpFrames(t *testing.T) {
	if os.Getenv("BT_DUMP") == "" {
		t.Skip("set BT_DUMP=1 to print frames")
	}
	a := newTestApp(t, 96, 28)
	for _, s := range []Screen{ScreenHome, ScreenDictionary, ScreenPractice, ScreenFlashcards, ScreenStats} {
		a.current = s
		t.Logf("\n=== %v ===\n%s", s, view(a))
	}
}
