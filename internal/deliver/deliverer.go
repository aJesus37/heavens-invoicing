package deliver

import (
	"context"

	"github.com/jesus/invoice-app/internal/model"
)

// Deliverer sends invoice documents and payment reminders to a client
// through a specific channel.
type Deliverer interface {
	Name() string
	SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error
	SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error
}
