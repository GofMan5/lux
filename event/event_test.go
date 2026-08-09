package event

import "testing"

func TestKeyString(t *testing.T) {
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModCtrl | ModAlt}).String(); got != "ctrl+alt+x" {
		t.Fatalf("unexpected key string %q", got)
	}
	if !KeyPress(KeyEnter, ModShift).Key.Is(KeyEnter, ModShift) {
		t.Fatal("key matching failed")
	}
}
