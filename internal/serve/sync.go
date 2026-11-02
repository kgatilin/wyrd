package serve

import (
	"context"

	"github.com/kgatilin/wyrd/internal/serve/modelstore"
	"github.com/kgatilin/wyrd/internal/treestamp"
)

// Sync brings the model to what is on disk now and returns the packages
// whose model moved (nil when nothing did).
//
// It compares the tree's content stamps with the ones recorded when the
// model was read, so it answers without trusting that every file event
// arrived: the packages whose files differ are read over the current model,
// one whose source is gone is dropped, and a change to go.mod or go.sum is a
// full parse. An unchanged tree costs one walk of stats. Every read records
// the stamps it took before reading, so a file edited during a read keeps
// its old stamp and the next Sync reads it again.
//
// A failed read is kept (ReadError) until a Sync succeeds. The stamps stay
// those of the old model, so the next Sync reads the same packages again.
func (s *State) Sync(ctx context.Context) (moved []string, err error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	defer func() { s.recordRead(err) }()

	now, err := treestamp.Scan(s.root)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	prev := s.stamps
	s.mu.RUnlock()

	dirs, module := now.Changed(prev)
	var versions []modelstore.Version
	switch {
	case module:
		versions, err = s.readAll(ctx)
	case len(dirs) == 0:
		return nil, nil
	default:
		versions, err = s.readOver(ctx, s.versionList(), dirs)
	}
	if err != nil {
		return nil, err
	}
	moved, _, err = s.replaceModel(ctx, versions, now)
	return moved, err
}
