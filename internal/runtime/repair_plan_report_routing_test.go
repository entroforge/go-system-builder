package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func TestRepairPlanReportRoutingNeverOpensBodiesAndKeepsUnknown(t *testing.T) {
	f := newRepairPlanReportRuntimeFixture(t)
	externalizeS9FixtureRows(t, f.root, f.state)
	reads := 0
	ctx := packetbody.WithResolver(t.Context(), func(context.Context, packetbody.Reference) ([]byte, error) {
		reads++
		return nil, errors.New("body unavailable")
	})
	routes, err := CurrentRepairPlanReportRoutesContext(ctx, f.state)
	if err != nil || len(routes) != 1 || routes[0].ID != f.packet.ID || reads != 0 {
		t.Fatalf("light routing: %+v reads=%d err=%v", routes, reads, err)
	}
	if _, _, err := FindRepairPlanReportPacketContext(ctx, f.state, routes[0].ID); err == nil || reads != 1 {
		t.Fatalf("selected body fault hidden: reads=%d err=%v", reads, err)
	}
	rows := f.state["entities"].(map[string]any)["work_packets"].([]any)
	f.state["entities"].(map[string]any)["work_packets"] = append(rows, rows[0])
	if _, err := CurrentRepairPlanReportRoutesContext(ctx, f.state); err == nil {
		t.Fatal("duplicate identity treated as no responsibility")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := CurrentRepairPlanReportRoutesContext(canceled, f.state); !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancellation lost: %v", err)
	}
}
