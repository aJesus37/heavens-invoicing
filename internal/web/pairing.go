package web

import (
	"context"
	"fmt"
	"sync"

	"github.com/jesus/invoice-app/internal/whatsapp"
)

// Pairing states surfaced by the settings fragment.
const (
	pairIdle      = "idle"      // unpaired, no attempt running
	pairPending   = "pairing"   // QR stream active, waiting for a scan
	pairConnected = "connected" // device linked
	pairFailed    = "failed"    // last attempt errored or timed out
)

// pairingManager drives the WhatsApp link flow for the settings page.
//
// whatsmeow's GetQRChannel only works on an unpaired, disconnected client,
// so start() drops any socket left over from boot before requesting the
// QR stream and reconnecting. A single background goroutine consumes the
// channel (codes rotate every ~20s) until success, timeout or error; the
// latest code is what the qr.png endpoint encodes.
type pairingManager struct {
	mu     sync.Mutex
	sess   *whatsapp.Session
	state  string
	qr     string
	errMsg string
	cancel context.CancelFunc
}

func newPairing(sess *whatsapp.Session) *pairingManager {
	return &pairingManager{sess: sess, state: pairIdle}
}

// start kicks off one pairing attempt. It is safe to call repeatedly: an
// in-flight attempt is never restarted, and paired sessions short-circuit.
func (p *pairingManager) start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.sess == nil || p.state == pairPending {
		return
	}
	if p.sess.IsPaired() {
		p.state = pairConnected
		return
	}
	p.stopLocked()

	// A connect from boot blocks GetQRChannel; drop it first.
	if p.sess.IsConnected() {
		p.sess.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.sess.QRChannel(ctx)
	if err != nil {
		cancel()
		p.state = pairFailed
		p.errMsg = err.Error()
		return
	}
	if err := p.sess.Connect(ctx); err != nil {
		cancel()
		p.state = pairFailed
		p.errMsg = fmt.Sprintf("conexão: %v", err)
		return
	}

	p.state = pairPending
	p.qr = ""
	p.errMsg = ""
	p.cancel = cancel

	go func() {
		defer func() {
			cancel()
			p.mu.Lock()
			if p.state == pairPending {
				p.state = pairFailed
				p.errMsg = "pareamento expirou"
			}
			p.mu.Unlock()
		}()
		for item := range ch {
			switch {
			case item.Event == "code":
				p.mu.Lock()
				p.qr = item.Code
				p.mu.Unlock()
			case item.Event == "success":
				p.mu.Lock()
				p.qr = ""
				p.state = pairConnected
				p.mu.Unlock()
				return
			case item.Event == "error":
				p.mu.Lock()
				p.qr = ""
				p.state = pairFailed
				p.errMsg = fmt.Sprint(item.Error)
				p.mu.Unlock()
				return
			default:
				// Final events ("timeout", "err-*"): keep the generic
				// expired message unless something more specific arrived.
				p.mu.Lock()
				p.qr = ""
				if p.state == pairPending {
					p.state = pairFailed
					p.errMsg = item.Event
				}
				p.mu.Unlock()
				return
			}
		}
	}()
}

// snapshot returns the current state, pending QR code ("" when none) and
// failure message for rendering.
func (p *pairingManager) snapshot() (state, qr, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, p.qr, p.errMsg
}

// stopLocked cancels an in-flight attempt without touching the session;
// caller holds mu. Closing the QR channel makes the pump goroutine exit.
func (p *pairingManager) stopLocked() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}
