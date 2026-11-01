package buildsurface

import (
	"context"

	loopruntime "github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/fileview"
)

// WithReadCache scopes the current journal and file-view readers to a request.
func WithReadCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return fileview.WithReadCache(loopruntime.WithJournalReadCache(ctx))
}
