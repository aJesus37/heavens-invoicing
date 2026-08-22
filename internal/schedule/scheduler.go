// Package schedule runs the recurring-invoice scheduler and the overdue
// payment confirmation flow. A Scheduler performs one full pass per Tick
// and is safe for concurrent Tick calls; all mutable bookkeeping is
// in-memory by design (see the pending-map notes below).
package schedule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
)

// InvoiceStore is the slice of invoice storage the scheduler needs.
// *repo.InvoiceRepo satisfies it; tests use fakes.
type InvoiceStore interface {
	CloneFromTemplate(ctx context.Context, templateID string, issueDate, dueDate time.Time) (*model.Invoice, error)
	Get(ctx context.Context, id string) (*model.Invoice, error)
	ListByStatus(ctx context.Context, statuses ...string) ([]*model.Invoice, error)
	UpdateStatus(ctx context.Context, id, status string) error
	Delete(ctx context.Context, id string) error
}

// ClientStore loads the schedule/invoice clients.
type ClientStore interface {
	Get(ctx context.Context, id string) (*model.Client, error)
}

// RecurringStore lists active schedules and persists their advancement.
type RecurringStore interface {
	ListActive(ctx context.Context) ([]*model.RecurringSchedule, error)
	Update(ctx context.Context, s *model.RecurringSchedule) error
}

// Sender is the delivery surface, satisfied by *deliver.Router. Per the
// Router contract a send counts as delivered only when at least one
// returned ChannelResult carries a nil Err; a nil top-level error alone
// does not mean anything was sent (e.g. "all" with an unreachable client).
type Sender interface {
	SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte, method string) ([]deliver.ChannelResult, error)
	SendReminder(ctx context.Context, c model.Client, inv model.Invoice, method string) ([]deliver.ChannelResult, error)
}

// Notifier reaches the admin chat. Failures are logged, never fatal.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

const (
	// DefaultInterval is how often Run wakes up to Tick. Daily cadence is
	// enough for the domain, but several passes a day make restarts and
	// failures self-heal faster.
	DefaultInterval = 6 * time.Hour
	// DefaultGraceDays is how long past the due date an invoice may sit in
	// "sent" before the admin gets asked about it.
	DefaultGraceDays = 7
	// DefaultReminderAfter is how long the admin's confirmation stays open
	// before the client is reminded automatically.
	DefaultReminderAfter = 24 * time.Hour
	// dueDaysOut is the due date given to generated recurring invoices.
	dueDaysOut = 30
	// maxStableDay caps the day-of-month of advanced schedules; see
	// nextSendDate for why.
	maxStableDay = 28
)

// Scheduler fires recurring invoices and drives the overdue confirmation
// flow. The pending-confirmation map and the daily attempt guard are
// in-memory on purpose: a restart forgets both, so the worst case after a
// restart is the admin being asked again about an already-asked invoice
// (answering /paid still resolves it) — acceptable for this app's scale.
type Scheduler struct {
	recurring RecurringStore
	invoices  InvoiceStore
	clients   ClientStore
	router    Sender
	notifier  Notifier
	locale    func() i18n.Lang // admin-notification language resolver
	info      pdf.SenderInfo

	now           func() time.Time // injectable clock
	interval      time.Duration    // Run cadence
	graceDays     int              // overdue grace window in days
	reminderAfter time.Duration    // ask->remind delay
	renderPDF     func(w io.Writer, sender pdf.SenderInfo, c model.Client, inv model.Invoice) error

	mu sync.Mutex
	// pending tracks open admin confirmations: invoice ID -> asked-at time.
	pending map[string]time.Time
	// attempted deduplicates recurring fires per calendar day so a failing
	// delivery retries tomorrow rather than on every tick of today.
	attempted map[string]time.Time
}

// New wires a Scheduler around its dependencies. now must be non-nil;
// tuning knobs keep their defaults unless overridden afterwards. locale
// supplies the admin-notification language, resolved per use so settings
// changes apply immediately; nil (or an unsupported value) keeps pt-BR,
// matching the Router's fallback.
func New(recurring RecurringStore, invoices InvoiceStore, clients ClientStore, router Sender, notifier Notifier, locale func() i18n.Lang, info pdf.SenderInfo, now func() time.Time) *Scheduler {
	return &Scheduler{
		recurring:     recurring,
		invoices:      invoices,
		clients:       clients,
		router:        router,
		notifier:      notifier,
		locale:        locale,
		info:          info,
		now:           now,
		interval:      DefaultInterval,
		graceDays:     DefaultGraceDays,
		reminderAfter: DefaultReminderAfter,
		renderPDF:     pdf.RenderInvoice,
		pending:       make(map[string]time.Time),
		attempted:     make(map[string]time.Time),
	}
}

// lang resolves the current admin-facing language.
func (s *Scheduler) lang() i18n.Lang {
	if s.locale == nil {
		return i18n.PtBR
	}
	return i18n.ResolveSettings(string(s.locale()))
}

// Tick runs one full pass: fires due recurring schedules, then walks the
// overdue confirmation flow. Individual failures are logged, reported to
// the admin, and returned aggregated — one bad item never blocks the rest
// of the pass. Exported for tests and manual triggering.
func (s *Scheduler) Tick(ctx context.Context) error {
	today := startOfDay(s.now())
	var errs []error
	if err := s.fireDueSchedules(ctx, today); err != nil {
		errs = append(errs, err)
	}
	if err := s.processOverdue(ctx, today); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Run loops Tick every interval until ctx is done, logging (but not
// propagating) per-pass errors.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.tickAndLog(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tickAndLog(ctx)
		}
	}
}

func (s *Scheduler) tickAndLog(ctx context.Context) {
	if err := s.Tick(ctx); err != nil {
		log.Printf("scheduler: tick: %v", err)
	}
}

// --- job 1: recurring fire ---

// localDay reinterprets a stored timestamp as a calendar day in the
// scheduler clock's location. Dates arrive as UTC midnight (the API parses
// user-entered YYYY-MM-DD with time.Parse) while today is local midnight;
// comparing the raw instants shifts schedules up to a whole day on non-UTC
// hosts, so every day-granularity comparison normalizes first (same rule
// as AdminBot's /upcoming window).
func localDay(t time.Time, loc *time.Location) time.Time {
	return startOfDay(t.In(loc))
}

func (s *Scheduler) fireDueSchedules(ctx context.Context, today time.Time) error {
	schedules, err := s.recurring.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active recurring schedules: %w", err)
	}
	var errs []error
	for _, sched := range schedules {
		// Hard skip: paused schedules must generate nothing and notify
		// nothing. ListActive already filters them out, but the guard here
		// keeps the rule local to the fire loop so it can't drift.
		if !sched.Active {
			continue
		}
		if localDay(sched.NextSendDate, today.Location()).After(today) {
			continue // not due yet
		}
		if s.attemptedToday(sched.ID, today) {
			continue // already tried today; failures retry tomorrow
		}
		s.markAttempted(sched.ID, today)
		if err := s.fireSchedule(ctx, sched, today); err != nil {
			log.Printf("scheduler: recurring %s (%s): %v", sched.ID, sched.Frequency, err)
			s.notifyAdmin(ctx, i18n.T(s.lang(), "schedule.recurring_failed", sched.ID, sched.Frequency, err))
			errs = append(errs, fmt.Errorf("recurring schedule %s: %w", sched.ID, err))
		}
	}
	return errors.Join(errs...)
}

// fireSchedule clones the schedule's template invoice, renders it and hands
// it to the Router. Only a confirmed delivery advances the schedule; any
// earlier step fails with the clone left behind as a draft for inspection.
func (s *Scheduler) fireSchedule(ctx context.Context, sched *model.RecurringSchedule, today time.Time) error {
	next, ok := nextSendDate(today, sched.Frequency)
	if !ok {
		return fmt.Errorf("invalid frequency %q", sched.Frequency)
	}
	client, err := s.clients.Get(ctx, sched.ClientID)
	if err != nil {
		return fmt.Errorf("load client %s: %w", sched.ClientID, err)
	}
	inv, err := s.invoices.CloneFromTemplate(ctx, sched.InvoiceTemplateID, today, today.AddDate(0, 0, dueDaysOut))
	if err != nil {
		return fmt.Errorf("clone template %s: %w", sched.InvoiceTemplateID, err)
	}

	buf := &bytes.Buffer{}
	if err := s.renderPDF(buf, s.info, *client, *inv); err != nil {
		return fmt.Errorf("render invoice #%06d: %w", inv.Number, err)
	}

	results, err := s.router.SendInvoice(ctx, *client, *inv, buf.Bytes(), sched.DeliveryMethod)
	if err != nil {
		// Routing refused outright (e.g. the invoice is already paid) or
		// delivery succeeded but the "sent" flip did not persist: the clone
		// is an orphan we must not leave behind, so roll it back and do not
		// advance. The Router already told the admin what happened.
		s.rollbackClone(ctx, inv.ID)
		return fmt.Errorf("send invoice #%06d via %s: %w", inv.Number, sched.DeliveryMethod, err)
	}
	if !anyChannelOK(results) {
		// Delivery failed on every channel: dropping the clone here is what
		// stops a permanently-failing schedule from accumulating one orphan
		// draft per day. No advance, so it retries tomorrow with a fresh try.
		s.rollbackClone(ctx, inv.ID)
		return fmt.Errorf("invoice #%06d via %q failed on every channel (%s)",
			inv.Number, sched.DeliveryMethod, summarize(s.lang(), results))
	}

	// Advance from the fire day (not the stale NextSendDate) so periods
	// missed during downtime collapse into one invoice instead of stacking.
	sched.LastSentDate = &today
	sched.NextSendDate = next
	if err := s.recurring.Update(ctx, sched); err != nil {
		return fmt.Errorf("advance schedule after sending #%06d: %w", inv.Number, err)
	}
	return nil
}

// nextSendDate advances from by one frequency period. Weeklies add seven
// days. Month-based frequencies add calendar months with the day-of-month
// clamped to <=28 BEFORE constructing the date: AddDate-style normalization
// rolls overflow into the following month (Jan 31 + 1 month would become
// Mar 3), drifting the billing day on every short month; clamping keeps
// occurrences stable (Jan-31 monthly => Feb-28, Mar-31 => Apr-28, Jan-31
// yearly => Jan-28). Quarterly/yearly share the rule because they are month
// multiples with the same overflow hazard.
func nextSendDate(from time.Time, frequency string) (time.Time, bool) {
	y, m, d := from.Date()
	switch frequency {
	case "weekly":
		return from.AddDate(0, 0, 7), true
	case "monthly":
		return monthAdd(y, m+1, d, from.Location()), true
	case "quarterly":
		return monthAdd(y, m+3, d, from.Location()), true
	case "yearly":
		return monthAdd(y, m+12, d, from.Location()), true
	default:
		return time.Time{}, false
	}
}

func monthAdd(year int, month time.Month, day int, loc *time.Location) time.Time {
	if day > maxStableDay {
		day = maxStableDay
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// --- job 2: overdue confirmation flow ---

// processOverdue finds sent invoices past the grace window, asks the admin
// whether each was paid, and — once a confirmation stays unanswered longer
// than reminderAfter — reminds the client via every available channel and
// flags the invoice "overdue" so it exits the flow. Reminders use method
// "all" deliberately: invoices carry no delivery method (only recurring
// schedules do), so the honest fallback is to reach the client anywhere.
func (s *Scheduler) processOverdue(ctx context.Context, today time.Time) error {
	invoices, err := s.invoices.ListByStatus(ctx, "sent")
	if err != nil {
		return fmt.Errorf("list sent invoices: %w", err)
	}
	cutoff := today.AddDate(0, 0, -s.graceDays)

	type candidate struct {
		inv  *model.Invoice
		days int
	}
	var candidates []candidate
	candidateIDs := make(map[string]bool)
	for _, inv := range invoices {
		due := localDay(inv.DueDate, today.Location())
		if !due.Before(cutoff) {
			continue
		}
		candidates = append(candidates, candidate{inv: inv, days: int(today.Sub(due).Hours() / 24)})
		candidateIDs[inv.ID] = true
	}

	// Reconcile: confirmations whose invoice left the "sent" pool (paid,
	// cancelled, deleted) are resolved without action.
	s.mu.Lock()
	for id := range s.pending {
		if !candidateIDs[id] {
			delete(s.pending, id)
		}
	}
	s.mu.Unlock()

	var errs []error
	for _, c := range candidates {
		if err := s.handleOverdue(ctx, c.inv, c.days, today); err != nil {
			log.Printf("scheduler: overdue invoice %s (#%06d): %v", c.inv.ID, c.inv.Number, err)
			errs = append(errs, fmt.Errorf("overdue invoice %s: %w", c.inv.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) handleOverdue(ctx context.Context, inv *model.Invoice, daysOverdue int, now time.Time) error {
	s.mu.Lock()
	askedAt, asked := s.pending[inv.ID]
	s.mu.Unlock()

	if !asked {
		client, err := s.clients.Get(ctx, inv.ClientID)
		if err != nil {
			return fmt.Errorf("load client %s: %w", inv.ClientID, err)
		}
		s.notifyAdmin(ctx, i18n.T(s.lang(), "schedule.overdue_ask",
			inv.Number, pdf.FormatBRL(inv.Total), client.Name, daysOverdue, inv.Number))
		s.mu.Lock()
		s.pending[inv.ID] = now
		s.mu.Unlock()
		return nil
	}

	if now.Sub(askedAt) < s.reminderAfter {
		return nil // confirmation still open, keep waiting
	}

	// Re-check before reminding: the admin may have answered /paid between
	// listing and now, or the status moved some other way.
	current, err := s.invoices.Get(ctx, inv.ID)
	if err != nil {
		return fmt.Errorf("reload invoice: %w", err)
	}
	if current.Status != "sent" {
		s.forget(inv.ID)
		return nil
	}
	client, err := s.clients.Get(ctx, inv.ClientID)
	if err != nil {
		return fmt.Errorf("load client %s: %w", inv.ClientID, err)
	}

	results, err := s.router.SendReminder(ctx, *client, *current, deliver.MethodAll)
	if err != nil || !anyChannelOK(results) {
		// Reset the confirmation clock: retry after another full window
		// instead of poking the client on every tick. The admin sees the
		// aggregated tick error.
		s.mu.Lock()
		s.pending[inv.ID] = now
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("send reminder: %w", err)
		}
		return fmt.Errorf("reminder #%06d failed on every channel (%s)", current.Number, summarize(s.lang(), results))
	}
	if err := s.invoices.UpdateStatus(ctx, inv.ID, "overdue"); err != nil {
		return fmt.Errorf("mark overdue after reminder: %w", err)
	}
	s.forget(inv.ID)
	return nil
}

// --- shared helpers ---

func (s *Scheduler) attemptedToday(id string, today time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.attempted[id]
	return ok && last.Equal(today)
}

func (s *Scheduler) markAttempted(id string, today time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempted[id] = today
}

func (s *Scheduler) forget(invoiceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, invoiceID)
}

// rollbackClone removes the draft a failed fire cloned from the template, so
// a delivery failure leaves no orphan invoice behind. A delete error is
// logged but does not mask the original failure.
func (s *Scheduler) rollbackClone(ctx context.Context, id string) {
	if err := s.invoices.Delete(ctx, id); err != nil {
		log.Printf("scheduler: rollback orphan draft %s: %v", id, err)
	}
}

// notifyAdmin is best-effort: notification problems are logged and dropped
// so they cannot mask or duplicate the underlying outcome.
func (s *Scheduler) notifyAdmin(ctx context.Context, text string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.Notify(context.WithoutCancel(ctx), text); err != nil {
		log.Printf("scheduler: notify admin: %v", err)
	}
}

func anyChannelOK(results []deliver.ChannelResult) bool {
	for _, r := range results {
		if r.Err == nil {
			return true
		}
	}
	return false
}

func summarize(lang i18n.Lang, results []deliver.ChannelResult) string {
	if len(results) == 0 {
		return i18n.T(lang, "schedule.no_channels")
	}
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.Err == nil {
			parts = append(parts, r.Channel+": ok")
			continue
		}
		parts = append(parts, r.Channel+": "+r.Err.Error())
	}
	return strings.Join(parts, "; ")
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
