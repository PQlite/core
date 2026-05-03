package p2p

import (
	"fmt"
	"strings"
)

type ProgressBar struct {
	Total int
	Current int
}

func (pb *ProgressBar) Render() {
	width := 40
	percent := float64(pb.Current) / float64(pb.Total)
	filled := int(percent * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("-", width-filled)
	fmt.Printf("\rSyncing: [%s] %d/%d", bar, pb.Current, pb.Total)
	if pb.Current >= pb.Total {
		fmt.Println()
	}
}
