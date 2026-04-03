package email

import (
	"sync"
	"time"
)

// QuotaCosts defines the API quota cost per operation.
var QuotaCosts = map[string]float64{
	"messages.list":        5,
	"messages.get":         5,
	"messages.modify":      5,
	"messages.batchModify": 50,
	"messages.trash":       5,
	"messages.delete":      10,
	"messages.send":        100,
	"labels.list":          1,
	"labels.get":           1,
}

// RateLimiter implements a token bucket rate limiter for Gmail API quota.
type RateLimiter struct {
	maxQuotaPerMinute float64
	tokens            float64
	lastRefill        time.Time
	mu                sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
// Default is 15000 quota units per minute (Gmail per-user limit).
func NewRateLimiter(maxQuotaPerMinute float64) *RateLimiter {
	if maxQuotaPerMinute <= 0 {
		maxQuotaPerMinute = 15000
	}
	return &RateLimiter{
		maxQuotaPerMinute: maxQuotaPerMinute,
		tokens:            maxQuotaPerMinute,
		lastRefill:        time.Now(),
	}
}

// Acquire waits until quota tokens are available for the operation.
func (r *RateLimiter) Acquire(operation string) {
	r.AcquireN(operation, 1)
}

// AcquireN waits until quota tokens are available for N operations.
func (r *RateLimiter) AcquireN(operation string, count int) {
	cost := QuotaCosts[operation]
	if cost == 0 {
		cost = 5 // Default cost for unknown operations
	}
	totalCost := cost * float64(count)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	for r.tokens < totalCost {
		waitTime := time.Duration((totalCost-r.tokens)/(r.maxQuotaPerMinute/60)*1000) * time.Millisecond
		r.mu.Unlock()
		time.Sleep(waitTime)
		r.mu.Lock()
		r.refill()
	}

	r.tokens -= totalCost
}

func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens += elapsed * r.maxQuotaPerMinute / 60
	if r.tokens > r.maxQuotaPerMinute {
		r.tokens = r.maxQuotaPerMinute
	}
	r.lastRefill = now
}

// Tokens returns the current available tokens (for testing).
func (r *RateLimiter) Tokens() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refill()
	return r.tokens
}
