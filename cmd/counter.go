package cmd

import (
	"sync"
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
	s.bar = bar
}

func (s *SpeedCounter) Start() {
	go s.worker()
}

func (s *SpeedCounter) IncrBy(n int) {
	s.mu.RLock()
	s.bpc += int64(n)
	s.mu.RUnlock()
}

func (s *SpeedCounter) Stop() {
	s.ticker.Stop()
}

func (s *SpeedCounter) worker() {
	for range s.ticker.C {
		if s.bpc == 0 {
			continue
		}
		if s.bar == nil {
			continue
		}
		s.mu.Lock()
		s.bar.EwmaIncrInt64(s.bpc, s.refreshRate)
		s.bpc = 0
		s.mu.Unlock()
	}
}
