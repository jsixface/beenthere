// Package tracks turns streams of points into tracks.
package tracks

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/sixface/beenthere/internal/store"
)

// Segment splits time-ordered points of one device into segments whenever the
// gap between consecutive points exceeds gapSeconds. Segments with fewer than
// two points are dropped (matching Tracks::Segmentation).
func Segment(pts []store.TrackPoint, gapSeconds int64) [][]store.TrackPoint {
	var out [][]store.TrackPoint
	var cur []store.TrackPoint
	for _, p := range pts {
		if len(cur) > 0 && p.Timestamp-cur[len(cur)-1].Timestamp > gapSeconds {
			if len(cur) >= 2 {
				out = append(out, cur)
			}
			cur = nil
		}
		cur = append(cur, p)
	}
	if len(cur) >= 2 {
		out = append(out, cur)
	}
	return out
}

func sameTracker(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Generate builds tracks for every untracked point of the user at or after
// fromTS, using the user's minutes_between_routes setting as the gap. Points
// stream from the database ordered by device then time, so only one segment is
// ever held in memory.
func Generate(ctx context.Context, s *store.Store, u *store.User, fromTS int64) (int, error) {
	gap := int64(u.Setting("minutes_between_routes")) * 60
	created := 0
	var cur []store.TrackPoint
	flush := func() error {
		defer func() { cur = cur[:0] }()
		if len(cur) < 2 {
			return nil
		}
		return s.Tx(ctx, func(tx pgx.Tx) error {
			id, err := store.InsertTrack(ctx, tx, u.ID, cur, nil)
			if id != 0 {
				created++
			}
			return err
		})
	}
	err := s.UntrackedPoints(ctx, u.ID, fromTS, func(p store.TrackPoint) error {
		if len(cur) > 0 {
			last := cur[len(cur)-1]
			if !sameTracker(last.TrackerID, p.TrackerID) || p.Timestamp-last.Timestamp > gap {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		cur = append(cur, p)
		return nil
	})
	if err != nil {
		return created, err
	}
	return created, flush()
}
