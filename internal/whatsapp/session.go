package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"mime"
	"path/filepath"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// Session wraps a whatsmeow client and its device store. The device
// identity lives in the same SQLite database as the rest of the app (in
// tables managed by whatsmeow's own upgrade system), so a pairing done
// once survives application restarts.
type Session struct {
	container *sqlstore.Container
	client    *whatsmeow.Client
}

// NewSession creates a WhatsApp session backed by the given database
// handle. It does not connect; call Connect for that.
func NewSession(ctx context.Context, db *sql.DB, logs waLog.Logger) (*Session, error) {
	container := sqlstore.NewWithDB(db, "sqlite3", logs)
	if err := container.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("whatsapp store upgrade: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsapp device load: %w", err)
	}
	return &Session{
		container: container,
		client:    whatsmeow.NewClient(device, logs),
	}, nil
}

// Connect establishes the connection to WhatsApp. It is a no-op when the
// client is already connected.
func (s *Session) Connect(ctx context.Context) error {
	if s.client.IsConnected() {
		return nil
	}
	return s.client.ConnectContext(ctx)
}

func (s *Session) IsConnected() bool {
	return s.client.IsConnected()
}

// QRChannel exposes the pairing QR code stream. It only works before the
// first successful Connect of an unpaired device.
func (s *Session) QRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return s.client.GetQRChannel(ctx)
}

// WaitPairCompleted blocks until the device pairing succeeds (PairSuccess)
// or the connection is fully established after a previous pairing
// (Connected), or the context is cancelled.
func (s *Session) WaitPairCompleted(ctx context.Context) error {
	evts := make(chan any, 2)
	id := s.client.AddEventHandler(func(evt any) {
		switch evt.(type) {
		case *events.PairSuccess, *events.Connected:
			select {
			case evts <- evt:
			default:
			}
		}
	})
	defer s.client.RemoveEventHandler(id)

	select {
	case <-evts:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendMessage sends a plain text message to the given JID
// (e.g. "5511999999999@s.whatsapp.net").
func (s *Session) SendMessage(ctx context.Context, jid, text string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("not connected to WhatsApp")
	}
	to, err := types.ParseJID(jid)
	if err != nil {
		return fmt.Errorf("invalid JID %q: %w", jid, err)
	}
	_, err = s.client.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(text)})
	return err
}

// SendDocument uploads data as a document and sends it to the given JID
// with the provided filename and caption.
func (s *Session) SendDocument(ctx context.Context, jid string, filename string, data []byte, caption string) error {
	if !s.client.IsConnected() {
		return fmt.Errorf("not connected to WhatsApp")
	}
	to, err := types.ParseJID(jid)
	if err != nil {
		return fmt.Errorf("invalid JID %q: %w", jid, err)
	}

	resp, err := s.client.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("document upload: %w", err)
	}
	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL:           proto.String(resp.URL),
		DirectPath:    proto.String(resp.DirectPath),
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    proto.Uint64(resp.FileLength),
		Mimetype:      proto.String(mimeType(filename)),
		Title:         proto.String(filename),
		FileName:      proto.String(filename),
		Caption:       proto.String(caption),
	}}
	_, err = s.client.SendMessage(ctx, to, msg)
	return err
}

// Close disconnects the client without removing the paired device from
// the store; the session can be reconnected later.
func (s *Session) Close() {
	s.client.Disconnect()
}

func mimeType(filename string) string {
	if ext := filepath.Ext(filename); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	return "application/octet-stream"
}
