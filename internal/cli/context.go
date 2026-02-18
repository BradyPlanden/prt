package cli

import (
	"context"
	"time"
)

const defaultCommandTimeout = 10 * time.Minute

func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultCommandTimeout)
}
