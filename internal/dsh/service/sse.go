package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// MuxFrame is one DSH events.mux data payload: {rpcId, payload}.
type MuxFrame struct {
	RPCID   uint64          `json:"rpcId"`
	Payload json.RawMessage `json:"payload"`
}

// ParseMuxStream reads a text/event-stream. Comment lines (including the
// leading ": connected") are skipped; each data event is decoded as MuxFrame.
// Reconnect / backoff belong to a later stage.
func ParseMuxStream(r io.Reader, emit func(MuxFrame) error) error {
	br := bufio.NewReader(r)
	var data bytes.Buffer
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		var frame MuxFrame
		if err := json.Unmarshal(data.Bytes(), &frame); err != nil {
			return fmt.Errorf("dsh sse: %w", err)
		}
		data.Reset()
		if emit == nil {
			return nil
		}
		return emit(frame)
	}
	for {
		line, err := br.ReadString('\n')
		eof := err == io.EOF
		if err != nil && !eof {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// comment, e.g. ": connected"
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(payload, " ") {
				payload = payload[1:]
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(payload)
		}
		if eof {
			if err := flush(); err != nil {
				return err
			}
			return nil
		}
	}
}
