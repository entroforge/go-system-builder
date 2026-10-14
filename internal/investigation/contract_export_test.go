package investigation

import (
	"context"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// The test build alone exposes the writer seam. CLI/user input cannot inject
// commit failures or alter the normal approval pipeline.
func ApproveContractWithTestWriter(root, sp, jp string, request ContractRequest, configure func(*runtime.Store) *runtime.Store) (runtime.Snapshot, error) {
	return approveContractContext(context.Background(), root, sp, jp, request, configure)
}
