package business

import (
	"net/http"

	"github.com/alexey-va/my-utils-api/internal/workout"
)

const MaxImportBytes = 96 << 20
const maxDocumentBytes = 20 << 20
const maxMinor int64 = 9_000_000_000_000_000 // Below JavaScript's exact integer ceiling.

type Contact struct {
	Email     string `json:"email"`
	Note      string `json:"note"`
	Confirmed bool   `json:"confirmed"`
}
type LineItem struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	Unit        string `json:"unit"`
	PriceMinor  int64  `json:"priceMinor"`
	AmountMinor int64  `json:"amountMinor"`
}
type InvoiceTemplate struct {
	ID    string     `json:"id"`
	Label string     `json:"label"`
	Basis string     `json:"basis"`
	Items []LineItem `json:"items"`
}
type Seller struct {
	Name                     string `json:"name"`
	INN                      string `json:"inn"`
	Address                  string `json:"address"`
	BankName                 string `json:"bankName"`
	BankBIK                  string `json:"bankBik"`
	BankAccount              string `json:"bankAccount"`
	BankCorrespondentAccount string `json:"bankCorrespondentAccount"`
}
type Counterparty struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	EntityType string            `json:"entityType"`
	INN        string            `json:"inn"`
	Address    string            `json:"address"`
	Inactive   bool              `json:"inactive"`
	Contacts   []Contact         `json:"contacts"`
	Templates  []InvoiceTemplate `json:"templates"`
}
type Document struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	Filename            string `json:"filename"`
	MIMEType            string `json:"mimeType"`
	Size                int    `json:"size"`
	SHA256              string `json:"sha256"`
	GeneratedServiceAct bool   `json:"generatedServiceAct,omitempty"`
}
type Invoice struct {
	ID             string     `json:"id"`
	CounterpartyID string     `json:"counterpartyId"`
	TemplateID     string     `json:"templateId"`
	Number         string     `json:"number"`
	IssueDate      string     `json:"issueDate"`
	DueDate        string     `json:"dueDate"`
	Period         string     `json:"period"`
	PeriodStart    string     `json:"periodStart"`
	PeriodEnd      string     `json:"periodEnd"`
	Basis          string     `json:"basis"`
	Items          []LineItem `json:"items"`
	AmountMinor    int64      `json:"amountMinor"`
	Currency       string     `json:"currency"`
	VAT            string     `json:"vat"`
	IsCurrent      bool       `json:"isCurrent"`
	Revision       int        `json:"revision"`
	Source         string     `json:"source"`
	Provider       string     `json:"provider"`
	ProviderState  string     `json:"providerState"`
	BankDocumentID string     `json:"bankDocumentId"`
	Documents      []Document `json:"documents"`
	CreatedAt      string     `json:"createdAt"`
}
type Allocation struct {
	ID          string `json:"id"`
	InvoiceID   string `json:"invoiceId"`
	PaymentID   string `json:"paymentId"`
	AmountMinor int64  `json:"amountMinor"`
	Automatic   bool   `json:"automatic"`
}
type Payment struct {
	ID               string `json:"id"`
	Date             string `json:"date"`
	AmountMinor      int64  `json:"amountMinor"`
	Currency         string `json:"currency"`
	CounterpartyID   string `json:"counterpartyId"`
	CounterpartyName string `json:"counterpartyName"`
	PayerINN         string `json:"payerInn"`
	Description      string `json:"description"`
	AccountLabel     string `json:"accountLabel"`
	Source           string `json:"source"`
}
type Bank struct {
	Connected     bool   `json:"connected"`
	LastSyncedAt  string `json:"lastSyncedAt"`
	CoverageStart string `json:"coverageStart"`
	CoverageEnd   string `json:"coverageEnd"`
	AccountCount  int    `json:"accountCount"`
	Error         string `json:"error"`
}
type InvoiceView struct {
	Invoice
	PaidMinor        int64        `json:"paidMinor"`
	OutstandingMinor int64        `json:"outstandingMinor"`
	Status           string       `json:"status"`
	Allocations      []Allocation `json:"allocations"`
}
type PaymentView struct {
	Payment
	HistoricalInvoiceID string       `json:"historicalInvoiceId,omitempty"`
	Classification      string       `json:"classification"`
	AdvanceMinor        int64        `json:"advanceMinor"`
	AllocatedMinor      int64        `json:"allocatedMinor"`
	UnallocatedMinor    int64        `json:"unallocatedMinor"`
	Allocations         []Allocation `json:"allocations"`
}
type PeriodDocuments struct {
	Invoice bool `json:"invoice"`
	Act     bool `json:"act"`
}
type CounterpartyView struct {
	Counterparty
	PreviousPeriodDocuments PeriodDocuments `json:"previousPeriodDocuments"`
	InvoicedMinor           int64           `json:"invoicedMinor"`
	PaidMinor               int64           `json:"paidMinor"`
	OutstandingMinor        int64           `json:"outstandingMinor"`
	ReceivedMinor           int64           `json:"receivedMinor"`
	AdvanceMinor            int64           `json:"advanceMinor"`
	InvoiceCount            int             `json:"invoiceCount"`
	UnreviewedCount         int             `json:"unreviewedCount"`
	LastPaymentAt           string          `json:"lastPaymentAt"`
}
type Summary struct {
	InvoicedMinor           int64 `json:"invoicedMinor"`
	PaidMinor               int64 `json:"paidMinor"`
	OutstandingMinor        int64 `json:"outstandingMinor"`
	ReceivedMinor           int64 `json:"receivedMinor"`
	HistoricalReceivedMinor int64 `json:"historicalReceivedMinor"`
	AdvanceMinor            int64 `json:"advanceMinor"`
	UnallocatedMinor        int64 `json:"unallocatedMinor"`
	InvoiceCount            int   `json:"invoiceCount"`
	CounterpartyCount       int   `json:"counterpartyCount"`
	PaidInvoiceCount        int   `json:"paidInvoiceCount"`
	PartialInvoiceCount     int   `json:"partialInvoiceCount"`
	UnreviewedInvoiceCount  int   `json:"unreviewedInvoiceCount"`
	ReconciliationComplete  bool  `json:"reconciliationComplete"`
}
type Workspace struct {
	Seller            Seller             `json:"seller"`
	Counterparties    []CounterpartyView `json:"counterparties"`
	Invoices          []InvoiceView      `json:"invoices"`
	Payments          []PaymentView      `json:"payments"`
	Summary           Summary            `json:"summary"`
	Bank              Bank               `json:"bank"`
	PreviousPeriod    string             `json:"previousPeriod"`
	Periods           []string           `json:"periods"`
	AccountingPeriods []string           `json:"accountingPeriods"`
	NextInvoiceNumber string             `json:"nextInvoiceNumber"`
}
type CreateInvoiceRequest struct {
	CounterpartyID string     `json:"counterpartyId"`
	TemplateID     string     `json:"templateId"`
	Period         string     `json:"period"`
	IssueDate      string     `json:"issueDate"`
	DueDate        string     `json:"dueDate"`
	Number         string     `json:"number"`
	Basis          *string    `json:"basis"`
	Items          []LineItem `json:"items"`
	Provider       string     `json:"provider"`
}
type AllocateRequest struct {
	InvoiceID   string `json:"invoiceId"`
	PaymentID   string `json:"paymentId"`
	AmountMinor int64  `json:"amountMinor"`
}
type ImportDocument struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	MIMEType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
}
type ImportRequest struct {
	HistoricalPaymentIDs []string         `json:"historicalPaymentIds,omitempty"`
	Seller               Seller           `json:"seller"`
	Counterparties       []Counterparty   `json:"counterparties"`
	Invoices             []Invoice        `json:"invoices"`
	Payments             []Payment        `json:"payments"`
	Bank                 *Bank            `json:"bank"`
	Documents            []ImportDocument `json:"documents"`
}
type ImportResult struct {
	CounterpartyCount int `json:"counterpartyCount"`
	InvoiceCount      int `json:"invoiceCount"`
	PaymentCount      int `json:"paymentCount"`
	DocumentCount     int `json:"documentCount"`
}
type File struct {
	Document
	Data []byte
}
type Coverage struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type state struct {
	BankAccountScope     string         `json:"bankAccountScope,omitempty"`
	HistoricalPaymentIDs []string       `json:"historicalPaymentIds,omitempty"`
	Seller               Seller         `json:"seller"`
	Counterparties       []Counterparty `json:"counterparties"`
	Invoices             []Invoice      `json:"invoices"`
	Payments             []Payment      `json:"payments"`
	Allocations          []Allocation   `json:"allocations"`
	Bank                 Bank           `json:"bank"`
	Coverage             []Coverage     `json:"coverage"`
}

func emptyState() state {
	return state{Counterparties: []Counterparty{}, Invoices: []Invoice{}, Payments: []Payment{}, Allocations: []Allocation{}, Coverage: []Coverage{}}
}
func invalid(message string) error {
	return &workout.Error{Status: http.StatusBadRequest, Message: message}
}
func conflict(message string) error {
	return &workout.Error{Status: http.StatusConflict, Message: message}
}
func missing(message string) error {
	return &workout.Error{Status: http.StatusNotFound, Message: message}
}
func unavailable(message string) error {
	return &workout.Error{Status: http.StatusServiceUnavailable, Message: message}
}
func bankFailure() error {
	return &workout.Error{Status: http.StatusBadGateway, Message: "Банк не подтвердил операцию. Проверьте состояние перед повтором."}
}
