package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	envelopeClientRequest  = "client-request"
	envelopeServerResponse = "server-response"
	envelopeClientResponse = "client-response"
)

// RpcClient is the in-process DSH JSON-RPC client. It talks only to the
// injected base URL (callers pass http://127.0.0.1:<dsh.port>).
type RpcClient struct {
	baseURL string
	http    *http.Client
	rpcID   atomic.Uint64
}

func NewRpcClient(baseURL string, httpClient *http.Client) *RpcClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &RpcClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
	}
}

type clientRequest struct {
	Type    string `json:"type"`
	RPCID   uint64 `json:"rpcId"`
	Method  string `json:"method"`
	Payload any    `json:"payload"`
}

type serverResponse struct {
	Type   string          `json:"type"`
	RPCID  uint64          `json:"rpcId"`
	Result json.RawMessage `json:"result"`
}

type rpcOutcome struct {
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value"`
	Error *rpcErrorBody   `json:"error"`
}

type rpcErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type clientResponse struct {
	Type   string `json:"type"`
	RPCID  uint64 `json:"rpcId"`
	Result any    `json:"result"`
}

type respondReply struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

func (c *RpcClient) nextRPCID() uint64 {
	return c.rpcID.Add(1)
}

// Call posts POST /api/<method> and returns the decoded result value.
func (c *RpcClient) Call(ctx context.Context, method string, payload any) (any, error) {
	raw, err := c.call(ctx, method, payload)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("dsh rpc decode value: %w", err)
	}
	return value, nil
}

// CallRaw is Call with a typed result decoded into out.
func (c *RpcClient) CallRaw(ctx context.Context, method string, payload, out any) error {
	raw, err := c.call(ctx, method, payload)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("dsh rpc decode value: %w", err)
	}
	return nil
}

func (c *RpcClient) call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	if payload == nil {
		payload = struct{}{}
	}
	body, err := json.Marshal(clientRequest{
		Type:    envelopeClientRequest,
		RPCID:   c.nextRPCID(),
		Method:  method,
		Payload: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("dsh rpc encode: %w", err)
	}
	raw, err := c.postJSON(ctx, "/api/"+method, body)
	if err != nil {
		return nil, err
	}
	var resp serverResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("dsh rpc decode: %w", err)
	}
	if resp.Type != envelopeServerResponse {
		return nil, fmt.Errorf("dsh rpc: unexpected type %q", resp.Type)
	}
	var outcome rpcOutcome
	if err := json.Unmarshal(resp.Result, &outcome); err != nil {
		return nil, fmt.Errorf("dsh rpc decode result: %w", err)
	}
	if !outcome.OK {
		code, message := "", ""
		if outcome.Error != nil {
			code = outcome.Error.Code
			message = outcome.Error.Message
		}
		return nil, &RPCError{Code: code, Message: message}
	}
	return outcome.Value, nil
}

// Respond posts POST /api/respond with a client-response envelope.
func (c *RpcClient) Respond(ctx context.Context, rpcID uint64, result any) (accepted bool, reason string, err error) {
	body, err := json.Marshal(clientResponse{
		Type:   envelopeClientResponse,
		RPCID:  rpcID,
		Result: result,
	})
	if err != nil {
		return false, "", fmt.Errorf("dsh rpc encode: %w", err)
	}
	raw, err := c.postJSON(ctx, "/api/respond", body)
	if err != nil {
		return false, "", err
	}
	var reply respondReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return false, "", fmt.Errorf("dsh rpc decode respond: %w", err)
	}
	return reply.Accepted, reply.Reason, nil
}

func (c *RpcClient) postJSON(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dsh rpc: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dsh rpc: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dsh rpc: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dsh rpc: http %d", resp.StatusCode)
	}
	return raw, nil
}
