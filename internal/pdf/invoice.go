package pdf

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/jesus/invoice-app/internal/model"
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

// uni converts UTF-8 text to cp1252, the encoding expected by fpdf's
// built-in core fonts, so accented characters render correctly instead
// of showing up garbled in the PDF.
var uni = func() func(string) string {
	tr := fpdf.New("P", "mm", "A4", "").UnicodeTranslatorFromDescriptor("")
	tr("warmup")
	return tr
}()

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

func formatInvoiceNumber(n int64) string {
	return fmt.Sprintf("Fatura #%06d", n)
}

func formatDate(t time.Time) string {
	return t.Format("02/01/2006")
}

// truncate shortens s so it fits maxWidth in the current font, appending
// an ellipsis when characters are cut off.
func truncate(p *fpdf.Fpdf, s string, maxWidth float64) string {
	s = uni(s)
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

// RenderInvoice writes a complete A4 invoice PDF to w.
func RenderInvoice(w io.Writer, sender SenderInfo, client model.Client, inv model.Invoice) error {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(margin, margin, margin)
	p.SetAutoPageBreak(true, margin)
	p.AddPage()

	drawHeader(p, sender, inv)
	drawBillTo(p, client)
	drawItems(p, inv)
	drawTotals(p, inv)
	drawPIX(p, inv, sender)
	drawNotes(p, inv)

	if p.Err() {
		return fmt.Errorf("rendering invoice: %s", p.Error())
	}
	if err := p.Output(w); err != nil {
		return fmt.Errorf("writing invoice pdf: %w", err)
	}
	return nil
}

func drawHeader(p *fpdf.Fpdf, sender SenderInfo, inv model.Invoice) {
	startY := p.GetY()

	setBold(p)
	p.SetFont("Helvetica", "B", 14)
	p.CellFormat(100, 8, truncate(p, sender.Name, 98), "", 2, "L", false, 0, "")
	setMuted(p)
	p.MultiCell(100, lineHeight, uni(sender.Address), "", "L", false)
	leftEnd := p.GetY()

	rightX, rightW := 105.0, contentSize-90
	p.SetY(startY)
	for _, line := range []struct {
		text string
		bold bool
	}{
		{formatInvoiceNumber(inv.Number), true},
		{"Emissão: " + formatDate(inv.IssueDate), false},
		{"Vencimento: " + formatDate(inv.DueDate), false},
	} {
		p.SetX(rightX)
		if line.bold {
			setBold(p)
			p.SetFont("Helvetica", "B", 12)
		} else {
			setMuted(p)
		}
		p.CellFormat(rightW, lineHeight, uni(line.text), "", 2, "R", false, 0, "")
	}
	rightEnd := p.GetY()

	end := max(leftEnd, rightEnd) + 4
	p.SetY(end)
	p.SetDrawColor(colorLine[0], colorLine[1], colorLine[2])
	p.SetLineWidth(0.3)
	p.Line(margin, end, margin+contentSize, end)
	p.Ln(6)
}

func drawBillTo(p *fpdf.Fpdf, client model.Client) {
	setBold(p)
	p.CellFormat(contentSize, lineHeight, uni("Cobrança para:"), "", 2, "L", false, 0, "")
	setRegular(p)
	p.CellFormat(contentSize, lineHeight, uni(client.Name), "", 2, "L", false, 0, "")
	if client.Address != "" {
		p.MultiCell(contentSize, lineHeight, uni(client.Address), "", "L", false)
	}
	p.Ln(4)
}

type column struct {
	width float64
	align string
}

var itemCols = []column{
	{width: 20, align: "L"},   // Qty
	{width: 85, align: "L"},   // Descrição
	{width: 37.5, align: "R"}, // Preço Unit.
	{width: 37.5, align: "R"}, // Total
}

func drawItems(p *fpdf.Fpdf, inv model.Invoice) {
	headers := []string{"Qty", "Descrição", "Preço Unit.", "Total"}

	p.SetFillColor(colorFill[0], colorFill[1], colorFill[2])
	setBold(p)
	for i, h := range headers {
		p.CellFormat(itemCols[i].width, lineHeight+1, uni(h), "", 0, itemCols[i].align, true, 0, "")
	}
	p.Ln(-1)

	setRegular(p)
	for _, it := range inv.Items {
		desc := truncate(p, it.Description, itemCols[1].width-2)
		cells := []string{
			fmt.Sprintf("%d", it.Quantity),
			desc,
			FormatBRL(it.UnitPrice),
			FormatBRL(it.Total),
		}
		rowH := lineHeight + 2
		for i, c := range cells {
			p.CellFormat(itemCols[i].width, rowH, c, "B", 0, itemCols[i].align, false, 0, "")
		}
		p.Ln(-1)
	}
	p.Ln(4)
}

func drawTotals(p *fpdf.Fpdf, inv model.Invoice) {
	labelW, valueW := contentSize-itemCols[3].width-60, 60.0
	valueX := margin + contentSize - valueW

	rows := []struct {
		label string
		value int64
		bold  bool
	}{
		{"Subtotal", inv.Subtotal, false},
		{"Total", inv.Total, true},
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

func drawPIX(p *fpdf.Fpdf, inv model.Invoice, sender SenderInfo) {
	key := PixKeyFor(inv, sender)
	if key == nil {
		return
	}
	setBold(p)
	p.CellFormat(contentSize, lineHeight, uni("Chave PIX: "+*key), "", 2, "L", false, 0, "")
	p.Ln(2)
}

func drawNotes(p *fpdf.Fpdf, inv model.Invoice) {
	notes := strings.TrimSpace(inv.Notes)
	if notes == "" {
		return
	}
	_, pageH := p.GetPageSize()
	if y := pageH - margin - lineHeight*3; p.GetY() < y {
		p.SetY(y) // pin notes to the bottom of the page
	}
	setMuted(p)
	p.MultiCell(contentSize, lineHeight, uni("Observações: "+notes), "", "L", false)
}
