package business

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool         *pgxpool.Pool
	bank         *BankClient
	cipher       *tokenCipher
	now          func() time.Time
	calendarZone *time.Location
}

func NewService(pool *pgxpool.Pool, jwtSecret string, bank *BankClient) (*Service, error) {
	cipher, err := newTokenCipher(jwtSecret)
	if err != nil {
		return nil, err
	}
	calendarZone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, bank: bank, cipher: cipher, now: time.Now, calendarZone: calendarZone}, nil
}

// ponytail: one row lock serializes this owner's small ledger. Normalize the
// snapshot into ledger tables if the archive grows beyond hundreds of invoices.
func (s *Service) mutate(ctx context.Context, owner string, change func(*state, pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `INSERT INTO business_workspaces(owner_id) VALUES($1::uuid) ON CONFLICT DO NOTHING`, owner); err != nil {
		return err
	}
	var data []byte
	var connected bool
	if err := tx.QueryRow(ctx, `SELECT snapshot,bank_token_ciphertext IS NOT NULL FROM business_workspaces WHERE owner_id=$1::uuid FOR UPDATE`, owner).Scan(&data, &connected); err != nil {
		return err
	}
	current := emptyState()
	if err := json.Unmarshal(data, &current); err != nil {
		return err
	}
	current.Bank.Connected = connected
	if err := change(&current, tx); err != nil {
		return err
	}
	if err := validateState(current); err != nil {
		return err
	}
	data, err = json.Marshal(current)
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return invalid("Достигнут предел размера личного архива")
	}
	if _, err := tx.Exec(ctx, `UPDATE business_workspaces SET snapshot=$2,updated_at=now() WHERE owner_id=$1::uuid`, owner, data); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) load(ctx context.Context, owner string) (state, error) {
	current := emptyState()
	var data []byte
	var connected bool
	err := s.pool.QueryRow(ctx, `SELECT snapshot,bank_token_ciphertext IS NOT NULL FROM business_workspaces WHERE owner_id=$1::uuid`, owner).Scan(&data, &connected)
	if errors.Is(err, pgx.ErrNoRows) {
		return current, nil
	}
	if err != nil {
		return current, err
	}
	if err := json.Unmarshal(data, &current); err != nil {
		return current, err
	}
	current.Bank.Connected = connected
	return current, nil
}

func (s *Service) Workspace(ctx context.Context, owner, period, party string) (Workspace, error) {
	if period != "" && period != "all" {
		if _, _, err := periodDates(period); err != nil {
			return Workspace{}, err
		}
	}
	current, err := s.load(ctx, owner)
	if err != nil {
		return Workspace{}, err
	}
	if party != "" {
		if _, ok := findParty(current, party); !ok {
			return Workspace{}, missing("Плательщик не найден")
		}
	}
	view := workspaceView(current, period, party)
	view.setPreviousPeriodDocuments(current.Invoices, s.now().In(s.calendarZone))
	return view, nil
}

func (s *Service) SetCounterpartyInactive(ctx context.Context, owner, id string, inactive bool) error {
	if !validID(id) {
		return missing("Плательщик не найден")
	}
	return s.mutate(ctx, owner, func(current *state, _ pgx.Tx) error {
		for i := range current.Counterparties {
			if current.Counterparties[i].ID == id {
				current.Counterparties[i].Inactive = inactive
				return nil
			}
		}
		return missing("Плательщик не найден")
	})
}

func (s *Service) File(ctx context.Context, owner, id string) (File, error) {
	var file File
	if !validID(id) {
		return file, missing("Документ не найден")
	}
	err := s.pool.QueryRow(ctx, `SELECT id,filename,mime_type,sha256,data FROM business_files WHERE owner_id=$1::uuid AND id=$2`, owner, id).Scan(&file.ID, &file.Filename, &file.MIMEType, &file.SHA256, &file.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return file, missing("Документ не найден")
	}
	file.Size = len(file.Data)
	return file, err
}

func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "" {
		return "document"
	}
	if len(name) > 240 {
		return "document" + filepath.Ext(name)
	}
	return name
}

func saveFile(ctx context.Context, tx pgx.Tx, owner, id, filename, mimeType string, data []byte) (Document, error) {
	if !validID(id) || len(data) == 0 || len(data) > maxDocumentBytes {
		return Document{}, invalid("Неверный размер или идентификатор документа")
	}
	switch mimeType {
	case "application/pdf":
		if !strings.HasPrefix(string(data[:min(len(data), 8)]), "%PDF-") {
			return Document{}, invalid("Документ не является PDF")
		}
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		if len(data) < 4 || string(data[:2]) != "PK" {
			return Document{}, invalid("Неверный формат документа Office")
		}
	default:
		return Document{}, invalid("Поддерживаются PDF, DOCX и XLSX")
	}
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	var oldSHA string
	err := tx.QueryRow(ctx, `SELECT sha256 FROM business_files WHERE owner_id=$1::uuid AND id=$2`, owner, id).Scan(&oldSHA)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Document{}, err
	}
	if err == nil && oldSHA != sha {
		return Document{}, conflict("Документ с этим идентификатором уже содержит другие данные")
	}
	var existing Document
	err = tx.QueryRow(ctx, `SELECT id,filename,mime_type,sha256,octet_length(data) FROM business_files WHERE owner_id=$1::uuid AND sha256=$2`, owner, sha).Scan(&existing.ID, &existing.Filename, &existing.MIMEType, &existing.SHA256, &existing.Size)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Document{}, err
	}
	doc := Document{ID: id, Filename: safeFilename(filename), MIMEType: mimeType, Size: len(data), SHA256: sha}
	_, err = tx.Exec(ctx, `INSERT INTO business_files(owner_id,id,filename,mime_type,sha256,data) VALUES($1::uuid,$2,$3,$4,$5,$6)`, owner, id, doc.Filename, doc.MIMEType, doc.SHA256, data)
	return doc, err
}

func importedFile(ctx context.Context, tx pgx.Tx, owner string, item ImportDocument) (Document, error) {
	if len(item.DataBase64) > base64.StdEncoding.EncodedLen(maxDocumentBytes) {
		return Document{}, invalid("Документ превышает 20 МБ")
	}
	data, err := base64.StdEncoding.DecodeString(item.DataBase64)
	if err != nil {
		return Document{}, invalid("Неверная кодировка документа")
	}
	return saveFile(ctx, tx, owner, item.ID, item.Filename, item.MIMEType, data)
}
