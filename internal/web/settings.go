package web

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jesus/invoice-app/internal/repo"
	"github.com/skip2/go-qrcode"
)

// settingsField describes one row of the settings form.
type settingsField struct {
	Key      string
	Label    string
	Value    string
	Kind     string // "text" | "password" | "textarea"
	KeepHint bool   // secret: blank input keeps the stored value
}

type configuracoesData struct {
	Fields    []settingsField
	Saved     bool
	WAEnabled bool
	Error     string
}

// secretKeys are never re-rendered into the form; a blank submit keeps the
// stored value instead of clearing it.
var secretKeys = map[string]bool{
	repo.SettingSMTPPass:         true,
	repo.SettingTelegramBotToken: true,
}

func settingsFields() []settingsField {
	return []settingsField{
		{Key: repo.SettingBusinessName, Label: "Nome do negócio", Kind: "text"},
		{Key: repo.SettingBusinessAddress, Label: "Endereço do negócio", Kind: "textarea"},
		{Key: repo.SettingDefaultPIXKey, Label: "Chave PIX padrão", Kind: "text"},
		{Key: repo.SettingSMTPHost, Label: "Servidor SMTP", Kind: "text"},
		{Key: repo.SettingSMTPPort, Label: "Porta SMTP", Kind: "text"},
		{Key: repo.SettingSMTPUser, Label: "Usuário SMTP", Kind: "text"},
		{Key: repo.SettingSMTPPass, Label: "Senha SMTP", Kind: "password", KeepHint: true},
		{Key: repo.SettingSMTPFrom, Label: "Remetente (From)", Kind: "text"},
		{Key: repo.SettingTelegramBotToken, Label: "Token do bot Telegram", Kind: "password", KeepHint: true},
		{Key: repo.SettingAdminTelegramChatID, Label: "Chat ID admin no Telegram", Kind: "text"},
	}
}

func (h *Handlers) loadSettings(w http.ResponseWriter, r *http.Request) *configuracoesData {
	fields := settingsFields()
	for i := range fields {
		value, err := h.repos.Settings.Get(r.Context(), fields[i].Key)
		if err != nil {
			continue // unset keys render blank; read errors shouldn't hide the page
		}
		if secretKeys[fields[i].Key] {
			continue
		}
		fields[i].Value = value
	}
	return &configuracoesData{
		Fields:    fields,
		Saved:     r.URL.Query().Get("saved") == "1",
		WAEnabled: h.wa != nil,
	}
}

func (h *Handlers) settingsForm(w http.ResponseWriter, r *http.Request) {
	h.render.renderPage(w, http.StatusOK, "configuracoes.html", "Configurações", h.loadSettings(w, r))
}

func (h *Handlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}
	for _, f := range settingsFields() {
		value := r.FormValue(f.Key)
		if secretKeys[f.Key] && value == "" {
			continue // keep stored secret when left blank
		}
		if err := h.repos.Settings.Set(ctx, f.Key, value); err != nil {
			writeRepoErr(w, err)
			return
		}
	}
	http.Redirect(w, r, "/configuracoes?saved=1", http.StatusSeeOther)
}

// waStatusData feeds the auto-refreshing WhatsApp fragment.
type waStatusData struct {
	Enabled bool
	State   string
	QRPath  string
	ErrMsg  string
}

func (h *Handlers) whatsappStatusFragment(w http.ResponseWriter, r *http.Request) {
	data := waStatusData{Enabled: h.wa != nil}
	if h.wa != nil {
		state, qr, errMsg := h.pairing.snapshot()
		switch {
		case h.wa.IsPaired():
			data.State = pairConnected
		case state == pairPending && qr != "":
			data.State = pairPending
			// Cache-buster so the <img> refetches on every fragment poll.
			data.QRPath = fmt.Sprintf("/configuracoes/whatsapp/qr.png?v=%d", time.Now().Unix())
		case state == pairFailed:
			data.State = pairFailed
			data.ErrMsg = errMsg
		default:
			data.State = pairIdle
		}
	}
	h.render.renderFragment(w, "wa_status.html", data)
}

// whatsappConnect starts a pairing attempt and returns the refreshed
// fragment so the QR appears without a full page reload.
func (h *Handlers) whatsappConnect(w http.ResponseWriter, r *http.Request) {
	h.pairing.start()
	h.whatsappStatusFragment(w, r)
}

// whatsappQRPNG encodes the current pairing code as a PNG image.
func (h *Handlers) whatsappQRPNG(w http.ResponseWriter, r *http.Request) {
	_, qr, _ := h.pairing.snapshot()
	if qr == "" {
		http.Error(w, "nenhum QR ativo", http.StatusNotFound)
		return
	}
	png, err := qrcode.Encode(qr, qrcode.Medium, 256)
	if err != nil {
		log.Printf("web: encode qr: %v", err)
		http.Error(w, "erro interno", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
