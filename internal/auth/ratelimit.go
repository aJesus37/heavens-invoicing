package auth

import (
	"sync"
	"time"
)

const (
	// MaxLoginFailures is how many failed password attempts are tolerated
	// from one address before the lockout kicks in.
	MaxLoginFailures = 5
	// loginLockout is how long an exhausted address stays locked out.
	loginLockout = time.Minute
)

// loginLimiter is an in-memory, per-IP throttle for POST /login. State is
// process-local by design: a restart clears it, which is acceptable for a
// single-admin homelab app and avoids adding storage for attacker data.
type loginLimiter struct {
	mu    sync.Mutex
	now   func() time.Time
	fails map[string]*loginFails
}

type loginFails struct {
	count int
	until time.Time // zero while not locked out
}

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{now: now, fails: map[string]*loginFails{}}
}

// allow reports whether the key may attempt a login. A lockout whose window
// has passed resets the record entirely — the next failure starts a fresh
// count instead of instantly re-locking.
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.fails[key]
	if !ok {
		return true
	}
	if !rec.until.IsZero() {
		if l.now().Before(rec.until) {
			return false
		}
		delete(l.fails, key)
	}
	return true
}

// failure bumps the counter; reaching MaxLoginFailures starts the lockout.
// Failures recorded during an active lockout do not extend it.
func (l *loginLimiter) failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec := l.fails[key]
	if rec == nil {
		rec = &loginFails{}
		l.fails[key] = rec
	}
	if !rec.until.IsZero() {
		return // already locked; wait it out
	}
	rec.count++
	if rec.count >= MaxLoginFailures {
		rec.until = l.now().Add(loginLockout)
	}
}

// success forgets the key after a good login.
func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
