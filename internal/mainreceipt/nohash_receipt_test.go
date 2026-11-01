package mainreceipt

import (
	"context"
	"testing"
)

func TestMainReceiptReadsActualResponsibilityWithoutDigestOrOID(t *testing.T) {
	f := newReaderFixture(t)
	writeCheckpointFixture(t, f, f.checkpoint)
	proof, err := Verify(context.Background(), f.input)
	if err != nil {
		t.Fatalf("original actual facts rejected by historical metadata: %v", err)
	}
	if proof.RecordReference() != f.input.Record.Reference() {
		t.Fatal("original responsibility was lost")
	}
	facts := proof.MainFacts()
	if len(facts.Checks) == 0 || facts.Checks[0].Status != "pass" {
		t.Fatal("actual check result was lost")
	}
}
