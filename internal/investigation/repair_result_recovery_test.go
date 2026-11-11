package investigation_test

import (
	"bytes"
	"testing"
)

func assertS9RecoveryPairUnchanged(t *testing.T, fixture *intakeFixture, state, journal []byte, reason string) {
	t.Helper()
	if !bytes.Equal(state, mustRead(t, fixture.statePath)) || !bytes.Equal(journal, mustRead(t, fixture.journalPath)) {
		t.Fatalf("%s changed the original Runtime pair", reason)
	}
}
