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

	if pb.Total <= 0 {
		fmt.Fprintf(os.Stderr, "\r\033[K%s Syncing... Block: %d (Target unknown)", s, pb.Current)
		return
	}

	percent := float64(pb.Current) / float64(pb.Total)
	if percent > 1.0 {
		percent = 1.0
	}
	filled := int(percent * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	
	fmt.Fprintf(os.Stderr, "\r\033[K%s Syncing: [%s] %.1f%% (%d/%d)", s, bar, percent*100, pb.Current, pb.Total)
	
	if pb.Current >= pb.Total && pb.Total > 0 {
		fmt.Fprintln(os.Stderr, "\nSync completed!")
	}
}
