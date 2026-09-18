package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
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
	// Interval between screen updates. Updates only change state; a
	// background goroutine redraws at this rate so rendering never slows
	// down the caller.
	updateInterval = 100 * time.Millisecond
	// ANSI color codes
	colorGreen = "\033[32m"
	colorReset = "\033[0m"
)

// ProgressItem represents a single progress bar entry
type ProgressItem struct {
	Location string
	Progress float64 // 0.0 to 1.0
	ETA      time.Duration
	Done     bool
}

// ProgressView manages multiple progress bars. On a terminal it redraws in
// place; otherwise it prints one plain line per finished location.
type ProgressView struct {
	items               map[string]*ProgressItem
	order               []string // Maintain insertion order
	message             string   // Current status message
	messagePrefix       string   // Prefix for status messages (e.g., "Writing", "Extracting")
	lastRenderedState   string   // Last rendered output (progress bars only)
	lastRenderedMessage string   // Last rendered message
	writer              io.Writer
	interactive         bool // Whether writer is a terminal
	mu                  sync.Mutex
	lastLines           int  // Track how many lines were printed last time
	cursorHidden        bool // Track if cursor is hidden
	stop                chan struct{}
	stopOnce            sync.Once
	stopped             chan struct{}
}

// NewProgressView creates a new progress view with a custom message prefix
func NewProgressView(messagePrefix string) *ProgressView {
	if messagePrefix == "" {
		messagePrefix = "Processing"
	}

	pv := &ProgressView{
		items:         make(map[string]*ProgressItem),
		order:         make([]string, 0),
		writer:        os.Stdout,
		interactive:   IsTerminal(),
		messagePrefix: messagePrefix,
		stop:          make(chan struct{}),
		stopped:       make(chan struct{}),
	}

	if pv.interactive {
		go pv.renderLoop()
	} else {
		close(pv.stopped)
	}

	return pv
}

// IsTerminal checks if stdout is a terminal (TTY)
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// renderLoop redraws the view periodically until stopRendering is called
func (pv *ProgressView) renderLoop() {
	defer close(pv.stopped)

	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pv.mu.Lock()
			pv.render()
			pv.mu.Unlock()
		case <-pv.stop:
			return
		}
	}
}

// stopRendering stops the render loop and waits for it to exit. It must be
// called without holding pv.mu.
func (pv *ProgressView) stopRendering() {
	pv.stopOnce.Do(func() { close(pv.stop) })
	<-pv.stopped
}

// Add adds a new progress bar for a location
func (pv *ProgressView) Add(location string, progress float64, eta time.Duration) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	// Hide cursor on first add
	if pv.interactive && !pv.cursorHidden {
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

	item.Progress = progress
	item.ETA = eta

	// Mark as done if progress is 1.0
	if progress >= 1.0 {
		item.Progress = 1.0
		pv.markDone(item)
	}
}

// Message sets a status message (typically the currently processing file path)
func (pv *ProgressView) Message(message string) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	pv.message = message
}

// Done marks a location as complete or incomplete
func (pv *ProgressView) Done(location string, done bool) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	if item, exists := pv.items[location]; exists {
		if done {
			item.Progress = 1.0
			pv.markDone(item)
		} else {
			item.Done = false
		}
	}
}

// markDone marks an item as done and, when not on a terminal, reports it once
func (pv *ProgressView) markDone(item *ProgressItem) {
	if item.Done {
		return
	}
	item.Done = true
	if !pv.interactive {
		fmt.Fprintf(pv.writer, "%s: %s done\n", pv.messagePrefix, item.Location)
	}
}

// Finish completes the progress view and shows cursor
func (pv *ProgressView) Finish(successMessage string) {
	pv.stopRendering()

	pv.mu.Lock()
	defer pv.mu.Unlock()

	if !pv.interactive {
		if successMessage != "" {
			fmt.Fprintf(pv.writer, "%s\n", successMessage)
		}
		return
	}

	// Force a final render to show completed state
	pv.message = ""
	pv.render()

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
	pv.stopRendering()

	pv.mu.Lock()
	defer pv.mu.Unlock()

	if !pv.interactive {
		return
	}

	var b strings.Builder
	pv.clearLines(&b)
	io.WriteString(pv.writer, b.String())
	pv.lastRenderedState = ""
	pv.lastRenderedMessage = ""
	pv.lastLines = 0
	pv.message = ""

	// Show cursor again
	if pv.cursorHidden {
		pv.showCursor()
	}
}

// render redraws the view if anything changed since the last frame. The whole
// frame is written with a single write call. Callers must hold pv.mu.
func (pv *ProgressView) render() {
	var frame strings.Builder

	// Render each progress item: location header, bar and status, blank line
	for i, location := range pv.order {
		item := pv.items[location]
		if i > 0 {
			frame.WriteByte('\n')
		}
		fmt.Fprintf(&frame, "%s\n[%s] %s\n", location, pv.renderProgressBar(item), pv.renderStatus(item))
	}
	output := frame.String()

	// Only update if progress bars or message changed
	if output == pv.lastRenderedState && pv.message == pv.lastRenderedMessage {
		return
	}

	var b strings.Builder

	// Clear previous output
	pv.clearLines(&b)

	// Write progress bars
	b.WriteString(output)

	// Save cursor position (after progress bars, before message area) and
	// clear everything below, which handles messages that wrapped
	b.WriteString("\033[s\033[J")

	// Write message on new line if present
	if pv.message != "" {
		fmt.Fprintf(&b, "\n%s: %s", pv.messagePrefix, pv.message)
	}

	// Restore cursor position (back to end of progress bars)
	b.WriteString("\033[u")

	io.WriteString(pv.writer, b.String())

	// Track state - only track progress bar lines
	pv.lastRenderedState = output
	pv.lastRenderedMessage = pv.message
	pv.lastLines = strings.Count(output, "\n")
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

// clearLines appends the sequences that erase the previously printed lines
func (pv *ProgressView) clearLines(b *strings.Builder) {
	if pv.lastLines == 0 {
		return
	}

	// Move cursor up to first line of our content, then to the beginning of
	// the line, and clear from there to the end of the screen. This clears
	// all our content without touching lines above.
	fmt.Fprintf(b, "\033[%dA\r\033[J", pv.lastLines)
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
