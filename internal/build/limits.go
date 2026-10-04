package build

import (
	"context"
	"time"
)

const buildTimeout = 30 * time.Minute

// acquire serializes builds across services using the shared Builder.
func (b *Builder) acquire(ctx context.Context) (func(), error) {
	b.once.Do(func() { b.slot = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case b.slot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-b.slot
			return nil, err
		}
		return func() { <-b.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
