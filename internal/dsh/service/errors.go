package service

import (
	"errors"
	"net/http"
)

const (
	dshCodeSessionNotFound  = "session-not-found"
	dshCodeSessionConflict  = "session-conflict"
	dshCodeModelUnavailable = "model-unavailable"
	dshCodeCancelled        = "cancelled"
	dshCodeBadRequest       = "bad-request"

	MsgSessionNotFound  = "dsh-session-not-found"
	MsgSessionConflict  = "dsh-session-conflict"
	MsgModelUnavailable = "dsh-model-unavailable"
	MsgUnavailable      = "dsh-unavailable"
)

// RPCError is a DSH envelope error. Details from DSH are dropped.
type RPCError struct {
	Code    string
	Message string
}

func (e *RPCError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// MappedError is the Bedrock HTTP mapping for a DSH client error.
// Handlers should use Status / Message; do not change the global envelope.
type MappedError struct {
	Status    int
	Message   string
	Cancelled bool
}

func MapError(err error) MappedError {
	if err == nil {
		return MappedError{Status: http.StatusOK}
	}
	if rpc, ok := errors.AsType[*RPCError](err); ok {
		switch rpc.Code {
		case dshCodeSessionNotFound:
			return MappedError{Status: http.StatusNotFound, Message: MsgSessionNotFound}
		case dshCodeSessionConflict:
			return MappedError{Status: http.StatusConflict, Message: MsgSessionConflict}
		case dshCodeModelUnavailable:
			return MappedError{Status: http.StatusServiceUnavailable, Message: MsgModelUnavailable}
		case dshCodeCancelled:
			return MappedError{Status: http.StatusOK, Cancelled: true}
		case dshCodeBadRequest:
			return MappedError{Status: http.StatusBadRequest, Message: rpc.Message}
		default:
			return MappedError{Status: http.StatusBadRequest, Message: rpc.Message}
		}
	}
	return MappedError{Status: http.StatusServiceUnavailable, Message: MsgUnavailable}
}
