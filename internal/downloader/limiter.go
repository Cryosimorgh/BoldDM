package downloader

import (
	"context"
	"sync"
	"time"
)

// BandwidthLimiter is a process-wide reservation limiter shared by every
// download and segment. A limit of zero means unlimited.
type BandwidthLimiter struct {
	mu    sync.Mutex
	limit int64
	next  time.Time
}

func NewBandwidthLimiter(bytesPerSecond int64) *BandwidthLimiter {
	l := &BandwidthLimiter{}
	l.SetLimit(bytesPerSecond)
	return l
}

func (l *BandwidthLimiter) SetLimit(bytesPerSecond int64) {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	l.mu.Lock()
	l.limit = bytesPerSecond
	l.next = time.Now()
	l.mu.Unlock()
}

func (l *BandwidthLimiter) Limit() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

func (l *BandwidthLimiter) Wait(ctx context.Context, bytes int) error {
	if bytes <= 0 {
		return nil
	}
	l.mu.Lock()
	limit := l.limit
	if limit <= 0 {
		l.mu.Unlock()
		return nil
	}
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	start := l.next
	duration := time.Duration(float64(bytes) / float64(limit) * float64(time.Second))
	if duration < 0 {
		duration = 0
	}
	l.next = l.next.Add(duration)
	l.mu.Unlock()

	wait := time.Until(start)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
