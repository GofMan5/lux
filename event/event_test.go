package event

import "testing"

func TestKindValuesAreUnique(t *testing.T) {
	// Kind is an open-ended classification: models match kinds they know
	// and treat the rest as ordinary updates, which only stays true while
	// every kind has its own value. The lifecycle additions must not
	// collide with any pre-existing kind.
	kinds := []Kind{None, Key, Text, Paste, Mouse, Resize, Focus, Frame, Quit, Error, Suspend, Resume}
	seen := make(map[Kind]bool, len(kinds))
	for _, kind := range kinds {
		if seen[kind] {
			t.Fatalf("kind %d is used by more than one constant", kind)
		}
		seen[kind] = true
	}
}

func TestKeyString(t *testing.T) {
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModCtrl | ModAlt}).String(); got != "ctrl+alt+x" {
		t.Fatalf("unexpected key string %q", got)
	}
	if !KeyPress(KeyEnter, ModShift).Key.Is(KeyEnter, ModShift) {
		t.Fatal("key matching failed")
	}
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModShift | ModSuper}).String(); got != "shift+super+x" {
		t.Fatalf("unexpected super key string %q", got)
	}
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModHyper | ModMeta}).String(); got != "hyper+meta+x" {
		t.Fatalf("unexpected hyper+meta key string %q", got)
	}
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModCtrl | ModCapsLock}).String(); got != "ctrl+caps-lock+x" {
		t.Fatalf("unexpected caps-lock key string %q", got)
	}
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModNumLock}).String(); got != "num-lock+x" {
		t.Fatalf("unexpected num-lock key string %q", got)
	}
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModCtrl, Action: ActionRelease}).String(); got != "released ctrl+x" {
		t.Fatalf("unexpected released key string %q", got)
	}
	if got := (KeyEvent{Code: KeyRune, Rune: 'x', Mod: ModAlt, Action: ActionRepeat}).String(); got != "alt+x" {
		t.Fatal("a repeat must string identically to a press")
	}
	// Named functional keys from the kitty private-use area.
	if got := (KeyEvent{Code: KeyF13}).String(); got != "f13" {
		t.Fatalf("unexpected f13 string %q", got)
	}
	if got := (KeyEvent{Code: KeyMediaPlayPause}).String(); got != "mediaplaypause" {
		t.Fatalf("unexpected media string %q", got)
	}
	if got := (KeyEvent{Code: KeyVolumeMute}).String(); got != "volumemute" {
		t.Fatalf("unexpected volume string %q", got)
	}
	if got := (KeyEvent{Code: KeyNumLock, Mod: ModShift}).String(); got != "shift+numlock" {
		t.Fatalf("unexpected shift+numlock string %q", got)
	}
}

func TestKeyActionZeroIsPress(t *testing.T) {
	// Constructors keep the zero value meaningful: an untyped KeyEvent is a
	// press, and models written before releases existed see the same stream.
	if (KeyEvent{Code: KeyRune, Rune: 'x'}).Action != ActionPress {
		t.Fatal("zero KeyEvent action must be a press")
	}
	press := KeyPress(KeyEnter, 0)
	if press.Key.Action != ActionPress || press.Key.Repeat != 1 {
		t.Fatalf("KeyPress must be a single press, got action %d repeat %d", press.Key.Action, press.Key.Repeat)
	}
	runePress := RuneKey('a', 0)
	if runePress.Key.Action != ActionPress || runePress.Key.Repeat != 1 {
		t.Fatalf("RuneKey must be a single press, got action %d repeat %d", runePress.Key.Action, runePress.Key.Repeat)
	}
}

func TestKeyIsToleratesLockBits(t *testing.T) {
	// Is() matches a subset of modifiers: a ctrl+c binding still fires when
	// the terminal reports caps-lock state alongside ctrl.
	locked := KeyEvent{Code: KeyRune, Rune: 'c', Mod: ModCtrl | ModCapsLock | ModNumLock}
	if !locked.Is(KeyRune, ModCtrl) {
		t.Fatal("caps-lock state must not hide a ctrl binding")
	}
	if locked.Is(KeyRune, ModCtrl|ModShift) {
		t.Fatal("missing shift must not match")
	}
	// A release of the same key is still that key: Is() is identity, not
	// activation. Filtering releases is the run loop's job, not Is()'s.
	release := KeyEvent{Code: KeyRune, Rune: 'c', Mod: ModCtrl, Action: ActionRelease}
	if !release.Is(KeyRune, ModCtrl) {
		t.Fatal("a released key must still match its own binding identity")
	}
}
