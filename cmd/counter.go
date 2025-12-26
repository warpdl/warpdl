package cmd

import (
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
)

type SpeedCounter struct {
	ticker *time.Ticker
	mu     *sync.RWMutex
	// bytes per cycle
	bpc int64
	// refresh rate
	refreshRate time.Duration
	// Bar
	bar *mpb.Bar
	// lastTick tracks the actual time of the last tick for accurate EWMA calculation
	lastTick time.Time
}

func NewSpeedCounter(refreshRate time.Duration) *SpeedCounter {
	sc := SpeedCounter{
		ticker:      time.NewTicker(refreshRate),
		mu:          &sync.RWMutex{},
		refreshRate: refreshRate,
	}
	return &sc
}

func (s *SpeedCounter) SetBar(bar *mpb.Bar) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bar = bar
}

func (s *SpeedCounter) Start() {
	s.lastTick = time.Now()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in SpeedCounter.worker: %v\n%s", r, debug.Stack())
				s.ticker.Stop()
			}
		}()
		s.worker()
	}()
}

func (s *SpeedCounter) IncrBy(n int) {
	atomic.AddInt64(&s.bpc, int64(n))
}

func (s *SpeedCounter) Stop() {
	s.ticker.Stop()
}

func (s *SpeedCounter) worker() {
	for range s.ticker.C {
		now := time.Now()

		if atomic.LoadInt64(&s.bpc) == 0 {
			s.lastTick = now
			continue
		}

		s.mu.RLock()
		bar := s.bar
		s.mu.RUnlock()

		if bar == nil {
			s.lastTick = now
			continue
		}

		elapsed := now.Sub(s.lastTick)
		s.lastTick = now

		bpc := atomic.SwapInt64(&s.bpc, 0)
		if bpc != 0 {
			bar.EwmaIncrInt64(bpc, elapsed)
		}
	}
}
