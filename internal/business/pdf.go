package business

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func moneyText(minor int64) string { return fmt.Sprintf("%d,%02d", minor/100, minor%100) }

func renderInvoice(seller Seller, buyer Counterparty, invoice Invoice) ([]byte, error) {
	return renderBillingPDF(seller, buyer, invoice, false)
}

func renderBillingPDF(seller Seller, buyer Counterparty, invoice Invoice, act bool) ([]byte, error) {
	title, sellerLabel, buyerLabel, totalLabel := "Счёт на оплату № "+invoice.Number, "Поставщик", "Покупатель", "Итого к оплате: "
	dateLabel := "от " + invoice.IssueDate
	if invoice.DueDate != "" {
		dateLabel += "  ·  Оплатить до " + invoice.DueDate
	}
	if act {
		title, sellerLabel, buyerLabel, totalLabel = "Акт оказанных услуг", "Исполнитель", "Заказчик", "Стоимость услуг: "
		dateLabel = "К счёту № " + invoice.Number + " от " + invoice.IssueDate
	}
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 18)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Go", "B", gobold.TTF)
	if act {
		pdf.SetTitle(title, true)
	} else {
		pdf.SetTitle("Счёт № "+invoice.Number+" от "+invoice.IssueDate, true)
	}
	pdf.SetAuthor(seller.Name, true)
	pdf.SetCreationDate(parseTimestamp(invoice.CreatedAt))
	pdf.SetCatalogSort(true)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("Go", "", 8)
		pdf.CellFormat(180, 5, fmt.Sprintf("Страница %d/{nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
	})
	pdf.AliasNbPages("")
	pdf.AddPage()
	pdf.SetTextColor(25, 25, 25)
	pdf.SetFont("Go", "B", 17)
	pdf.MultiCell(180, 8, title, "", "L", false)
	pdf.SetFont("Go", "", 10)
	pdf.MultiCell(180, 6, dateLabel, "", "L", false)
	pdf.Ln(5)
	paragraph := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		pdf.SetFont("Go", "B", 9)
		pdf.MultiCell(180, 5, label, "", "L", false)
		pdf.SetFont("Go", "", 10)
		pdf.MultiCell(180, 5, value, "", "L", false)
		pdf.Ln(3)
	}
	paragraph(sellerLabel, seller.Name+"\nИНН "+seller.INN+nonemptyLine(seller.Address))
	if !act {
		paragraph("Банковские реквизиты", seller.BankName+nonemptyLine("БИК "+seller.BankBIK)+nonemptyLine("Расчётный счёт "+seller.BankAccount)+nonemptyLine("Корреспондентский счёт "+seller.BankCorrespondentAccount))
	}
	buyerText := buyer.Name
	if buyer.INN != "" {
		buyerText += "\nИНН " + buyer.INN
	}
	buyerText += nonemptyLine(buyer.Address)
	paragraph(buyerLabel, buyerText)
	paragraph("Основание", invoice.Basis)
	paragraph("Период оказания услуг", invoice.PeriodStart+" - "+invoice.PeriodEnd)
	widths := []float64{8, 82, 18, 14, 28, 30}
	header := func() {
		pdf.SetFont("Go", "B", 9)
		pdf.SetFillColor(242, 242, 242)
		for i, value := range []string{"№", "Товары / услуги", "Кол-во", "Ед.", "Цена, руб.", "Сумма, руб."} {
			pdf.CellFormat(widths[i], 9, value, "1", 0, "C", true, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetFont("Go", "", 9)
	}
	if pdf.GetY() > 240 {
		pdf.AddPage()
	}
	header()
	for index, item := range invoice.Items {
		values := []string{fmt.Sprint(index + 1), item.Description, item.Quantity, item.Unit, moneyText(item.PriceMinor), moneyText(item.AmountMinor)}
		height := 8.0
		for i, value := range values {
			lines := pdf.SplitText(value, widths[i]-3)
			height = max(height, float64(len(lines))*4.5+3)
		}
		if pdf.GetY()+height > 275 {
			pdf.AddPage()
			header()
		}
		x, y := 15.0, pdf.GetY()
		for i, value := range values {
			pdf.Rect(x, y, widths[i], height, "D")
			pdf.SetXY(x+1.5, y+1.5)
			align := "L"
			if i == 0 || i == 2 || i == 3 {
				align = "C"
			}
			if i >= 4 {
				align = "R"
			}
			pdf.MultiCell(widths[i]-3, 4.5, value, "", align, false)
			x += widths[i]
		}
		pdf.SetXY(15, y+height)
	}
	pdf.Ln(5)
	pdf.SetFont("Go", "B", 12)
	pdf.MultiCell(180, 7, totalLabel+moneyText(invoice.AmountMinor)+" руб.", "", "R", false)
	pdf.SetFont("Go", "", 10)
	pdf.MultiCell(180, 6, invoice.VAT, "", "R", false)
	if act {
		if pdf.GetY() > 240 {
			pdf.AddPage()
		}
		pdf.Ln(8)
		pdf.MultiCell(180, 8, "Исполнитель __________________   Заказчик __________________\nДата подписания __________________", "", "L", false)
	}
	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func nonemptyLine(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "\n" + value
}
