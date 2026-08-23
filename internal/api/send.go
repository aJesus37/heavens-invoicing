package api

import (
	"bytes"
	"log"
	"net/http"

	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/pdf"
)

type sendRequest struct {
	Method string `json:"method"`
}

type channelResultResponse struct {
	Channel string `json:"channel"`
	Error   string `json:"error,omitempty"`
}

type sendResponse struct {
	Sent    bool                    `json:"sent"`
	Results []channelResultResponse `json:"results"`
	Error   string                  `json:"error,omitempty"`
}

// sendInvoice renders the invoice PDF and hands delivery to the Router,
// which fans out per the requested method. The response always carries the
// per-channel results.
func (a *api) sendInvoice(w http.ResponseWriter, r *http.Request) {
	var body sendRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validSendMethod(body.Method) {
		writeError(w, http.StatusBadRequest, "method must be one of: email, whatsapp, telegram, all")
		return
	}

	inv, ok := a.loadInvoice(w, r)
	if !ok {
		return
	}
	client, err := a.repos.Clients.Get(r.Context(), inv.ClientID)
	if err != nil {
		writeRepoErr(w, err)
		return
	}

	buf := &bytes.Buffer{}
	if err := pdf.RenderInvoice(buf, a.senderInfo, *client, *inv); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render pdf")
		return
	}

	results, err := a.router.SendInvoice(r.Context(), *client, *inv, buf.Bytes(), body.Method)
	if err != nil && len(results) == 0 {
		// Routing refused up front (e.g. the invoice is already paid);
		// that is a client-visible rejection, not an internal error.
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	resp := toSendResponse(results)
	if err != nil {
		// err is a persistence failure after delivery; details stay in the
		// server log, not the response.
		log.Printf("send invoice %s: %v", inv.ID, err)
		resp.Error = "internal error"
		writeJSON(w, http.StatusInternalServerError, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func validSendMethod(m string) bool {
	switch m {
	case deliver.MethodEmail, deliver.MethodWhatsApp, deliver.MethodTelegram, deliver.MethodAll:
		return true
	default:
		return false
	}
}

func toSendResponse(results []deliver.ChannelResult) sendResponse {
	resp := sendResponse{Results: make([]channelResultResponse, 0, len(results))}
	for _, res := range results {
		errText := ""
		if res.Err != nil {
			errText = res.Err.Error()
		} else {
			resp.Sent = true
		}
		resp.Results = append(resp.Results, channelResultResponse{Channel: res.Channel, Error: errText})
	}
	return resp
}
