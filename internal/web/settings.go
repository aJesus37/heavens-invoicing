package web

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/skip2/go-qrcode"
)

// settingsField describes one row of the settings form. Labels are
// resolved at render time from field.<key> catalog entries.
type settingsField struct {
	Key      string
	Value    string
	Kind     string // "text" | "password" | "textarea"
	KeepHint bool   // secret: blank input keeps the stored value
}

type configuracoesData struct {
	Locale    i18n.Lang
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
		{Key: repo.SettingBusinessName, Kind: "text"},
		{Key: repo.SettingBusinessAddress, Kind: "textarea"},
		{Key: repo.SettingDefaultPIXKey, Kind: "text"},
		{Key: repo.SettingSMTPHost, Kind: "text"},
		{Key: repo.SettingSMTPPort, Kind: "text"},
		{Key: repo.SettingSMTPUser, Kind: "text"},
		{Key: repo.SettingSMTPPass, Kind: "password", KeepHint: true},
		{Key: repo.SettingSMTPFrom, Kind: "text"},
		{Key: repo.SettingTelegramBotToken, Kind: "password", KeepHint: true},
		{Key: repo.SettingAdminTelegramChatID, Kind: "text"},
	}
}

// loadSettings assembles the settings page data. lang is resolved once by
// the caller and reused for the whole request.
func (h *Handlers) loadSettings(lang i18n.Lang, r *http.Request) *configuracoesData {
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
		Locale:    lang,
		Fields:    fields,
		Saved:     r.URL.Query().Get("saved") == "1",
		WAEnabled: h.wa != nil,
	}
}

func (h *Handlers) settingsForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	h.renderPage(w, r, http.StatusOK, "configuracoes.html", i18n.T(lang, "settings.title"), lang, h.loadSettings(lang, r))
}

func (h *Handlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}
	for _, f := range settingsFields() {
		value := r.FormValue(f.Key)
		if secretKeys[f.Key] && value == "" {
			continue // keep stored secret when left blank
		}
		if err := h.repos.Settings.Set(ctx, f.Key, value); err != nil {
			writeRepoErr(w, lang, err)
			return
		}
	}
	// The language selector is not part of settingsFields; only persist
	// values we actually understand so junk can't break every page render.
	if v := r.FormValue(repo.SettingLocale); v != "" {
		if parsed, ok := i18n.Parse(v); ok {
			if err := h.repos.Settings.Set(ctx, repo.SettingLocale, string(parsed)); err != nil {
				writeRepoErr(w, lang, err)
				return
			}
		} else {
			log.Printf("web: ignoring unsupported locale value %q", v)
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
	lang := h.lang(r)
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
	h.render.renderFragment(w, "wa_status.html", lang, data)
}

// whatsappConnect starts a pairing attempt and returns the refreshed
// fragment so the QR appears without a full page reload.
func (h *Handlers) whatsappConnect(w http.ResponseWriter, r *http.Request) {
	h.pairing.start(h.lang(r))
	h.whatsappStatusFragment(w, r)
}

// whatsappQRPNG encodes the current pairing code as a PNG image.
func (h *Handlers) whatsappQRPNG(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	_, qr, _ := h.pairing.snapshot()
	if qr == "" {
		http.Error(w, i18n.T(lang, "error.no_qr"), http.StatusNotFound)
		return
	}
	png, err := qrcode.Encode(qr, qrcode.Medium, 256)
	if err != nil {
		log.Printf("web: encode qr: %v", err)
		failInternal(w, lang)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
