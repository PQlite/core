package p2p

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

var GlobalProgressBar *ProgressBar
var pbMu sync.Mutex

type ProgressBar struct {
	Total   int
	Current int
	spinner int
}

type LogWrapper struct {
	io.Writer
}

func (l *LogWrapper) Write(p []byte) (n int, err error) {
	pbMu.Lock()
	defer pbMu.Unlock()

	if GlobalProgressBar != nil {
		fmt.Fprintf(l.Writer, "\r\033[K")
	}
	n, err = l.Writer.Write(p)
	if GlobalProgressBar != nil {
		GlobalProgressBar.renderLocked()
	}
	return
}

func NewLogWrapper(w io.Writer) *LogWrapper {
	return &LogWrapper{Writer: w}
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (pb *ProgressBar) Render() {
	pbMu.Lock()
	defer pbMu.Unlock()
	pb.renderLocked()
}

func (pb *ProgressBar) renderLocked() {
	width := 40
	pb.spinner = (pb.spinner + 1) % len(spinnerChars)
	s := spinnerChars[pb.spinner]

	if pb.Total <= pb.Current {
		fmt.Fprintf(os.Stderr, "\r\033[K%s Syncing... Block: %d (Scanning for network height...)", s, pb.Current)
		return
	}

	percent := float64(pb.Current) / float64(pb.Total)
	if percent > 1.0 {
		percent = 1.0
	}
	filled := int(percent * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	fmt.Fprintf(os.Stderr, "\r\033[K%s Syncing: [%s] %.1f%% (%d/%d)", s, bar, percent*100, pb.Current, pb.Total)
}

func (pb *ProgressBar) Finish() {
	pbMu.Lock()
	defer pbMu.Unlock()
	GlobalProgressBar = nil
	fmt.Fprintln(os.Stderr, "\n\033[32mSync completed successfully!\033[0m")
}
