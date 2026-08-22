package deliver

import (
	"context"
	"fmt"
	"strings"

	"github.com/jesus/invoice-app/internal/model"
)

// ChannelResult reports the outcome of one delivery attempt.
type ChannelResult struct {
	Channel string // email|whatsapp|telegram
	Err     error  // nil on success
}

// Delivery method names shared with callers of the Router.
const (
	MethodEmail    = "email"
	MethodWhatsApp = "whatsapp"
	MethodTelegram = "telegram"
	MethodAll      = "all"
)

// InvoiceStatusUpdater is the slice of invoice storage the Router needs to
// flip an invoice to "sent". *repo.InvoiceRepo satisfies it.
type InvoiceStatusUpdater interface {
	UpdateStatus(ctx context.Context, id, status string) error
}

// Notifier receives admin-facing delivery summaries (e.g. Telegram).
// Implementations may be silent no-ops when unconfigured.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// channelTarget pairs a deliverer with the channel name used in results.
type channelTarget struct {
	name      string
	deliverer Deliverer
}

// Router orchestrates invoice/reminder delivery across channels: it picks
// the target channels for a delivery method, collects per-channel results,
// marks the invoice sent when at least one channel succeeds, and reports
// every outcome to the admin notifier. Deliverers may be nil; routing to a
// nil deliverer yields a "not configured" error result instead of a panic.
type Router struct {
	invoices InvoiceStatusUpdater // required when SendInvoice can succeed
	notifier Notifier             // optional; nil skips notifications
	email    Deliverer
	whatsapp Deliverer
	telegram Deliverer
}

func NewRouter(invoices InvoiceStatusUpdater, notifier Notifier, email, whatsapp, telegram Deliverer) *Router {
	return &Router{
		invoices: invoices,
		notifier: notifier,
		email:    email,
		whatsapp: whatsapp,
		telegram: telegram,
	}
}

// SendInvoice delivers the rendered invoice PDF through the requested
// method and marks it "sent" when at least one attempted channel succeeds.
// Paid invoices are rejected up front: resending must never downgrade
// their status back to "sent". Drafts are accepted (the scheduler sends
// freshly cloned drafts).
func (r *Router) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte, method string) ([]ChannelResult, error) {
	if inv.Status == "paid" {
		return nil, fmt.Errorf("fatura já está paga")
	}
	targets, err := r.targets(c, method)
	if err != nil {
		return nil, fmt.Errorf("send invoice #%06d: %w", inv.Number, err)
	}
	return r.run(ctx, c, inv, targets, invoiceKind, true, func(d Deliverer) error {
		return d.SendInvoice(ctx, c, inv, pdf)
	})
}

// SendReminder behaves like SendInvoice but never touches invoice status.
func (r *Router) SendReminder(ctx context.Context, c model.Client, inv model.Invoice, method string) ([]ChannelResult, error) {
	targets, err := r.targets(c, method)
	if err != nil {
		return nil, fmt.Errorf("send reminder #%06d: %w", inv.Number, err)
	}
	return r.run(ctx, c, inv, targets, reminderKind, false, func(d Deliverer) error {
		return d.SendReminder(ctx, c, inv)
	})
}

// summaryKind supplies the admin-summary wording, which differs between
// invoices ("Fatura ... enviada") and reminders ("Lembrete ... enviado").
type summaryKind struct {
	noun string
	sent string
}

var (
	invoiceKind  = summaryKind{noun: "Fatura", sent: "enviada"}
	reminderKind = summaryKind{noun: "Lembrete da fatura", sent: "enviado"}
)

// targets resolves which channels a delivery attempts. A single method
// always attempts its channel — the deliverer itself reports a client
// missing the matching address — while "all" only attempts channels the
// client can actually receive on. Only attempted channels appear in the
// results.
func (r *Router) targets(c model.Client, method string) ([]channelTarget, error) {
	switch method {
	case MethodEmail:
		return []channelTarget{{MethodEmail, r.email}}, nil
	case MethodWhatsApp:
		return []channelTarget{{MethodWhatsApp, r.whatsapp}}, nil
	case MethodTelegram:
		return []channelTarget{{MethodTelegram, r.telegram}}, nil
	case MethodAll:
		var ts []channelTarget
		if hasText(c.Email) {
			ts = append(ts, channelTarget{MethodEmail, r.email})
		}
		if hasText(c.Phone) {
			ts = append(ts, channelTarget{MethodWhatsApp, r.whatsapp})
		}
		if hasText(c.TelegramChatID) {
			ts = append(ts, channelTarget{MethodTelegram, r.telegram})
		}
		return ts, nil
	default:
		return nil, fmt.Errorf("unknown delivery method %q (valid: %s, %s, %s, %s)", method, MethodEmail, MethodWhatsApp, MethodTelegram, MethodAll)
	}
}

func (r *Router) run(ctx context.Context, c model.Client, inv model.Invoice, targets []channelTarget, kind summaryKind, updateStatus bool, send func(Deliverer) error) ([]ChannelResult, error) {
	results := make([]ChannelResult, 0, len(targets))
	for _, t := range targets {
		var err error
		if t.deliverer == nil {
			err = fmt.Errorf("not configured")
		} else {
			err = send(t.deliverer)
		}
		results = append(results, ChannelResult{Channel: t.name, Err: err})
	}

	var ok []string
	for _, res := range results {
		if res.Err == nil {
			ok = append(ok, res.Channel)
		}
	}

	// The status flip happens before notifying so the admin summary
	// reflects the invoice's final persisted state.
	var statusErr error
	if updateStatus && len(ok) > 0 {
		if err := r.invoices.UpdateStatus(ctx, inv.ID, "sent"); err != nil {
			statusErr = fmt.Errorf("mark invoice #%06d sent: %w", inv.Number, err)
		}
	}

	r.notifyOutcome(ctx, inv.Number, c.Name, kind, results, ok, statusErr)

	return results, statusErr
}

// notifyOutcome sends the admin summary; notification failures are ignored
// on purpose so a broken admin channel cannot mask or alter delivery
// results.
func (r *Router) notifyOutcome(ctx context.Context, number int64, clientName string, kind summaryKind, results []ChannelResult, ok []string, statusErr error) {
	if r.notifier == nil {
		return
	}
	_ = r.notifier.Notify(ctx, outcomeText(number, clientName, kind, results, ok, statusErr))
}

func outcomeText(number int64, clientName string, kind summaryKind, results []ChannelResult, ok []string, statusErr error) string {
	numbered := fmt.Sprintf("%s #%06d", kind.noun, number)
	if len(ok) > 0 {
		text := numbered + fmt.Sprintf(" %s para %s via %s", kind.sent, clientName, strings.Join(ok, ", "))
		if statusErr != nil {
			text += ", mas falha ao marcar como enviada: " + statusErr.Error()
		}
		return text
	}
	numbered += fmt.Sprintf(" para %s falhou", clientName)
	if len(results) == 0 {
		return numbered + ": cliente sem canal de envio configurado"
	}
	parts := make([]string, 0, len(results))
	for _, res := range results {
		parts = append(parts, res.Channel+": "+res.Err.Error())
	}
	return numbered + ": " + strings.Join(parts, "; ")
}

func hasText(p *string) bool {
	return p != nil && strings.TrimSpace(*p) != ""
}
