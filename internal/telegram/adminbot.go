package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
)

// InvoiceRepo is the slice of invoice storage the admin bot commands need.
// *repo.InvoiceRepo satisfies it; tests use fakes.
type InvoiceRepo interface {
	GetByNumber(ctx context.Context, number int64) (*model.Invoice, error)
	UpdateStatus(ctx context.Context, id, status string) error
	ListByStatus(ctx context.Context, statuses ...string) ([]*model.Invoice, error)
}

// ClientRepo is the slice of client storage the admin bot commands need.
type ClientRepo interface {
	List(ctx context.Context) ([]*model.Client, error)
}

// BotAPI is what AdminBot needs from a Telegram client: replying and polling.
// *Client satisfies it.
type BotAPI interface {
	MessageSender
	GetUpdates(ctx context.Context, offset int64) ([]Update, error)
}

const helpText = `Comandos:
/paid <número> - marca a fatura como paga
/status - faturas pendentes
/upcoming - vencimentos dos próximos 7 dias
/clients - lista de clientes`

const upcomingEmptyText = "Nenhuma fatura vence nos próximos 7 dias."

// AdminBot answers commands from the admin chat and polls Telegram for
// updates. All dependencies are injected; Handle carries the pure command logic.
type AdminBot struct {
	api         BotAPI
	adminChatID string
	invoices    InvoiceRepo
	clients     ClientRepo

	now      func() time.Time // injectable clock
	interval time.Duration    // poll interval

	lastOffset int64 // highest update_id already seen
}

func NewAdminBot(api BotAPI, adminChatID string, invoices InvoiceRepo, clients ClientRepo) *AdminBot {
	return &AdminBot{
		api:         api,
		adminChatID: strings.TrimSpace(adminChatID),
		invoices:    invoices,
		clients:     clients,
		now:         time.Now,
		interval:    3 * time.Second,
	}
}

// Handle processes one incoming message text and returns the reply string.
// Exported so the scheduler can reuse it for pending confirmations.
func (b *AdminBot) Handle(ctx context.Context, text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return helpText
	}
	switch strings.ToLower(fields[0]) {
	case "/paid":
		if len(fields) != 2 {
			return "Uso: /paid <número da fatura>"
		}
		return b.handlePaid(ctx, fields[1])
	case "/status":
		return b.handlePending(ctx, "Nenhuma fatura pendente.")
	case "/upcoming":
		return b.handleUpcoming(ctx)
	case "/clients":
		return b.handleClients(ctx)
	default:
		return helpText
	}
}

func (b *AdminBot) handlePaid(ctx context.Context, arg string) string {
	number, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || number < 1 {
		return fmt.Sprintf("Número de fatura inválido: %q", arg)
	}
	inv, err := b.invoices.GetByNumber(ctx, number)
	if errors.Is(err, repo.ErrNotFound) {
		return fmt.Sprintf("Fatura #%d não encontrada", number)
	}
	if err != nil {
		return fmt.Sprintf("Erro ao buscar a fatura #%d: %v", number, err)
	}
	if err := b.invoices.UpdateStatus(ctx, inv.ID, "paid"); err != nil {
		return fmt.Sprintf("Erro ao marcar a fatura #%d como paga: %v", number, err)
	}
	return fmt.Sprintf("Fatura #%d marcada como paga ✓", number)
}

func (b *AdminBot) handlePending(ctx context.Context, emptyReply string) string {
	invoices, err := b.invoices.ListByStatus(ctx, "sent", "overdue")
	if err != nil {
		return fmt.Sprintf("Erro ao listar faturas pendentes: %v", err)
	}
	if len(invoices) == 0 {
		return emptyReply
	}
	lines := make([]string, 0, len(invoices))
	for _, inv := range invoices {
		lines = append(lines, invoiceLine(inv))
	}
	return strings.Join(lines, "\n")
}

func (b *AdminBot) handleUpcoming(ctx context.Context) string {
	invoices, err := b.invoices.ListByStatus(ctx, "sent", "overdue")
	if err != nil {
		return fmt.Sprintf("Erro ao listar faturas pendentes: %v", err)
	}
	nowLoc := b.now()
	today := startOfDay(nowLoc)
	limit := today.AddDate(0, 0, 8) // exclusive upper bound: keeps today..today+7

	var lines []string
	for _, inv := range invoices {
		due := startOfDay(inv.DueDate.In(nowLoc.Location()))
		if due.Before(today) || !due.Before(limit) {
			continue
		}
		lines = append(lines, invoiceLine(inv))
	}
	if len(lines) == 0 {
		return upcomingEmptyText
	}
	return strings.Join(lines, "\n")
}

func (b *AdminBot) handleClients(ctx context.Context) string {
	clients, err := b.clients.List(ctx)
	if err != nil {
		return fmt.Sprintf("Erro ao listar clientes: %v", err)
	}
	if len(clients) == 0 {
		return "Nenhum cliente cadastrado."
	}
	names := make([]string, 0, len(clients))
	for _, c := range clients {
		names = append(names, c.Name)
	}
	return strings.Join(names, "\n")
}

func invoiceLine(inv *model.Invoice) string {
	return fmt.Sprintf("Fatura #%d - %s - venc. %s - %s",
		inv.Number, pdf.FormatBRL(inv.Total), inv.DueDate.Format("02/01"), inv.Status)
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// processUpdate handles one polled update: non-message updates and foreign
// chats are ignored silently, messages from the admin chat are answered via
// SendMessage with Handle's reply.
func (b *AdminBot) processUpdate(ctx context.Context, u Update) (bool, error) {
	if u.Message == nil || b.adminChatID == "" {
		return false, nil
	}
	chatID := strconv.FormatInt(u.Message.Chat.ID, 10)
	if chatID != b.adminChatID {
		return false, nil
	}
	reply := b.Handle(ctx, u.Message.Text)
	if err := b.api.SendMessage(ctx, chatID, reply); err != nil {
		return false, fmt.Errorf("telegram sendMessage: %w", err)
	}
	return true, nil
}

// Run polls GetUpdates every interval until ctx is done. The offset advances
// past every received update (including ignored ones) to avoid refetching.
func (b *AdminBot) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := b.api.GetUpdates(ctx, b.lastOffset+1)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("telegram getUpdates: %w", err)
		}
		for _, u := range updates {
			if _, err := b.processUpdate(ctx, u); err != nil {
				return err
			}
			if u.UpdateID > b.lastOffset {
				b.lastOffset = u.UpdateID
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.interval):
		}
	}
}
