package p2p

import (
	"fmt"
	"os"
	"strings"
)

type ProgressBar struct {
	Total   int
	Current int
}

func (pb *ProgressBar) Render() {
	if pb.Total <= 0 {
		return
	}
	width := 40
	percent := float64(pb.Current) / float64(pb.Total)
	if percent > 1.0 {
		percent = 1.0
	}
	filled := int(percent * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("-", width-filled)
	fmt.Fprintf(os.Stderr, "\rSyncing: [%s] %d/%d", bar, pb.Current, pb.Total)
	if pb.Current >= pb.Total {
		fmt.Fprintln(os.Stderr)
	}
}
