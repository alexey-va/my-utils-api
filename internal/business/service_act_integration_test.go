package business

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestBusinessServiceActPersistsOnceAndPreservesImports(t *testing.T) {
	for _, test := range []struct {
		name, mime, filename, contents string
	}{
		{name: "no original act"},
		{"imported DOCX", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "original-act.docx", "PK\x03\x04synthetic"},
		{"imported PDF", "application/pdf", "original-act.pdf", "%PDF-1.4\noriginal act\n%%EOF"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, owner := testService(t, nil)
			ctx := context.Background()
			s.now = func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
			originals := func() ImportRequest {
				request := importFixture()
				if test.mime != "" {
					request.Documents = append(request.Documents, ImportDocument{
						ID: "original-act", Filename: test.filename, MIMEType: test.mime,
						DataBase64: base64.StdEncoding.EncodeToString([]byte(test.contents)),
					})
					request.Invoices[0].Documents = append(request.Invoices[0].Documents, Document{ID: "original-act", Kind: "act"})
				}
				return request
			}
			before, err := s.Import(ctx, owner, originals())
			if err != nil {
				t.Fatal(err)
			}
			view, err := s.Workspace(ctx, owner, "all", "")
			if err != nil || view.PreviousPeriod != "2026-09" || len(view.Counterparties) != 1 || view.Counterparties[0].PreviousPeriodDocuments != (PeriodDocuments{Invoice: true, Act: test.mime != ""}) {
				t.Fatalf("initial document coverage = %+v, %v", view.Counterparties, err)
			}
			var acts [2]File
			var actErrors [2]error
			var concurrent sync.WaitGroup
			start := make(chan struct{})
			for i := range acts {
				concurrent.Go(func() {
					<-start
					acts[i], actErrors[i] = s.ServiceActPDF(ctx, owner, "payer-one", "rent", "2026-09")
				})
			}
			close(start)
			concurrent.Wait()
			act := acts[0]
			generated := test.mime != "application/pdf"
			if actErrors[0] != nil || actErrors[1] != nil || act.ID == "" || act.Kind != "act" || act.GeneratedServiceAct != generated || act.Document != acts[1].Document || !bytes.Equal(act.Data, acts[1].Data) {
				t.Fatalf("concurrent acts = %+v and %+v, errors %v", act.Document, acts[1].Document, actErrors)
			}
			downloaded, err := s.File(ctx, owner, act.ID)
			if err != nil || downloaded.MIMEType != "application/pdf" || downloaded.SHA256 != act.SHA256 || !bytes.Equal(downloaded.Data, act.Data) {
				t.Fatalf("stored act = %+v, %v", downloaded.Document, err)
			}
			if test.mime != "" {
				original, err := s.File(ctx, owner, "original-act")
				if err != nil || original.MIMEType != test.mime || string(original.Data) != test.contents {
					t.Fatalf("original attachment = %+v, %v", original.Document, err)
				}
				if !generated && act.ID != original.ID {
					t.Fatalf("original PDF should be returned, got %q", act.ID)
				}
			}
			wantFiles := before.DocumentCount
			if generated {
				wantFiles++
			}
			reimport := originals()
			reimport.Seller.Name += " (новые реквизиты)"
			result, err := s.Import(ctx, owner, reimport)
			if err != nil || result.DocumentCount != wantFiles || result.InvoiceCount != before.InvoiceCount {
				t.Fatalf("reimport originals = %+v, %v", result, err)
			}
			again, err := s.ServiceActPDF(ctx, owner, "payer-one", "rent", "2026-09")
			if err != nil || again.Document != act.Document || !bytes.Equal(again.Data, act.Data) {
				t.Fatalf("retry after seller change = %+v, %v", again.Document, err)
			}
			if generated {
				reimport = originals()
				reimport.Invoices[0].Documents = append(reimport.Invoices[0].Documents, Document{ID: act.ID, Kind: "act"})
				if result, err := s.Import(ctx, owner, reimport); err != nil || result.DocumentCount != wantFiles {
					t.Fatalf("reimport with act = %+v, %v", result, err)
				}
				reimport.Invoices[0].Documents[len(reimport.Invoices[0].Documents)-1].Kind = "other"
				_, err = s.Import(ctx, owner, reimport)
				requireStatus(t, err, http.StatusConflict)
			}
			view, err = s.Workspace(ctx, owner, "all", "")
			if err != nil || len(view.Invoices) != 2 || len(view.Counterparties) != 1 || view.Summary.InvoiceCount != 1 || view.Summary.InvoicedMinor != 10000 || view.Counterparties[0].PreviousPeriodDocuments != (PeriodDocuments{Invoice: true, Act: true}) {
				t.Fatalf("act coverage or receivable changed incorrectly = %+v, %v", view.Summary, err)
			}
			wantAttachments := len(originals().Invoices[0].Documents)
			if generated {
				wantAttachments++
			}
			current, archived := view.Invoices[0], view.Invoices[1]
			if !current.IsCurrent || len(current.Documents) != wantAttachments || current.Documents[wantAttachments-1] != act.Document || archived.IsCurrent || len(archived.Documents) != 1 {
				t.Fatalf("current/archived documents = %+v and %+v", current.Documents, archived.Documents)
			}
		})
	}
}
