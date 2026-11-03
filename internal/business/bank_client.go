package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Public Russian Trusted Root CA from the bank's documented TLS installation
// guide. It supplements system roots; hostname and certificate checks stay on.
//
//go:embed russian_trusted_root_ca.pem
var russianTrustedRoot []byte

const bankBase = "https://enter.tochka.com/uapi"

var bankAccountID = regexp.MustCompile(`^[0-9]{20}/[0-9]{9}$`)
var bankCustomerCode = regexp.MustCompile(`^[0-9]{1,9}$`)
var bankResourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type BankClient struct {
	http         *http.Client
	pollInterval time.Duration
}
type bankRequestError struct{ status int }

func (e *bankRequestError) Error() string { return "Банк отклонил запрос" }

type rejectedBankCreate struct{ cause error }

func (e *rejectedBankCreate) Error() string {
	return "Банк отклонил создание счёта; можно выбрать локальный PDF"
}
func rejectedCreate(err error) bool {
	var rejection *rejectedBankCreate
	if errors.As(err, &rejection) {
		return true
	}
	var response *bankRequestError
	if !errors.As(err, &response) {
		return false
	}
	switch response.status {
	case 400, 401, 403, 404, 405, 409, 413, 422:
		return true
	}
	return false
}

type BankAccount struct {
	ID           string `json:"accountId"`
	CustomerCode string `json:"customerCode"`
	Status       string `json:"status"`
	Currency     string `json:"currency"`
	Type         string `json:"accountType"`
}
type bankMeta struct {
	TotalPages int `json:"totalPages"`
}
type bankStatement struct {
	AccountID    string            `json:"accountId"`
	ID           string            `json:"statementId"`
	Status       string            `json:"status"`
	From         string            `json:"startDateTime"`
	To           string            `json:"endDateTime"`
	Transactions []bankTransaction `json:"Transaction"`
}
type bankTransaction struct {
	ID          string `json:"transactionId"`
	PaymentID   string `json:"paymentId"`
	Direction   string `json:"creditDebitIndicator"`
	Status      string `json:"status"`
	Date        string `json:"documentProcessDate"`
	Description string `json:"description"`
	Amount      struct {
		Amount   json.RawMessage `json:"amount"`
		Currency string          `json:"currency"`
	} `json:"Amount"`
	Debtor struct {
		INN  string `json:"inn"`
		Name string `json:"name"`
	} `json:"DebtorParty"`
}

func NewBankClient(injected *http.Client) (*BankClient, error) {
	client := injected
	if client == nil {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(russianTrustedRoot) {
			return nil, errors.New("business bank public CA is invalid")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		client = &http.Client{Transport: transport, Timeout: 20 * time.Second}
	} else {
		clone := *client
		client = &clone
		if client.Timeout == 0 {
			client.Timeout = 20 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &BankClient{http: client, pollInterval: time.Second}, nil
}

func (c *BankClient) request(ctx context.Context, token, method, path string, body, target any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return bankFailure()
		}
		input = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, bankBase+path, input)
	if err != nil {
		return bankFailure()
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return bankFailure()
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return &bankRequestError{status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return bankFailure()
	}
	if err := json.Unmarshal(data, target); err != nil {
		return bankFailure()
	}
	return nil
}

func (c *BankClient) Accounts(ctx context.Context, token string) ([]BankAccount, error) {
	var response struct {
		Data struct {
			Accounts []BankAccount `json:"Account"`
		} `json:"Data"`
		Meta bankMeta `json:"Meta"`
	}
	err := c.request(ctx, token, http.MethodGet, "/open-banking/v1.0/accounts", nil, &response)
	if bankReadDenied(err) {
		session, initErr := c.readonlyMCP(ctx, token)
		if initErr != nil {
			return nil, initErr
		}
		defer session.close()
		err = session.read(http.MethodGet, "/open-banking/v1.0/accounts", nil, &response)
	}
	if err != nil {
		return nil, err
	}
	if response.Data.Accounts == nil || response.Meta.TotalPages != 1 || len(response.Data.Accounts) > 20 {
		return nil, bankFailure()
	}
	accounts := []BankAccount{}
	seen := map[string]bool{}
	for _, account := range response.Data.Accounts {
		if account.Currency != "RUB" || account.Status != "Enabled" {
			continue
		}
		if !bankAccountID.MatchString(account.ID) || !bankCustomerCode.MatchString(account.CustomerCode) || seen[account.ID] {
			return nil, bankFailure()
		}
		seen[account.ID] = true
		accounts = append(accounts, account)
	}
	if len(accounts) == 0 {
		return nil, unavailable("У подключения нет доступных рублёвых счетов")
	}
	return accounts, nil
}

func (c *BankClient) Statements(ctx context.Context, token string, accounts []BankAccount, from, to string) ([]Payment, error) {
	var session *bankMCPSession
	defer func() {
		if session != nil {
			session.close()
		}
	}()
	read := func(method, path string, body, target any) error {
		if session != nil {
			return session.read(method, path, body, target)
		}
		err := c.request(ctx, token, method, path, body, target)
		if !bankReadDenied(err) {
			return err
		}
		var initErr error
		session, initErr = c.readonlyMCP(ctx, token)
		if initErr != nil {
			return initErr
		}
		return session.read(method, path, body, target)
	}
	payments := []Payment{}
	var pending []struct {
		account BankAccount
		id      string
	}
	for _, account := range accounts {
		var input struct {
			Data struct {
				Statement struct {
					AccountID string `json:"accountId"`
					From      string `json:"startDateTime"`
					To        string `json:"endDateTime"`
				} `json:"Statement"`
			} `json:"Data"`
		}
		input.Data.Statement.AccountID, input.Data.Statement.From, input.Data.Statement.To = account.ID, from, to
		var initialized struct {
			Data struct {
				Statement bankStatement `json:"Statement"`
			} `json:"Data"`
		}
		if err := read(http.MethodPost, "/open-banking/v1.0/statements", input, &initialized); err != nil {
			return nil, err
		}
		id := initialized.Data.Statement.ID
		if !bankResourceID.MatchString(id) || initialized.Data.Statement.AccountID != account.ID {
			return nil, bankFailure()
		}
		pending = append(pending, struct {
			account BankAccount
			id      string
		}{account: account, id: id})
	}
	// Start every report before polling so normal bank generation overlaps.
	for _, report := range pending {
		account, id := report.account, report.id
		ready := false
		for attempt := 0; attempt < 60; attempt++ {
			var response struct {
				Data struct {
					Statements []bankStatement `json:"Statement"`
				} `json:"Data"`
				Meta bankMeta `json:"Meta"`
			}
			if err := read(http.MethodGet, "/open-banking/v1.0/accounts/"+account.ID+"/statements/"+id, nil, &response); err != nil {
				return nil, err
			}
			if response.Meta.TotalPages != 1 || len(response.Data.Statements) != 1 {
				return nil, bankFailure()
			}
			statement := response.Data.Statements[0]
			if statement.AccountID != account.ID || statement.ID != id || statement.From != from || statement.To != to {
				return nil, bankFailure()
			}
			if statement.Status == "Ready" {
				if len(statement.Transactions)+len(payments) > 10000 {
					return nil, bankFailure()
				}
				for _, transaction := range statement.Transactions {
					if !strings.EqualFold(transaction.Direction, "Credit") {
						continue
					}
					if !strings.EqualFold(transaction.Status, "Booked") || transaction.Amount.Currency != "RUB" || !validDate(transaction.Date) || transaction.Date < from || transaction.Date > to {
						return nil, bankFailure()
					}
					providerID := transaction.ID
					if providerID == "" {
						providerID = transaction.PaymentID
					}
					if providerID == "" || len(providerID) > 256 {
						return nil, bankFailure()
					}
					amount, err := bankAmount(transaction.Amount.Amount)
					if err != nil {
						return nil, err
					}
					hash := sha256.Sum256([]byte(account.ID + "\x00" + providerID))
					inn := strings.TrimSpace(transaction.Debtor.INN)
					if !validINN(inn, true) {
						inn = ""
					}
					payment := Payment{ID: "tochka-" + hex.EncodeToString(hash[:]), Date: transaction.Date, AmountMinor: amount, Currency: "RUB", CounterpartyName: transaction.Debtor.Name, PayerINN: inn, Description: transaction.Description, AccountLabel: "Точка · " + account.ID[16:20], Source: "tochka"}
					if err := normalizePayment(&payment); err != nil {
						return nil, bankFailure()
					}
					payments = append(payments, payment)
				}
				ready = true
				break
			}
			if statement.Status != "Created" && statement.Status != "Processing" {
				return nil, bankFailure()
			}
			timer := time.NewTimer(c.pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, bankFailure()
			case <-timer.C:
			}
		}
		if !ready {
			return nil, bankFailure()
		}
	}
	return payments, nil
}

func bankAmount(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, bankFailure()
		}
	}
	amount, ok := new(big.Rat).SetString(value)
	if !ok || amount.Sign() <= 0 {
		return 0, bankFailure()
	}
	amount.Mul(amount, big.NewRat(100, 1))
	if !amount.IsInt() || !amount.Num().IsInt64() || amount.Num().Int64() > maxMinor {
		return 0, bankFailure()
	}
	return amount.Num().Int64(), nil
}
func bankDecimal(minor int64) json.Number {
	return json.Number(strconv.FormatInt(minor/100, 10) + "." + fmtTwo(minor%100))
}
func fmtTwo(value int64) string {
	if value < 10 {
		return "0" + strconv.FormatInt(value, 10)
	}
	return strconv.FormatInt(value, 10)
}

type bankPosition struct {
	Name     string      `json:"positionName"`
	Unit     string      `json:"unitCode"`
	VAT      string      `json:"ndsKind"`
	Price    json.Number `json:"price"`
	Quantity json.Number `json:"quantity"`
	Amount   json.Number `json:"totalAmount"`
}

func (c *BankClient) CreateInvoice(ctx context.Context, token string, seller Seller, buyer Counterparty, invoice Invoice) (string, error) {
	accounts, err := c.Accounts(ctx, token)
	if err != nil {
		return "", &rejectedBankCreate{cause: err}
	}
	var selected *BankAccount
	for i := range accounts {
		if accounts[i].ID == seller.BankAccount+"/"+seller.BankBIK && accounts[i].Type == "Business" {
			selected = &accounts[i]
			break
		}
	}
	if selected == nil {
		return "", &rejectedBankCreate{cause: invalid("Расчётный счёт продавца не найден среди доступных счетов банка")}
	}
	var input struct {
		Data struct {
			AccountID    string `json:"accountId"`
			CustomerCode string `json:"customerCode"`
			Buyer        struct {
				INN     string `json:"taxCode"`
				Type    string `json:"type"`
				Name    string `json:"secondSideName"`
				Address string `json:"legalAddress"`
			} `json:"SecondSide"`
			Content struct {
				Invoice struct {
					Positions []bankPosition `json:"Positions"`
					Date      string         `json:"date"`
					Amount    json.Number    `json:"totalAmount"`
					Number    string         `json:"number"`
					Basis     string         `json:"basedOn"`
					Due       string         `json:"paymentExpiryDate,omitempty"`
				} `json:"Invoice"`
			} `json:"Content"`
		} `json:"Data"`
	}
	input.Data.AccountID, input.Data.CustomerCode = selected.ID, selected.CustomerCode
	input.Data.Buyer.INN, input.Data.Buyer.Type, input.Data.Buyer.Name, input.Data.Buyer.Address = buyer.INN, buyer.EntityType, buyer.Name, buyer.Address
	bill := &input.Data.Content.Invoice
	bill.Date, bill.Amount, bill.Number, bill.Basis, bill.Due = invoice.IssueDate, bankDecimal(invoice.AmountMinor), invoice.Number, invoice.Basis, invoice.DueDate
	for _, item := range invoice.Items {
		unit := bankUnit(item.Unit)
		if unit == "" {
			return "", &rejectedBankCreate{cause: invalid("Единица измерения не поддерживается банком")}
		}
		bill.Positions = append(bill.Positions, bankPosition{Name: item.Description, Unit: unit, VAT: "without_nds", Price: bankDecimal(item.PriceMinor), Quantity: json.Number(item.Quantity), Amount: bankDecimal(item.AmountMinor)})
	}
	var response struct {
		Data struct {
			DocumentID string `json:"documentId"`
		} `json:"Data"`
	}
	if err := c.request(ctx, token, http.MethodPost, "/invoice/v1.0/bills", input, &response); err != nil {
		return "", err
	}
	if !bankResourceID.MatchString(response.Data.DocumentID) {
		return "", bankFailure()
	}
	return response.Data.DocumentID, nil
}
func bankUnit(unit string) string {
	switch strings.TrimSpace(unit) {
	case "", "мес", "мес.", "месяц":
		return "усл.ед."
	case "шт":
		return "шт."
	case "услуга":
		return "услуга."
	case "м²", "м2":
		return "м2."
	case "шт.", "тыс.шт.", "компл.", "пар.", "усл.ед.", "упак.", "услуга.", "пач.", "мин.", "ч.", "сут.", "г.", "кг.", "л.", "м.", "м2.", "м3.", "км.", "га.", "кВт.", "кВт.ч.":
		return strings.TrimSpace(unit)
	}
	return ""
}
