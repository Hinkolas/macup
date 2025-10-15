# TUI Package

A simple terminal user interface package for displaying progress bars during backup operations.

## Features

- Multiple progress bars for different locations
- Braille characters (⣿) for visual progress
- ETA (Estimated Time of Arrival) calculation and display
- Color-coded status messages (green DONE ✔, ETA, etc.)
- Current file being written display
- Thread-safe updates
- Automatic terminal detection

## Usage

```go
import "github.com/hinkolas/macup/internal/tui"

// Create a new progress view
pv := tui.NewProgressView()

// Add a new progress bar
pv.Add("~/github", 0.0, 0)
pv.Add("~/playground", 0.0, 0)

// Update progress
pv.Set("~/github", 0.5, 30*time.Second) // 50% done, 30s remaining

// Set status message (e.g., current file being processed)
pv.Message("~/github/myproject/file.txt")

// Mark as complete
pv.Done("~/github", true)

// Finish and show success message (keeps final state on screen)
pv.Finish("✓ Backup successfully stored at ./backup")

// Or use Clear() only for error cases
// pv.Clear()

// Check if running in a terminal
if tui.IsTerminal() {
    // Use TUI
} else {
    // Fall back to plain logs
}
```

## API

### `NewProgressView() *ProgressView`
Creates a new progress view instance.

### `Add(location string, progress float64, eta time.Duration)`
Adds a new progress bar for a location.
- `location`: Path to the location being backed up
- `progress`: Initial progress (0.0 to 1.0)
- `eta`: Estimated time remaining

### `Set(location string, progress float64, eta time.Duration)`
Updates an existing progress bar.
- `location`: Path to the location
- `progress`: Current progress (0.0 to 1.0)
- `eta`: Estimated time remaining

### `Message(message string)`
Sets a status message displayed at the bottom (typically the currently processing file path).

### `Done(location string, done bool)`
Marks a location as complete or incomplete.
- `location`: Path to the location
- `done`: true to mark as complete (shows "DONE ✔"), false to mark as incomplete

### `Finish(successMessage string)`
Completes the progress view, keeps the final state on screen, shows cursor, and displays a success message.
- `successMessage`: Optional message to display after completion (e.g., "✓ Backup complete")

### `Clear()`
Clears the progress view from the terminal. Use this only for error cases. For successful completion, use `Finish()` instead.

### `IsTerminal() bool`
Returns true if stdout is a terminal (TTY).

## Example Output

```
~/github
[⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿] DONE ✔ (in green)

~/playground
[⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡿          ] ETA 12s

~/Screenshots
[                                          ] ETA 2min 31s

Writing: ~/github/schneider-group/ugm-website/src

✓ Backup successfully stored at ./backup (✓ in green)
```

## Implementation Details

- Uses ANSI escape sequences for terminal manipulation
- Thread-safe with mutex locks
- Progress bars are 42 characters wide
- Rate limited updates (max 1 update per 10ms) to prevent terminal overload
- Smart rendering: only updates when visual changes occur
- Automatic cursor hiding during operation
- Full file paths displayed (wraps if needed, automatically cleared)
- Final state remains on screen after completion
- ANSI color support for success indicators (green)

