//go:build integration

package api

import (
	"context"

	"github.com/sixface/beenthere/internal/visits"
)

func detectVisits(ctx context.Context, h *harness) (int, error) {
	u, err := h.s.UserByID(ctx, h.uid)
	if err != nil {
		return 0, err
	}
	return visits.Run(ctx, h.s, u, 0, 1<<31-1)
}
