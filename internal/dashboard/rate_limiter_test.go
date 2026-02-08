package dashboard

import (
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Minute)

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		if !rl.Allow("user1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should fail
	if rl.Allow("user1") {
		t.Error("4th request should be rate limited")
	}

	// Different user should succeed
	if !rl.Allow("user2") {
		t.Error("different user should not be limited")
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	// Use a short window for testing
	rl := NewRateLimiter(2, 100*time.Millisecond)

	// Use up the tokens
	rl.Allow("user1")
	rl.Allow("user1")

	// Next should fail
	if rl.Allow("user1") {
		t.Error("should be rate limited")
	}

	// Wait for window to reset
	time.Sleep(150 * time.Millisecond)

	// Should succeed again after reset
	if !rl.Allow("user1") {
		t.Error("should be allowed after window reset")
	}
}

func TestRateLimiter_MultipleKeys(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute)

	// User1 uses 2 tokens
	rl.Allow("user1")
	rl.Allow("user1")

	// User1 should be limited
	if rl.Allow("user1") {
		t.Error("user1 should be rate limited")
	}

	// User2 should still have tokens
	if !rl.Allow("user2") {
		t.Error("user2 should not be limited")
	}

	// User3 should also have tokens
	if !rl.Allow("user3") {
		t.Error("user3 should not be limited")
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Second)

	// Test concurrent access to same key
	results := make(chan bool, 20)

	for i := 0; i < 20; i++ {
		go func() {
			results <- rl.Allow("concurrent-user")
		}()
	}

	// Collect results
	allowed := 0
	denied := 0
	for i := 0; i < 20; i++ {
		if <-results {
			allowed++
		} else {
			denied++
		}
	}

	// Should allow exactly 10 (the rate limit)
	if allowed != 10 {
		t.Errorf("expected 10 allowed, got %d", allowed)
	}
	if denied != 10 {
		t.Errorf("expected 10 denied, got %d", denied)
	}
}

func TestRateLimiter_ZeroRate(t *testing.T) {
	rl := NewRateLimiter(0, 1*time.Minute)

	// With zero rate, first request succeeds (bucket reset), then all subsequent fail
	if !rl.Allow("user1") {
		t.Error("first request should succeed (bucket initialization)")
	}

	// All subsequent requests should be denied
	if rl.Allow("user1") {
		t.Error("second request should be denied with zero rate")
	}
	if rl.Allow("user1") {
		t.Error("third request should be denied with zero rate")
	}
}

func TestRateLimiter_HighRate(t *testing.T) {
	rl := NewRateLimiter(1000, 1*time.Minute)

	// Should allow many requests
	for i := 0; i < 1000; i++ {
		if !rl.Allow("user1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 1001st should fail
	if rl.Allow("user1") {
		t.Error("should be rate limited after 1000 requests")
	}
}

func TestRateLimiter_BucketReset(t *testing.T) {
	rl := NewRateLimiter(5, 200*time.Millisecond)

	// Use all tokens
	for i := 0; i < 5; i++ {
		rl.Allow("user1")
	}

	// Should be limited
	if rl.Allow("user1") {
		t.Error("should be limited")
	}

	// Wait for reset
	time.Sleep(250 * time.Millisecond)

	// Should have full tokens again
	for i := 0; i < 5; i++ {
		if !rl.Allow("user1") {
			t.Errorf("token %d should be available after reset", i+1)
		}
	}
}

func TestRateLimiter_PartialConsumption(t *testing.T) {
	rl := NewRateLimiter(5, 1*time.Minute)

	// Use only 3 tokens
	rl.Allow("user1")
	rl.Allow("user1")
	rl.Allow("user1")

	// Should still have 2 tokens left
	if !rl.Allow("user1") {
		t.Error("should still have tokens available")
	}
	if !rl.Allow("user1") {
		t.Error("should still have tokens available")
	}

	// Now should be limited
	if rl.Allow("user1") {
		t.Error("should be limited after using all tokens")
	}
}

func TestRateLimiter_EmptyKey(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Minute)

	// Empty key should still work
	if !rl.Allow("") {
		t.Error("empty key should be allowed")
	}
}

func TestRateLimiter_IPAddressKeys(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute)

	// Simulate different IP addresses
	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"

	// Each IP gets its own bucket
	rl.Allow(ip1)
	rl.Allow(ip1)
	rl.Allow(ip2)
	rl.Allow(ip2)

	// Both should be limited now
	if rl.Allow(ip1) {
		t.Error("ip1 should be limited")
	}
	if rl.Allow(ip2) {
		t.Error("ip2 should be limited")
	}
}
