package auth_test

import (
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/auth"
)

// authMaxFailures mirrors the exported policy constant.
func authMaxFailures() int { return auth.MaxLoginFailures }

func TestLoginLimiterLocksAfterFiveFailures(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	mgr, _, _ := newManagerAt(t, clock)

	const ip = "192.168.1.50:54321"

	for i := 0; i < 4; i++ {
		if !mgr.AllowLogin(ip) {
			t.Fatalf("attempt %d must be allowed", i+1)
		}
		mgr.LoginFailure(ip)
	}
	if !mgr.AllowLogin(ip) {
		t.Fatal("the 5th attempt itself is still allowed before its failure")
	}
	mgr.LoginFailure(ip)

	if mgr.AllowLogin(ip) {
		t.Fatal("after 5 failures the IP must be locked out")
	}

	// 59 seconds in: still locked.
	now = now.Add(59 * time.Second)
	if mgr.AllowLogin(ip) {
		t.Fatal("lockout must survive 59s")
	}

	// At the 60-second mark the lockout expires and the counter resets,
	// so a correct password can get through again.
	now = now.Add(time.Second)
	if !mgr.AllowLogin(ip) {
		t.Fatal("lockout must expire after a full minute")
	}
}

func TestLoginLimiterSuccessResetsCounter(t *testing.T) {
	mgr, _, _ := newManager(t)
	const ip = "10.0.0.9:1234"

	for i := 0; i < 4; i++ {
		mgr.LoginFailure(ip)
	}
	mgr.LoginSuccess(ip)

	for i := 0; i < authMaxFailures(); i++ {
		if !mgr.AllowLogin(ip) {
			t.Fatalf("post-success attempt %d must be allowed (counter should have reset)", i+1)
		}
		mgr.LoginFailure(ip)
	}
	if mgr.AllowLogin(ip) {
		t.Fatal("a fresh run of max failures must still lock out")
	}
}

func TestLoginLimiterPerIPIndependence(t *testing.T) {
	mgr, _, _ := newManager(t)
	const attacker = "203.0.113.7:9999"
	const victim = "198.51.100.3:7777"

	for i := 0; i < 10; i++ {
		mgr.LoginFailure(attacker)
	}
	if mgr.AllowLogin(attacker) {
		t.Fatal("attacker IP must be locked")
	}
	if !mgr.AllowLogin(victim) {
		t.Fatal("other IPs must be unaffected by another IP's lockout")
	}
}

func TestLoginLimiterUnknownIPAllowed(t *testing.T) {
	mgr, _, _ := newManager(t)
	if !mgr.AllowLogin("never-seen:1") {
		t.Fatal("unknown IP must be allowed")
	}
}
