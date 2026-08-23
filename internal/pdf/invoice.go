package pdf

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/ajesus37/heavens-invoicing/internal/i18n"
	"github.com/ajesus37/heavens-invoicing/internal/model"
)

// SenderInfo holds the business emitting the invoice. PIXKey is the
// fallback payment key shown when the invoice has none of its own.
type SenderInfo struct {
	Name    string
	Address string
	PIXKey  string // fallback shown if invoice has no pix_key
}

const (
	margin      = 15.0
	pageWidth   = 210.0
	contentSize = pageWidth - 2*margin // 180mm usable width

	lineHeight = 6.0
)

var (
	colorText = [3]int{40, 40, 40}
	colorGray = [3]int{110, 110, 110}
	colorFill = [3]int{235, 235, 235}
	colorLine = [3]int{200, 200, 200}
)

// tr converts UTF-8 text to cp1252, the encoding expected by fpdf's
// built-in core fonts, so accented characters render correctly instead
// of showing up garbled in the PDF. The underlying translator reuses a
// shared buffer and is not safe for concurrent use, so calls are
// serialized; the lock is uncontended in practice (microsecond work).
var (
	uniMu sync.Mutex
	uni   = fpdf.New("P", "mm", "A4", "").UnicodeTranslatorFromDescriptor("")
)

func tr(s string) string {
	uniMu.Lock()
	defer uniMu.Unlock()
	return uni(s)
}

// PixKeyFor returns the PIX key to display on the invoice: the invoice's
// own key when set, otherwise the sender's, otherwise nil.
func PixKeyFor(inv model.Invoice, sender SenderInfo) *string {
	if inv.PIXKey != nil && *inv.PIXKey != "" {
		return inv.PIXKey
	}
	if sender.PIXKey != "" {
		k := sender.PIXKey
		return &k
	}
	return nil
}

// invoiceLang resolves the invoice language from the client's stored
// preference, falling back to pt-BR when it is empty or unknown.
func invoiceLang(c model.Client) i18n.Lang {
	return i18n.Resolve(c.Language)
}

func formatInvoiceNumber(lang i18n.Lang, n int64) string {
	return i18n.T(lang, "pdf.invoice_number", n)
}

// formatDate renders dates as dd/mm/yyyy in every locale — a documented
// decision: both supported audiences read this format unambiguously and
// the invoice stays layout-identical across languages.
func formatDate(t time.Time) string {
	return t.Format("02/01/2006")
}

// truncate shortens s so it fits maxWidth in the current font, appending
// an ellipsis when characters are cut off.
func truncate(p *fpdf.Fpdf, s string, maxWidth float64) string {
	s = tr(s)
	if p.GetStringWidth(s) <= maxWidth {
		return s
	}
	const ellipsis = "..."
	for len(s) > 0 && p.GetStringWidth(s+ellipsis) > maxWidth {
		r := []rune(s)
		s = string(r[:len(r)-1])
	}
	return strings.TrimRight(s, " ") + ellipsis
}

func setRegular(p *fpdf.Fpdf) {
	p.SetFont("Helvetica", "", 10)
	p.SetTextColor(colorText[0], colorText[1], colorText[2])
}
func setMuted(p *fpdf.Fpdf) {
	p.SetFont("Helvetica", "", 10)
	p.SetTextColor(colorGray[0], colorGray[1], colorGray[2])
}
func setBold(p *fpdf.Fpdf) {
	p.SetFont("Helvetica", "B", 10)
	p.SetTextColor(colorText[0], colorText[1], colorText[2])
}

// RenderInvoice writes a complete A4 invoice PDF to w, localized to the
// client's language (labels only; number and date formats are shared).
func RenderInvoice(w io.Writer, sender SenderInfo, client model.Client, inv model.Invoice) error {
	return renderInvoice(w, sender, client, inv, true)
}

// renderInvoice is RenderInvoice with a stream-compression switch so tests
// can emit uncompressed PDFs and assert on the localized text markers.
func renderInvoice(w io.Writer, sender SenderInfo, client model.Client, inv model.Invoice, compress bool) error {
	p := fpdf.New("P", "mm", "A4", "")
	if !compress {
		p.SetCompression(false)
	}
	p.SetMargins(margin, margin, margin)
	p.SetAutoPageBreak(true, margin)
	p.AddPage()

	lang := invoiceLang(client)
	drawHeader(p, lang, sender, inv)
	drawBillTo(p, lang, client)
	drawItems(p, lang, inv)
	drawTotals(p, lang, inv)
	drawPIX(p, lang, inv, sender)
	drawNotes(p, lang, inv)

	if p.Err() {
		return fmt.Errorf("rendering invoice: %s", p.Error())
	}
	if err := p.Output(w); err != nil {
		return fmt.Errorf("writing invoice pdf: %w", err)
	}
	return nil
}

func drawHeader(p *fpdf.Fpdf, lang i18n.Lang, sender SenderInfo, inv model.Invoice) {
	startY := p.GetY()

	setBold(p)
	p.SetFont("Helvetica", "B", 14)
	p.CellFormat(100, 8, truncate(p, sender.Name, 98), "", 2, "L", false, 0, "")
	setMuted(p)
	p.MultiCell(100, lineHeight, tr(sender.Address), "", "L", false)
	leftEnd := p.GetY()

	rightX, rightW := 105.0, contentSize-90
	p.SetY(startY)
	for _, line := range []struct {
		text string
		bold bool
	}{
		{formatInvoiceNumber(lang, inv.Number), true},
		{i18n.T(lang, "pdf.issued", formatDate(inv.IssueDate)), false},
		{i18n.T(lang, "pdf.due", formatDate(inv.DueDate)), false},
	} {
		p.SetX(rightX)
		if line.bold {
			setBold(p)
			p.SetFont("Helvetica", "B", 12)
		} else {
			setMuted(p)
		}
		p.CellFormat(rightW, lineHeight, tr(line.text), "", 2, "R", false, 0, "")
	}
	rightEnd := p.GetY()

	end := max(leftEnd, rightEnd) + 4
	p.SetY(end)
	p.SetDrawColor(colorLine[0], colorLine[1], colorLine[2])
	p.SetLineWidth(0.3)
	p.Line(margin, end, margin+contentSize, end)
	p.Ln(6)
}

func drawBillTo(p *fpdf.Fpdf, lang i18n.Lang, client model.Client) {
	setBold(p)
	p.CellFormat(contentSize, lineHeight, tr(i18n.T(lang, "pdf.bill_to")), "", 2, "L", false, 0, "")
	setRegular(p)
	p.CellFormat(contentSize, lineHeight, tr(client.Name), "", 2, "L", false, 0, "")
	if client.Address != "" {
		p.MultiCell(contentSize, lineHeight, tr(client.Address), "", "L", false)
	}
	p.Ln(4)
}

type column struct {
	width float64
	align string
}

var itemCols = []column{
	{width: 20, align: "L"},   // Qty
	{width: 85, align: "L"},   // Description
	{width: 37.5, align: "R"}, // Unit price
	{width: 37.5, align: "R"}, // Total
}

func drawItems(p *fpdf.Fpdf, lang i18n.Lang, inv model.Invoice) {
	headers := []string{
		i18n.T(lang, "pdf.qty"),
		i18n.T(lang, "pdf.description"),
		i18n.T(lang, "pdf.unit_price"),
		i18n.T(lang, "pdf.total"),
	}

	p.SetFillColor(colorFill[0], colorFill[1], colorFill[2])
	setBold(p)
	for i, h := range headers {
		p.CellFormat(itemCols[i].width, lineHeight+1, tr(h), "", 0, itemCols[i].align, true, 0, "")
	}
	p.Ln(-1)

	setRegular(p)
	for _, it := range inv.Items {
		// Description wraps instead of truncating; compute row height from
		// wrapped lines so the full text is readable. Other columns share
		// the same height and get a single bottom border.
		// Use the original UTF-8 description for SplitText (it expects UTF-8
		// runes); tr() is only for rendering.
		descW := itemCols[1].width - 2
		rawDesc := it.Description
		if strings.TrimSpace(rawDesc) == "" {
			rawDesc = " "
		}
		lines := p.SplitText(rawDesc, descW)
		if len(lines) == 0 {
			lines = []string{rawDesc}
		}
		rowH := float64(len(lines))*lineHeight + 2
		if rowH < lineHeight+2 {
			rowH = lineHeight + 2
		}
		_, pageH := p.GetPageSize()
		if p.GetY()+rowH > pageH-margin {
			p.AddPage()
			setRegular(p)
		}
		startY := p.GetY()
		startX := margin
		xQty := startX
		xDesc := startX + itemCols[0].width
		xUnit := xDesc + itemCols[1].width
		xTotal := xUnit + itemCols[2].width

		// Qty (top-aligned, full row height)
		p.SetXY(xQty, startY)
		p.CellFormat(itemCols[0].width, rowH, tr(fmt.Sprintf("%d", it.Quantity)), "", 0, itemCols[0].align, false, 0, "")
		// Description (wrapped)
		p.SetXY(xDesc, startY)
		p.MultiCell(itemCols[1].width, lineHeight, tr(it.Description), "", itemCols[1].align, false)
		// Unit and total share the same row height, drawn at startY
		p.SetXY(xUnit, startY)
		p.CellFormat(itemCols[2].width, rowH, tr(FormatBRL(it.UnitPrice)), "", 0, itemCols[2].align, false, 0, "")
		p.SetXY(xTotal, startY)
		p.CellFormat(itemCols[3].width, rowH, tr(FormatBRL(it.Total)), "", 0, itemCols[3].align, false, 0, "")
		// Single bottom border for the row
		p.SetDrawColor(colorLine[0], colorLine[1], colorLine[2])
		p.SetLineWidth(0.3)
		p.Line(margin, startY+rowH, margin+contentSize, startY+rowH)
		p.SetY(startY + rowH)
	}
	p.Ln(4)
}

func drawTotals(p *fpdf.Fpdf, lang i18n.Lang, inv model.Invoice) {
	labelW, valueW := contentSize-itemCols[3].width-60, 60.0
	valueX := margin + contentSize - valueW

	rows := []struct {
		label string
		value int64
		bold  bool
	}{
		{i18n.T(lang, "pdf.subtotal"), inv.Subtotal, false},
		{i18n.T(lang, "pdf.total"), inv.Total, true},
	}
	for _, r := range rows {
		if r.bold {
			setBold(p)
		} else {
			setRegular(p)
		}
		p.SetX(valueX - labelW)
		p.CellFormat(labelW, lineHeight, r.label, "", 0, "R", false, 0, "")
		p.CellFormat(valueW, lineHeight, FormatBRL(r.value), "", 2, "R", false, 0, "")
	}
	p.Ln(4)
}

func drawPIX(p *fpdf.Fpdf, lang i18n.Lang, inv model.Invoice, sender SenderInfo) {
	key := PixKeyFor(inv, sender)
	if key == nil {
		return
	}
	setBold(p)
	p.CellFormat(contentSize, lineHeight, tr(i18n.T(lang, "pdf.pix_key", *key)), "", 2, "L", false, 0, "")
	p.Ln(2)
}

func drawNotes(p *fpdf.Fpdf, lang i18n.Lang, inv model.Invoice) {
	notes := strings.TrimSpace(inv.Notes)
	if notes == "" {
		return
	}
	_, pageH := p.GetPageSize()
	if y := pageH - margin - lineHeight*3; p.GetY() < y {
		p.SetY(y) // pin notes to the bottom of the page
	}
	setMuted(p)
	p.MultiCell(contentSize, lineHeight, tr(i18n.T(lang, "pdf.notes", notes)), "", "L", false)
}
