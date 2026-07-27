package service

import "errors"

// ConflictError maps to HTTP 409 (referenced resource cannot be deleted / state conflict).
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }

func NewConflict(msg string) error {
	return &ConflictError{Message: msg}
}

func IsConflict(err error) bool {
	_, ok := errors.AsType[*ConflictError](err)
	return ok
}

// NotFoundError maps to HTTP 404.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

func NewNotFound(msg string) error {
	return &NotFoundError{Message: msg}
}

func IsNotFound(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// ForbiddenError maps to HTTP 403.
type ForbiddenError struct {
	Message string
}

func (e *ForbiddenError) Error() string { return e.Message }

func NewForbidden(msg string) error {
	return &ForbiddenError{Message: msg}
}

func IsForbidden(err error) bool {
	_, ok := errors.AsType[*ForbiddenError](err)
	return ok
}
