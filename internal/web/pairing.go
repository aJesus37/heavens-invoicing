package web

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/whatsapp"
)

// reconnectDelay gives the post-PairSuccess server-side disconnect time to
// land before the fresh session dials back in.
const reconnectDelay = 2 * time.Second

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
// latest code is what the qr.png endpoint encodes. Session I/O never runs
// under mu: the settings tab polls snapshot() through it every few
// seconds and must not queue behind slow network dials.
type pairingManager struct {
	mu     sync.Mutex
	sess   *whatsapp.Session
	state  string
	qr     string
	errMsg string
	cancel context.CancelFunc
	gen    int // attempt generation; stale pumps must not touch newer runs
}

func newPairing(sess *whatsapp.Session) *pairingManager {
	return &pairingManager{sess: sess, state: pairIdle}
}

// start kicks off one pairing attempt. It is safe to call repeatedly: an
// in-flight attempt is never restarted, and paired sessions short-circuit.
// The state decision happens under mu; the blocking session I/O runs in a
// background goroutine so the HTTP handler returns the fragment at once
// instead of queueing behind a slow dial. lang localizes the failure
// messages recorded for the settings fragment.
func (p *pairingManager) start(lang i18n.Lang) {
	p.mu.Lock()
	if p.sess == nil || p.state == pairPending {
		p.mu.Unlock()
		return
	}
	if p.sess.IsPaired() {
		p.state = pairConnected
		p.mu.Unlock()
		return
	}
	p.stopLocked()

	// Reserve the attempt before releasing mu: pairPending doubles as the
	// in-flight marker, so concurrent callers never start a second
	// consumer loop while this one dials.
	p.gen++
	gen := p.gen
	p.state = pairPending
	p.qr = ""
	p.errMsg = ""
	p.mu.Unlock()

	go p.attempt(gen, lang)
}

// attempt runs the blocking half of a pairing attempt with no lock held.
//
// The context handed to Connect must stay alive for the whole pairing:
// whatsmeow keeps using it for the connection's keep-alive and handler
// loops after ConnectContext returns. Cancelling it once the dial
// completes — the tempting "bound only the dial" pattern — silently tears
// the socket down a few hundred milliseconds later, before WhatsApp ever
// sends the QR codes, and the stream then ends as a bare "timeout".
func (p *pairingManager) attempt(gen int, lang i18n.Lang) {
	// A connect left over from boot blocks GetQRChannel; drop it first.
	if p.sess.IsConnected() {
		p.sess.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.sess.QRChannel(ctx)
	if err != nil {
		cancel()
		p.fail(err.Error())
		return
	}
	if err := p.sess.Connect(ctx); err != nil {
		cancel()
		p.fail(i18n.T(lang, "wa.err_connection", err))
		return
	}

	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			p.mu.Lock()
			// Only expire our own attempt: a stale pump from a superseded
			// run must not clobber a newer in-flight one.
			if p.gen == gen && p.state == pairPending {
				p.state = pairFailed
				p.errMsg = i18n.T(lang, "wa.err_expired")
			}
			p.mu.Unlock()
		}()
		for item := range ch {
			switch {
			case item.Event == "code":
				p.mu.Lock()
				if p.gen == gen {
					p.qr = item.Code
				}
				p.mu.Unlock()
			case item.Event == "success":
				p.mu.Lock()
				if p.gen == gen {
					p.qr = ""
					p.state = pairConnected
				}
				p.mu.Unlock()
				go p.reconnectAfterPair()
				return
			case item.Event == "error":
				p.mu.Lock()
				if p.gen == gen {
					p.qr = ""
					p.state = pairFailed
					p.errMsg = fmt.Sprint(item.Error)
				}
				p.mu.Unlock()
				return
			default:
				// Final events ("timeout", "err-*"): keep the generic
				// expired message unless something more specific arrived.
				p.mu.Lock()
				if p.gen == gen {
					p.qr = ""
					if p.state == pairPending {
						p.state = pairFailed
						p.errMsg = item.Event
					}
				}
				p.mu.Unlock()
				return
			}
		}
	}()
}

// fail records a synchronous setup/connect failure. Callers must not hold
// mu.
func (p *pairingManager) fail(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.qr = ""
	p.state = pairFailed
	p.errMsg = msg
}

// reconnectAfterPair establishes the real session once linking succeeds.
// WhatsApp disconnects the companion immediately after PairSuccess, and
// the QR stream's context dies with the pump, so without this explicit
// reconnect the app would report "connected" while every send failed as
// not connected.
func (p *pairingManager) reconnectAfterPair() {
	time.Sleep(reconnectDelay)
	if p.sess == nil {
		return
	}
	p.sess.Close()
	if err := p.sess.Connect(context.Background()); err != nil {
		log.Printf("web: whatsapp reconnect after pairing failed (sends will retry on use): %v", err)
	}
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
