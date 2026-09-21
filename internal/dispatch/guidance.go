package dispatch

import (
	"fmt"
	"strings"
)

// Advisory renders bounded, read-only hints. A zero-capacity board can identify
// eligible tasks without inventing physical slots or claiming agents were spawned.
func Advisory(b Board) []string {
	if b.Legacy || b.Plan == nil {
		return nil
	}
	ready, reported, waiting := []string{}, []string{}, []string{}
	for _, row := range b.Rows {
		switch row.State {
		case "ready", "queued":
			ready = append(ready, row.Task.ID)
		case "reported":
			reported = append(reported, row.Task.ID)
		case "waiting", "blocked":
			waiting = append(waiting, row.Task.ID+": "+row.Reason)
		}
	}
	result := []string{"S6 approved dispatch plan: " + b.Plan.Path}
	if len(ready) > 0 {
		result = append(result, "eligible tasks (choose within actual available capacity): "+boundedItems(ready))
	}
	if len(reported) > 0 {
		result = append(result, "results awaiting verified integration into the bound development branch: "+boundedItems(reported))
	}
	if len(waiting) > 0 {
		result = append(result, "waiting: "+boundedItems(waiting))
	}
	if len(ready) == 0 && len(reported) == 0 && len(waiting) == 0 {
		result = append(result, "no new dispatch candidates; running/integrated tasks remain visible in s6 status")
	}
	return result
}
func boundedItems(items []string) string {
	const limit = 4
	if len(items) <= limit {
		return strings.Join(items, "; ")
	}
	return strings.Join(items[:limit], "; ") + fmt.Sprintf("; +%d more (s6 status)", len(items)-limit)
}
