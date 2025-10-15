package tui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	// Braille character for filled progress
	brailleFilled = "⣿"
	// Space for empty progress
	brailleEmpty = " "
	// Progress bar width in characters
	progressBarWidth = 42
	// Minimum time between screen updates (rate limiting)
	updateInterval = 10 * time.Millisecond
	// ANSI color codes
	colorGreen = "\033[32m"
	colorReset = "\033[0m"
)

// ProgressItem represents a single progress bar entry
type ProgressItem struct {
	Location        string
	Progress        float64 // 0.0 to 1.0
	ETA             time.Duration
	Done            bool
	lastRenderedBar int           // Last rendered bar length
	lastRenderedETA time.Duration // Last rendered ETA
}

// ProgressView manages multiple progress bars
type ProgressView struct {
	items               map[string]*ProgressItem
	order               []string  // Maintain insertion order
	message             string    // Current status message
	lastRenderedState   string    // Last rendered output (progress bars only)
	lastRenderedMessage string    // Last rendered message
	lastUpdateTime      time.Time // Last screen update time
	writer              io.Writer
	mu                  sync.RWMutex
	lastLines           int  // Track how many lines were printed last time
	cursorHidden        bool // Track if cursor is hidden
}

// NewProgressView creates a new progress view
func NewProgressView() *ProgressView {
	pv := &ProgressView{
		items:  make(map[string]*ProgressItem),
		order:  make([]string, 0),
		writer: os.Stdout,
	}

	// Set up signal handler for Ctrl+C
	pv.setupSignalHandler()

	return pv
}

// setupSignalHandler sets up handling for interrupt signals
func (pv *ProgressView) setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		// Show cursor before exiting
		pv.mu.Lock()
		if pv.cursorHidden {
			pv.showCursor()
		}
		pv.mu.Unlock()
		os.Exit(130) // Standard exit code for Ctrl+C
	}()
}

// IsTerminal checks if stdout is a terminal (TTY)
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Add adds a new progress bar for a location
func (pv *ProgressView) Add(location string, progress float64, eta time.Duration) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	// Hide cursor on first add
	if !pv.cursorHidden {
		pv.hideCursor()
	}

	if _, exists := pv.items[location]; !exists {
		pv.order = append(pv.order, location)
	}

	pv.items[location] = &ProgressItem{
		Location: location,
		Progress: progress,
		ETA:      eta,
		Done:     false,
	}

	pv.render()
}

// Set updates an existing progress bar
func (pv *ProgressView) Set(location string, progress float64, eta time.Duration) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	item, exists := pv.items[location]
	if !exists {
		// If it doesn't exist, add it
		pv.order = append(pv.order, location)
		item = &ProgressItem{
			Location: location,
		}
		pv.items[location] = item
	}

	// Check if we need to update
	newBarLength := min(int(progress*float64(progressBarWidth)), progressBarWidth)
	etaSeconds := int(eta.Seconds())
	lastEtaSeconds := int(item.lastRenderedETA.Seconds())

	// Only update if braille character changed or ETA changed by at least 1 second
	shouldUpdate := newBarLength != item.lastRenderedBar || etaSeconds != lastEtaSeconds

	item.Progress = progress
	item.ETA = eta

	// Mark as done if progress is 1.0
	if progress >= 1.0 {
		item.Done = true
		item.Progress = 1.0
		shouldUpdate = true // Always update when done
	}

	if shouldUpdate {
		item.lastRenderedBar = newBarLength
		item.lastRenderedETA = eta
		pv.render()
	}
}

// Message sets a status message (typically the currently processing file path)
func (pv *ProgressView) Message(message string) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	// Only update if message changed
	if pv.message != message {
		pv.message = message
		pv.render()
	}
}

// Done marks a location as complete or incomplete
func (pv *ProgressView) Done(location string, done bool) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	if item, exists := pv.items[location]; exists {
		item.Done = done
		if done {
			item.Progress = 1.0
		}
		pv.render()
	}
}

// Finish completes the progress view and shows cursor
func (pv *ProgressView) Finish(successMessage string) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	// Force a final render to show completed state
	pv.message = ""
	pv.renderNow()

	// Print success message on a new line with green checkmark
	if successMessage != "" {
		// Replace the checkmark with a colored version
		coloredMessage := strings.Replace(successMessage, "✓", colorGreen+"✓"+colorReset, 1)
		fmt.Fprintf(pv.writer, "\n\n%s\n", coloredMessage)
	} else {
		fmt.Fprint(pv.writer, "\n")
	}

	// Show cursor again
	if pv.cursorHidden {
		pv.showCursor()
	}
}

// Clear clears the progress view from the terminal (for errors/cleanup)
func (pv *ProgressView) Clear() {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	pv.clearLines()
	pv.lastRenderedState = ""
	pv.lastRenderedMessage = ""
	pv.message = ""

	// Show cursor again
	if pv.cursorHidden {
		pv.showCursor()
	}
}

// render renders the entire progress view with rate limiting
func (pv *ProgressView) render() {
	// Rate limit: only update if enough time has passed
	now := time.Now()
	if !pv.lastUpdateTime.IsZero() && now.Sub(pv.lastUpdateTime) < updateInterval {
		return
	}

	pv.renderNow()
}

// renderNow renders immediately without rate limiting
func (pv *ProgressView) renderNow() {
	lines := make([]string, 0)

	// Render each progress item
	for _, location := range pv.order {
		item := pv.items[location]

		// Location header
		lines = append(lines, location)

		// Progress bar and status
		bar := pv.renderProgressBar(item)
		status := pv.renderStatus(item)
		lines = append(lines, fmt.Sprintf("[%s] %s", bar, status))

		// Empty line between items
		lines = append(lines, "")
	}

	// Build output for progress bars only
	output := strings.Join(lines, "\n")

	// Only update if progress bars or message changed
	if output == pv.lastRenderedState && pv.message == pv.lastRenderedMessage {
		return
	}

	// Clear previous output
	pv.clearLines()

	// Write progress bars
	fmt.Fprint(pv.writer, output)

	// Save cursor position (after progress bars, before message area)
	fmt.Fprint(pv.writer, "\033[s")

	// Clear everything from cursor to end of screen
	// This handles messages that wrap across multiple lines
	fmt.Fprint(pv.writer, "\033[J")

	// Write message on new line if present
	if pv.message != "" {
		fmt.Fprintf(pv.writer, "\nWriting: %s", pv.message)
	}

	// Restore cursor position (back to end of progress bars)
	fmt.Fprint(pv.writer, "\033[u")

	// Track state - only track progress bar lines
	pv.lastRenderedState = output
	pv.lastRenderedMessage = pv.message
	pv.lastLines = len(lines)
	pv.lastUpdateTime = time.Now()
}

// renderProgressBar creates the braille progress bar
func (pv *ProgressView) renderProgressBar(item *ProgressItem) string {
	filled := min(int(item.Progress*float64(progressBarWidth)), progressBarWidth)

	bar := strings.Repeat(brailleFilled, filled)
	empty := strings.Repeat(brailleEmpty, progressBarWidth-filled)

	return bar + empty
}

// renderStatus creates the status message (ETA or DONE)
func (pv *ProgressView) renderStatus(item *ProgressItem) string {
	if item.Done {
		return colorGreen + "DONE ✔" + colorReset
	}

	if item.ETA > 0 {
		return fmt.Sprintf("ETA %s", pv.formatDuration(item.ETA))
	}

	return "Calculating..."
}

// formatDuration formats a duration for display
func (pv *ProgressView) formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}

	seconds := int(d.Seconds())
	minutes := seconds / 60
	seconds = seconds % 60

	if minutes > 0 {
		return fmt.Sprintf("%dmin %ds", minutes, seconds)
	}

	return fmt.Sprintf("%ds", seconds)
}

// clearLines clears the previously printed lines
func (pv *ProgressView) clearLines() {
	if pv.lastLines == 0 {
		return
	}

	// Move cursor up to first line of our content
	for i := 0; i < pv.lastLines; i++ {
		fmt.Fprint(pv.writer, "\033[A")
	}

	// Move to beginning of line
	fmt.Fprint(pv.writer, "\r")

	// Clear from cursor to end of screen
	// This clears all our content without touching lines above
	fmt.Fprint(pv.writer, "\033[J")
}

// hideCursor hides the terminal cursor
func (pv *ProgressView) hideCursor() {
	fmt.Fprint(pv.writer, "\033[?25l")
	pv.cursorHidden = true
}

// showCursor shows the terminal cursor
func (pv *ProgressView) showCursor() {
	fmt.Fprint(pv.writer, "\033[?25h")
	pv.cursorHidden = false
}
