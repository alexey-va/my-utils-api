package business

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	bankMCPEndpoint        = "https://api.tochka.com/mcp"
	bankMCPProtocol        = "2025-03-26"
	mcpAccountsTool        = "openapi_mcp.getAccountsList"
	mcpCreateStatementTool = "openapi_mcp.createStatement"
	mcpReadStatementTool   = "openapi_mcp.readStatement"
	maxMCPBytes            = 16 << 20
)

var mcpStatementPath = regexp.MustCompile(`^/open-banking/v1\.0/accounts/([0-9]{20}/[0-9]{9})/statements/([A-Za-z0-9_-]{1,128})$`)

type bankMCPSession struct {
	client   *BankClient
	ctx      context.Context
	cancel   context.CancelFunc
	token    string
	id       string
	protocol string
	nextID   int
	tools    map[string]bool
}
type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func bankReadDenied(err error) bool {
	var response *bankRequestError
	return errors.As(err, &response) && (response.status == http.StatusUnauthorized || response.status == http.StatusForbidden)
}

func (c *BankClient) readonlyMCP(ctx context.Context, token string) (*bankMCPSession, error) {
	privateCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	session := &bankMCPSession{client: c, ctx: privateCtx, cancel: cancel, token: token, tools: map[string]bool{}}
	ready := false
	defer func() {
		if !ready {
			session.close()
		}
	}()
	var init struct {
		Protocol string `json:"protocolVersion"`
	}
	params := struct {
		Protocol     string   `json:"protocolVersion"`
		Capabilities struct{} `json:"capabilities"`
		ClientInfo   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}{Protocol: bankMCPProtocol}
	params.ClientInfo.Name, params.ClientInfo.Version = "my-utils-business-readonly", "1.0"
	if err := session.rpc("initialize", params, &init); err != nil {
		return nil, err
	}
	if init.Protocol != bankMCPProtocol {
		return nil, bankFailure()
	}
	session.protocol = init.Protocol
	if err := session.rpc("notifications/initialized", nil, nil); err != nil {
		return nil, err
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		NextCursor string `json:"nextCursor"`
	}
	if err := session.rpc("tools/list", struct{}{}, &catalog); err != nil {
		return nil, err
	}
	if len(catalog.Tools) > 100 || catalog.NextCursor != "" {
		return nil, bankFailure()
	}
	seen := map[string]bool{}
	for _, tool := range catalog.Tools {
		if seen[tool.Name] {
			return nil, bankFailure()
		}
		seen[tool.Name] = true
	}
	for _, name := range []string{mcpAccountsTool, mcpCreateStatementTool, mcpReadStatementTool} {
		if !seen[name] {
			return nil, bankFailure()
		}
		session.tools[name] = true
	}
	ready = true
	return session, nil
}

func (s *bankMCPSession) headers(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.Header.Set("Accept", "application/json, text/event-stream")
	if s.protocol != "" {
		request.Header.Set("MCP-Protocol-Version", s.protocol)
	}
	if s.id != "" {
		request.Header.Set("Mcp-Session-Id", s.id)
	}
}

func (s *bankMCPSession) close() {
	if s.id != "" {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 2*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodDelete, bankMCPEndpoint, nil)
		if err == nil {
			s.headers(request)
			if response, err := s.client.http.Do(request); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				_ = response.Body.Close()
			}
		}
		cancel()
	}
	s.cancel()
}

func (s *bankMCPSession) rpc(method string, params, target any) error {
	payload := mcpRequest{JSONRPC: "2.0", Method: method, Params: params}
	if target != nil {
		s.nextID++
		id := s.nextID
		payload.ID = &id
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return bankFailure()
	}
	request, err := http.NewRequestWithContext(s.ctx, http.MethodPost, bankMCPEndpoint, bytes.NewReader(data))
	if err != nil {
		return bankFailure()
	}
	s.headers(request)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.http.Do(request)
	if err != nil {
		return bankFailure()
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return bankFailure()
	}
	if id := response.Header.Get("Mcp-Session-Id"); id != "" {
		if len(id) > 512 || strings.IndexFunc(id, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 || s.id != "" && s.id != id {
			return bankFailure()
		}
		s.id = id
	}
	if target == nil {
		if response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusNoContent {
			return bankFailure()
		}
		return nil
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return bankFailure()
	}
	if contentType == "text/event-stream" {
		return readMCPEvent(response.Body, *payload.ID, target)
	}
	if contentType != "application/json" {
		return bankFailure()
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, maxMCPBytes+1))
	if err != nil || len(data) > maxMCPBytes {
		return bankFailure()
	}
	matched, err := decodeMCPResponse(data, *payload.ID, target)
	if err != nil || !matched {
		return bankFailure()
	}
	return nil
}

func decodeMCPResponse(data []byte, id int, target any) (bool, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return false, bankFailure()
	}
	if data[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(data, &batch); err != nil || len(batch) == 0 || len(batch) > 32 {
			return false, bankFailure()
		}
		found := false
		for _, message := range batch {
			matched, err := decodeMCPResponse(message, id, target)
			if err != nil || matched && found {
				return false, bankFailure()
			}
			found = found || matched
		}
		return found, nil
	}
	var message mcpResponse
	if err := json.Unmarshal(data, &message); err != nil || message.JSONRPC != "2.0" {
		return false, bankFailure()
	}
	if string(message.ID) != strconv.Itoa(id) {
		return false, nil
	}
	if len(message.Error) > 0 && string(message.Error) != "null" || len(message.Result) == 0 || string(message.Result) == "null" {
		return false, bankFailure()
	}
	if err := json.Unmarshal(message.Result, target); err != nil {
		return false, bankFailure()
	}
	return true, nil
}

func readMCPEvent(reader io.Reader, id int, target any) error {
	limited := &io.LimitedReader{R: reader, N: maxMCPBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 16<<10), maxMCPBytes+1)
	var data strings.Builder
	flush := func() (bool, error) {
		if data.Len() == 0 {
			return false, nil
		}
		message := data.String()
		data.Reset()
		return decodeMCPResponse([]byte(message), id, target)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			matched, err := flush()
			if err != nil {
				return err
			}
			if matched {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if scanner.Err() != nil || limited.N <= 0 {
		return bankFailure()
	}
	matched, err := flush()
	if err != nil || !matched {
		return bankFailure()
	}
	return nil
}

func (s *bankMCPSession) call(name string, arguments, target any) error {
	if (name != mcpAccountsTool && name != mcpCreateStatementTool && name != mcpReadStatementTool) || !s.tools[name] {
		return bankFailure()
	}
	params := struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
	}{Name: name, Arguments: arguments}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := s.rpc("tools/call", params, &result); err != nil {
		return err
	}
	if result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" || len(result.Content[0].Text) > maxMCPBytes {
		return bankFailure()
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), target); err != nil {
		return bankFailure()
	}
	return nil
}

// Adapt the three fixed read tools' root Account/Statement objects to the
// native Data envelope so both transports share money, scope and limit checks.
func (s *bankMCPSession) read(method, path string, body, target any) error {
	var root json.RawMessage
	switch {
	case method == http.MethodGet && path == "/open-banking/v1.0/accounts":
		if err := s.call(mcpAccountsTool, struct{}{}, &root); err != nil {
			return err
		}
	case method == http.MethodPost && path == "/open-banking/v1.0/statements":
		var input struct {
			Data struct {
				Statement struct {
					AccountID string `json:"accountId"`
					From      string `json:"startDateTime"`
					To        string `json:"endDateTime"`
				} `json:"Statement"`
			} `json:"Data"`
		}
		data, err := json.Marshal(body)
		if err != nil || json.Unmarshal(data, &input) != nil {
			return bankFailure()
		}
		args := input.Data.Statement
		if !bankAccountID.MatchString(args.AccountID) || !validDate(args.From) || !validDate(args.To) || args.From > args.To {
			return bankFailure()
		}
		if err := s.call(mcpCreateStatementTool, args, &root); err != nil {
			return err
		}
	case method == http.MethodGet:
		parts := mcpStatementPath.FindStringSubmatch(path)
		if len(parts) != 3 {
			return bankFailure()
		}
		args := struct {
			AccountID   string `json:"accountId"`
			StatementID string `json:"statementId"`
		}{AccountID: parts[1], StatementID: parts[2]}
		if err := s.call(mcpReadStatementTool, args, &root); err != nil {
			return err
		}
	default:
		return bankFailure()
	}
	// The MCP output schemas return exhaustive lists without Meta. Reject any
	// explicit partial marker rather than manufacturing complete coverage.
	var pagination struct {
		Meta       *bankMeta `json:"Meta"`
		NextCursor string    `json:"nextCursor"`
	}
	root = bytes.TrimSpace(root)
	if len(root) == 0 || root[0] != '{' || json.Unmarshal(root, &pagination) != nil || pagination.NextCursor != "" || pagination.Meta != nil && pagination.Meta.TotalPages != 1 {
		return bankFailure()
	}
	envelope := struct {
		Data json.RawMessage `json:"Data"`
		Meta bankMeta        `json:"Meta"`
	}{Data: root, Meta: bankMeta{TotalPages: 1}}
	data, err := json.Marshal(envelope)
	if err != nil || json.Unmarshal(data, target) != nil {
		return bankFailure()
	}
	return nil
}
