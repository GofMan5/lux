# Manual terminal checklist

Automated tests pin the byte streams; this checklist pins what a person sees
and feels in a real terminal window. Run it before every release and after any
change to input decoding, presentation, or terminal setup. Each new visible
feature adds one to three rows here.

## How to run

1. Build the binaries once from the repository root, so a `go run` parent
   process never sits between the terminal and the app:

   ```sh
   go build -o bin/ ./examples/16-studio ./examples/01-counter ./examples/03-textedit \
       ./examples/04-table ./examples/10-form ./examples/11-select ./examples/13-logs
   ```

2. Open the terminal under test at 120×36 or larger (the studio needs at
   least 96×28) and start `bin/16-studio` (`bin\16-studio.exe` on Windows).
3. Note the terminal name and version, the OS, the shell, the font, and
   `git rev-parse --short HEAD` in the run log at the bottom.
4. Walk the rows in order. Mark each terminal column with `✓` (behaves as
   expected), `✗` (does not), or `–` (not applicable there, with the reason in
   the run log).
5. Every `✗` gets a finding: terminal, steps, expected, actual. Findings are
   tracked in `docs/MASTER-PLAN.md` under "Найдено при ручной проверке" and
   each one ends with a regression test and a fix commit.

Columns: **WT** Windows Terminal · **CH** conhost (`cmd.exe` in the classic
console window) · **VS** VS Code integrated terminal · **LX** a Linux terminal
(local or over SSH) · **TX** tmux on Linux.

The studio's **Settings** page (press `5`) has a live event inspector: every
decoded key, text, paste, mouse, resize, and focus event appears there as one
row. Keyboard rows below that say "the inspector shows" are checked on that
page. Keys `q`, `t`, `d`, `1`–`5`, and `?` are bound by the studio itself, so
avoid them while inspecting.

## Known by design

These are current, documented behaviors, not findings:

- Windows has no job control: `Ctrl+Z` reaches the app (undo in text fields)
  instead of suspending it.
- Windows Terminal and conhost keep the program's window title after exit;
  they have no title stack to restore from.
- Right and middle clicks do nothing; there is no context menu yet.
- No control draws a scrollbar yet, so there is no thumb to drag (`[LAY-11]`
  adds a drag row here when it ships).
- tmux drops taskbar progress and desktop notifications.
- OSC 52 clipboard writes larger than 32 KiB are dropped; the app never reads
  the clipboard.

## Start and exit

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 1 | Start `16-studio`. | The full UI appears without a visible delay (under ~0.2 s). No probe replies such as `^[[?1;2c` or `[?62c` flash on screen or remain in the shell afterwards. | | | | | |
| 2 | Press `q`. | The app exits at once. The shell prompt is where it was before the start, the screen content from before is back, the cursor is visible, typing echoes normally. | | | | | |
| 3 | Start again, move the mouse over the window, then quit with `Ctrl+Q`. | Same as row 2. After exit, moving the mouse and scrolling print nothing; no `[<35;…M` or `[67;0;3;0;8;1_` garbage appears at the prompt. | | | | | |
| 4 | Start, go to **Motion** (`4`) so the 3D scene animates, press `Ctrl+C`. | Exits immediately during the animation; terminal restored as in row 2. | | | | | |
| 5 | Start a deploy (`1`, then `d`) and quit with `q` while it runs. | Exits; the taskbar/tab progress indicator disappears; no spinner glyphs are left on screen. | | | | | |
| 6 | Unix only: press `Ctrl+Z`, then run `fg`. | `Ctrl+Z` returns to the shell with a sane, cooked terminal. `fg` restores the whole UI on the same page with the same state; keys and mouse work again. | – | – | | | |
| 7 | Close the terminal window or tab while the studio runs (on Linux/tmux: kill the pane). | The window closes without hanging; the studio process is gone (`tasklist`/`ps`). | | | | | |

## Mouse

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 8 | Sweep the pointer across the whole window. | Nothing is printed. Buttons under the pointer switch to their hover color and lose it when the pointer leaves. | | | | | |
| 9 | Click each tab in the header strip. | The page switches on each click; the strip does not shift or jump. | | | | | |
| 10 | Click **Deploy** on the Dashboard; click it again (now **Cancel**). | One click starts exactly one deploy; the second cancels it ("cancelled at step N"). | | | | | |
| 11 | Press on **Deploy**, drag the pointer off the button, release. | Nothing happens: a click completes only when released on the same control. | | | | | |
| 12 | On **Settings**, click both checkboxes, each theme radio row, and open the sample-rate select and pick a value. | Each click toggles or selects exactly once; the select popup opens, the clicked option commits, the popup closes. | | | | | |
| 13 | Wheel over the Dashboard **Activity** list, the **Services** table, the **Files** tree, the editor, and the Settings event inspector. | Only the panel under the pointer scrolls. The table wheel moves the viewport and keeps the selected row. At either end the panel stops; nothing else moves. | | | | | |
| 14 | In **Services**, type a 60-character filter, then press in the field and drag the pointer past its right edge. | The selection grows and the field scrolls horizontally while the pointer moves beyond the edge. | | | | | |
| 15 | In **Files**, open `README.md`, press in the editor and drag down past its bottom edge. | The selection grows line by line and the editor scrolls while the pointer moves below the edge. | | | | | |
| 16 | On **Motion**, press on the spring track, drag left and right, release. | The target marker (`▼`) follows the pointer during the drag and the ball springs toward it with momentum. | | | | | |
| 17 | Open the palette (`Ctrl+P`) and click outside it. Repeat with help (`F1`) and the restart confirmation (**Services** → **Restart…**). | Each overlay closes on the outside click. The click does not reach the control underneath. | | | | | |
| 18 | Right-click and middle-click anywhere. | Nothing happens in the app and nothing is printed; the inspector shows `mouse press right` / `mouse press middle`. | | | | | |

## Keyboard

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 19 | On each page press `Tab` repeatedly, then `Shift+Tab`. | Focus visits every control once in reading order and wraps around; the focused control is always visibly marked. | | | | | |
| 20 | Use `↑`/`↓` in the Activity list, the Services table, the Files tree, and the theme radio group. | The selection moves one row per press and the view follows it. | | | | | |
| 21 | In the Services table press `Home`, `End`, `PgDn`, `PgUp`. | First row, last row, one page down, one page up. | | | | | |
| 22 | Focus **Deploy** with `Tab`, press `Enter`; focus **Copy text** on Settings, press `Space`. | Each key activates the focused control exactly once. | | | | | |
| 23 | Open the palette, help, and the restart confirmation in turn and press `Esc` in each; then press `Esc` on a page with nothing open. | `Esc` closes the topmost overlay only. With nothing open the app keeps running (in the filter or the editor `Esc` only moves focus out of the field). | | | | | |
| 24 | Press `Ctrl+P`, type `gtf`, press `Enter`. | The palette opens, "Go to Files" is the first match, `Enter` runs it and the palette closes. `↑`/`↓` move through matches. | | | | | |
| 25 | Switch the keyboard layout to Russian. Press `Ctrl+P` (the `З` key), then `Esc`, then `Ctrl+Q` (the `Й` key). | Both shortcuts work exactly as in the English layout. | | | | | |
| 26 | On Settings, press `Alt+X`; then press `Esc` alone. | The inspector shows `key alt+x`, not `escape` followed by `x`. A lone `Esc` shows `key escape` with no noticeable delay. | | | | | |
| 27 | On Settings, press `F2` through `F12` (skip keys the terminal reserves, such as F11 fullscreen). `F1` separately. | Each F key appears once as `key f2` … `key f12`. `F1` opens help. | | | | | |
| 28 | In the Services filter type `alpha beta gamma`, press `Ctrl+←` twice, `Ctrl+→` once, `Ctrl+Backspace`. | The caret jumps by whole words; `Ctrl+Backspace` deletes one word. | | | | | |
| 29 | In the Files editor type a word, then undo and redo: `Ctrl+Z` (Windows) or `Alt+U` (Unix, where `Ctrl+Z` suspends), then `Ctrl+Y`, then `Ctrl+Shift+Z`. | Undo removes the typing; both redo bindings restore it. | | | | | |
| 30 | In the Files editor select text and press `Ctrl+C`; outside a text field press `Ctrl+C`. | In the editor `Ctrl+C` copies (toast "Copied N bytes…") and the app keeps running; elsewhere it quits. | | | | | |
| 31 | Type `q` into the Services filter, the palette query, and the editor. | `q` is inserted as text each time; the app does not quit. | | | | | |
| 32 | Hold `↓` in the Services table for 10 s, then release. | Movement is smooth and keeps up; it stops the instant the key is released, with no coasting. | | | | | |

## Text input and paste

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 33 | Type Latin, Cyrillic (`привет`), an emoji (`😀`), and CJK (`漢字`) into the Services filter. | Each character appears once, wide characters take two cells, the caret sits right after the last character. | | | | | |
| 34 | Type `e` followed by a combining acute (`U+0301`), or paste `é` written that way; press `←` once. | One visible `é`; a single `←` steps over the whole character. | | | | | |
| 35 | Paste a short single line into the Services filter; then paste it once on Settings. | The text lands once in the filter and nothing runs on its own. On Settings the inspector shows a single `paste N bytes` row. | | | | | |
| 36 | Paste three lines into the Services filter. | They join into one line with spaces; no `Enter` is acted on. | | | | | |
| 37 | Paste `a😀b` into the Services filter and into the editor (on Windows especially). | Three characters `a`, `😀`, `b` — no unknown-key events and no missing emoji. | | | | | |
| 38 | Paste 1000 lines into the Files editor. | The paste completes in under a second, the `Ln … lines` status updates, scrolling and typing stay responsive. | | | | | |
| 39 | Paste about 1 MB of text into the Files editor. | The UI does not freeze; memory stays bounded; the app remains usable. | | | | | |
| 40 | Type 80 characters into the Services filter; press `Home`, then `End`. | The field scrolls horizontally, the caret is always visible, `Home`/`End` jump to the ends. | | | | | |
| 41 | Select text in the editor, `Ctrl+C`, paste into another application. | The pasted text matches the selection (where the terminal allows OSC 52 clipboard writes). | | | | | |
| 42 | With a CJK IME enabled, type into the Services filter. | The IME candidate window opens at the caret, and committed text appears once. | | | | | |

## Window size

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 43 | Resize the window slowly in both directions on every page. | Each redraw is clean: no stale cells, broken borders, or doubled text. | | | | | |
| 44 | Resize quickly back and forth for 10 s. | No garbage, no crash; the final frame matches the final size. | | | | | |
| 45 | Shrink below 96×28. | The UI is replaced by `LUX Studio needs 96x28, this terminal is W×H` with the live size. | | | | | |
| 46 | Shrink to the smallest the terminal allows (1–5 columns or rows), then restore. | No crash; on restore the same page, focus, and data return. | | | | | |
| 47 | Maximize, enter fullscreen (`F11` / `Alt+Enter` where available), and leave it. | The UI fills the new size immediately each time. | | | | | |
| 48 | Open the palette and resize. | The palette stays centered and keeps working; the query is kept. | | | | | |
| 49 | Start a text drag selection in the editor and resize while the button is held; release. | No crash; the selection ends at a sane position. | | | | | |
| 50 | Start a deploy and resize while it runs. | The pipeline keeps progressing; percentages and gauges continue from where they were. | | | | | |

## Rendering

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 51 | Check Settings → CAPABILITIES. | The terminal name and color depth match the terminal (truecolor in modern terminals); colors look as designed, not oversaturated. | | | | | |
| 52 | Inspect panel borders on every page. | Rounded corners and straight edges, no gaps or offset segments. | | | | | |
| 53 | Open `README.md` in Files (`漢字とカナ`, `🚀✨`, combining `é`). | Wide characters and emoji take their width; the panel's right border stays straight; wrapping never splits a character. | | | | | |
| 54 | On Services, Ctrl+click the URL in the details pane. | It opens in the browser where OSC 8 is supported; elsewhere it is plain underlined text with no escape garbage. | | | | | |
| 55 | Look at the window or tab title while running, then quit. | The title reads `LUX Studio`. After exit the previous title returns where the terminal keeps a title stack; Windows Terminal and conhost keep the program title (see `docs/COMPATIBILITY.md`). | | | | | |
| 56 | Run a deploy to completion. | Where supported (Windows Terminal), tab/taskbar progress rises to 100% and disappears at the end; nowhere does stray text appear. | | | | | |
| 57 | Let a deploy finish, and press **Notify** on Settings. | A desktop notification appears where the terminal supports OSC 9/777; elsewhere nothing happens and nothing is printed. | | | | | |
| 58 | Press `t` three times on each page (with no text field focused). | Aurora → Ember → Matrix → Aurora; every cell repaints, no leftover cells in the old palette. | | | | | |
| 59 | Tick Settings → **reduce motion**, then visit Dashboard and Motion. | Springs land without animating, the 3D scene and spinners hold still, and on Motion the header frame counter stops. | | | | | |
| 60 | Click **Copy text** and **Copy URL**, then paste elsewhere. | The copied text arrives where OSC 52 writes are allowed; nothing is printed on screen. | | | | | |

## Load

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 61 | Leave the studio on Settings for 10 minutes. | CPU stays at about 0% (Task Manager or `top`); the header frame counter does not advance. | | | | | |
| 62 | Leave it on Motion for 1 minute while the scene animates. | Memory stays flat; the animation stays smooth. | | | | | |
| 63 | Move the mouse rapidly over the Settings page for 30 s, then stop. | The inspector keeps one updating `mouse move` row; the screen catches up within ~0.1 s of stopping. | | | | | |
| 64 | Start and cancel a deploy with `d` 20 times quickly. | Every press toggles once; no spinner stays stuck, the final state is consistent. | | | | | |

## Environment

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 65 | Run the studio inside tmux and repeat rows 2, 8, 13, 26, 33, 43. | Same results as outside tmux. | – | – | – | – | |
| 66 | Run the studio over SSH and repeat rows 2, 26, 43. | Same results; `Esc` stays responsive over the network. | | | | | |
| 67 | Run `bin/01-counter \| cat` (PowerShell: `bin\01-counter.exe \| Out-String`). | One plain text frame, no escape bytes, and the process exits by itself. | | | | | |
| 68 | Run `TERM=dumb bin/01-counter` (Unix). | One plain text frame, no escape bytes, clean exit. | – | – | | | |
| 69 | Run the studio with `NO_COLOR=1`. | Settings shows `color mono`; the UI is still readable and focus is still visible without color. | | | | | |

## Other examples

| # | Do | Expect | WT | CH | VS | LX | TX |
|---|---|---|---|---|---|---|---|
| 70 | `03-textedit`: click into the text, press `Ctrl+W`, then resize narrow and wide. | Wrapping toggles; the caret stays on the same character across resizes. | | | | | |
| 71 | `04-table`: hold `PgDn`, then click a header twice. | 200k rows scroll without lag; the column sorts ascending, then descending. | | | | | |
| 72 | `10-form`: fill in only the name and press `Ctrl+S`; then press `Esc`. | Focus jumps to the host field with "host is required" shown; `Esc` over the unsaved draft asks before quitting. | | | | | |
| 73 | `11-select`: open the bottom select. | Its popup flips above the anchor; `Esc` or an outside click reverts and closes it. | | | | | |
| 74 | `13-logs`: press `↑`, wait, then press `End`. | `↑` pauses follow and the view holds still while lines arrive; `End` resumes following the tail. | | | | | |
| 75 | `13-logs`: press `/`, type a word, `Enter`. | Only matching lines remain; the filter applies incrementally while typing. | | | | | |

## Run log

| Date | Terminal and version | OS / shell / font | Commit | Result | Findings |
|---|---|---|---|---|---|
| | | | | | |
