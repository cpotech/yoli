# Yoli TUI Feature

This document describes the Text User Interface (TUI) for Yoli, including interactive features like prompt history and cursor movement.

## Features

### Interactive REPL

Run `yoli tui` to start an interactive REPL session with the following features:

- **Prompt History (↑/↓ arrows)**: Navigate through previously entered prompts
  - Press **Up arrow** to cycle through older prompts
  - Press **Down arrow** to cycle back to newer prompts or empty new prompt
  - History is preserved across the session (up to 1000 entries)
  - Duplicate consecutive entries are not added to history

- **Cursor Movement (←/→ arrows)**: Move the cursor within the current prompt
  - Press **Left arrow** to move cursor backward
  - Press **Right arrow** to move cursor forward
  - Allows editing in the middle of the prompt text

- **Line Editing**:
  - **Backspace**: Delete character before cursor
  - **Ctrl-C**: Clear the current input line (without quitting)
  - **Ctrl-D**: Exit the REPL when line is empty (or Ctrl-D at EOF)
  - **Enter**: Submit the prompt

### Slash Commands

Available commands in TUI mode:
- `/help` - Show available commands
- `/model [slug]` - Show or switch the AI model
- `/context` - Show estimated context size
- `/clear` - Start a new session
- `/exit`, `/quit` - Leave the REPL

## Implementation Details

### Terminal Raw Mode

The TUI uses `golang.org/x/term` to put the terminal into raw mode, which:
- Disables canonical mode (line buffering)
- Disables echo (characters are printed manually)
- Allows reading individual keystrokes including arrow keys

Terminal settings are restored when the TUI exits.

### Line Editor (`tuiLineEditor`)

The `tuiLineEditor` struct in `internal/cli/tui_cmd.go` provides:
- History management (`history`, `histIdx`)
- Cursor positioning (`prompt`, `cursor`)
- Terminal state management (`origTerm`)

Key methods:
- `readLine()` - Main loop reading and processing keystrokes
- `redrawLine()` - Redraws the current line with cursor positioning
- `historyUp()` / `historyDown()` - Navigate prompt history
- `addToHistory()` - Add prompts to history (with deduplication)

### Escape Sequence Handling

Arrow keys send ANSI escape sequences:
- Up: `ESC [ A`
- Down: `ESC [ B`
- Right: `ESC [ C`
- Left: `ESC [ D`

The editor reads these sequences byte-by-byte after detecting the initial `ESC` (byte 27).

## Usage

```bash
# Start TUI mode
yoli tui

# With session options
yoli tui --no-session

# With debug logging
yoli tui --loglevel debug
```

## Testing

The TUI has been tested for:
- Basic prompt entry and submission
- History navigation with up/down arrows
- Cursor movement with left/right arrows
- Backspace functionality
- Ctrl-C to clear line
- Ctrl-D to exit
- Slash command processing

## Future Enhancements

Potential improvements:
- [ ] Persistent history across sessions (save to file)
- [ ] Reverse history search (Ctrl-R)
- [ ] Word navigation (Alt-Left, Alt-Right)
- [ ] Word deletion (Ctrl-W)
- [ ] Kill to beginning/end of line (Ctrl-U, Ctrl-K)
- [ ] Multi-line input support
- [ ] Syntax highlighting for prompts
