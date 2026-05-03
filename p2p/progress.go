package p2p

import (
	"fmt"
	"os"
	"strings"
)

type ProgressBar struct {
	Total   int
	Current int
	spinner int
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (pb *ProgressBar) Render() {
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
	fmt.Fprintln(os.Stderr, "\n\033[32mSync completed successfully!\033[0m")
}
