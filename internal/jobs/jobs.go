// Package jobs is the in-process replacement for Sidekiq: a debounced
// per-user "points arrived" pipeline plus a small bounded worker pool.
package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sixface/beenthere/internal/stats"
	"github.com/sixface/beenthere/internal/store"
	"github.com/sixface/beenthere/internal/tracks"
)

const debounce = 10 * time.Second

type pending struct {
	minTS, maxTS int64
	timer        *time.Timer
}

type Manager struct {
	S     *store.Store
	log   *slog.Logger
	mu    sync.Mutex
	dirty map[int64]*pending
	queue chan func(context.Context)
	wg    sync.WaitGroup
	ctx   context.Context
	locks sync.Map // userID -> *sync.Mutex; serializes track/stat rebuilds per user
}

func New(ctx context.Context, s *store.Store, workers int, log *slog.Logger) *Manager {
	m := &Manager{S: s, log: log, dirty: map[int64]*pending{}, queue: make(chan func(context.Context), 256), ctx: ctx}
	for i := 0; i < workers; i++ {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case fn := <-m.queue:
					fn(ctx)
				}
			}
		}()
	}
	return m
}

// Enqueue schedules fn on the worker pool. If the queue is full it runs the job
// in a fresh goroutine rather than dropping it.
func (m *Manager) Enqueue(fn func(context.Context)) {
	select {
	case m.queue <- fn:
	default:
		go fn(m.ctx)
	}
}

// PointsArrived records that new points landed for a user; after a quiet
// period it generates tracks and refreshes monthly stats.
func (m *Manager) PointsArrived(userID int64, ids []int64, minTS, maxTS int64) {
	if len(ids) > 0 {
		m.Enqueue(func(ctx context.Context) {
			if err := m.S.AssignCountries(ctx, userID, ids); err != nil {
				m.log.Warn("assign countries", "user", userID, "err", err)
			}
		})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.dirty[userID]
	if !ok {
		p = &pending{minTS: minTS, maxTS: maxTS}
		m.dirty[userID] = p
	} else {
		p.minTS, p.maxTS = min(p.minTS, minTS), max(p.maxTS, maxTS)
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(debounce, func() { m.flush(userID) })
}

func (m *Manager) flush(userID int64) {
	m.mu.Lock()
	p := m.dirty[userID]
	delete(m.dirty, userID)
	m.mu.Unlock()
	if p == nil {
		return
	}
	m.Enqueue(func(ctx context.Context) { m.Recompute(ctx, userID, p.minTS, p.maxTS) })
}

// Recompute builds tracks and stats for a user's timestamp range.
func (m *Manager) Recompute(ctx context.Context, userID, minTS, maxTS int64) {
	l, _ := m.locks.LoadOrStore(userID, &sync.Mutex{})
	mu := l.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	u, err := m.S.UserByID(ctx, userID)
	if err != nil {
		return
	}
	from := minTS - int64(u.Setting("minutes_between_routes"))*60*2
	if n, err := tracks.Generate(ctx, m.S, u, from); err != nil {
		m.log.Error("generate tracks", "user", userID, "err", err)
	} else if n > 0 {
		m.log.Info("tracks generated", "user", userID, "count", n)
	}
	for _, ym := range stats.MonthsInRange(minTS, maxTS, u.Timezone()) {
		if err := stats.CalculateMonth(ctx, m.S, u, ym[0], ym[1]); err != nil {
			m.log.Error("calculate stats", "user", userID, "ym", ym, "err", err)
		}
	}
}

func (m *Manager) Wait() { m.wg.Wait() }
