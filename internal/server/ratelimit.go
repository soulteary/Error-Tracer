package server

import (
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRatePerMinute  = 120
	defaultRateBurst      = 30
	maxRateLimitClients   = 10_000
	rateLimitClientTTL    = 10 * time.Minute
	rateLimitEvictionScan = 64
)

type rateBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

type rateLimiter struct {
	mu            sync.Mutex
	buckets       map[string]rateBucket
	perSecond     float64
	burst         float64
	maxClients    int
	now           func() time.Time
	lastSweepTime time.Time
}

func newRateLimiter(perMinute, burst int) *rateLimiter {
	if perMinute <= 0 {
		perMinute = defaultRatePerMinute
	}
	if burst <= 0 {
		burst = defaultRateBurst
	}
	return &rateLimiter{
		buckets:    make(map[string]rateBucket),
		perSecond:  float64(perMinute) / 60,
		burst:      float64(burst),
		maxClients: maxRateLimitClients,
		now:        time.Now,
	}
}

// Allow consumes one token for a client or returns the time until one is
// available. The map is bounded so unique-source floods cannot grow memory
// without limit.
func (limiter *rateLimiter) Allow(client string) (bool, time.Duration) {
	return limiter.AllowN(client, 1)
}

// AllowN atomically consumes a positive number of tokens for one client.
func (limiter *rateLimiter) AllowN(client string, tokens int) (bool, time.Duration) {
	if tokens <= 0 {
		return true, 0
	}
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if limiter.lastSweepTime.IsZero() || now.Sub(limiter.lastSweepTime) >= time.Minute {
		for key, bucket := range limiter.buckets {
			if now.Sub(bucket.lastSeen) >= rateLimitClientTTL {
				delete(limiter.buckets, key)
			}
		}
		limiter.lastSweepTime = now
	}

	bucket, exists := limiter.buckets[client]
	if !exists {
		if len(limiter.buckets) >= limiter.maxClients {
			limiter.evictBucket(now)
		}
		bucket = rateBucket{tokens: limiter.burst, updated: now, lastSeen: now}
	}
	if elapsed := now.Sub(bucket.updated).Seconds(); elapsed > 0 {
		bucket.tokens = min(limiter.burst, bucket.tokens+elapsed*limiter.perSecond)
		bucket.updated = now
	}
	bucket.lastSeen = now
	required := float64(tokens)
	if bucket.tokens >= required {
		bucket.tokens -= required
		limiter.buckets[client] = bucket
		return true, 0
	}
	limiter.buckets[client] = bucket
	retrySeconds := (required - bucket.tokens) / limiter.perSecond
	return false, time.Duration(math.Ceil(retrySeconds * float64(time.Second)))
}

func (limiter *rateLimiter) evictBucket(now time.Time) {
	oldestKey := ""
	oldestSeen := now
	checked := 0
	for key, bucket := range limiter.buckets {
		if now.Sub(bucket.lastSeen) >= rateLimitClientTTL {
			delete(limiter.buckets, key)
			return
		}
		if oldestKey == "" || bucket.lastSeen.Before(oldestSeen) {
			oldestKey = key
			oldestSeen = bucket.lastSeen
		}
		checked++
		if checked >= rateLimitEvictionScan {
			break
		}
	}
	if oldestKey != "" {
		delete(limiter.buckets, oldestKey)
	}
}

func clientAddress(remoteAddress string) string {
	remoteAddress = strings.TrimSpace(remoteAddress)
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		if address := net.ParseIP(host); address != nil {
			return address.String()
		}
	}
	if address := net.ParseIP(remoteAddress); address != nil {
		return address.String()
	}
	return "unknown"
}

func retryAfterHeader(retryAfter time.Duration) string {
	seconds := int64(math.Ceil(retryAfter.Seconds()))
	return strconv.FormatInt(max(seconds, 1), 10)
}
