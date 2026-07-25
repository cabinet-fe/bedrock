package service

import "errors"

var ErrAIDomainUnavailable = errors.New("文档生成依赖 AI 域（P4），当前未启用")

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
func NewConflict(message string) error { return &ConflictError{Message: message} }
func IsConflict(err error) bool {
	_, ok := errors.AsType[*ConflictError](err)
	return ok
}

type ForbiddenError struct{ Message string }

func (e *ForbiddenError) Error() string { return e.Message }
func NewForbidden(message string) error { return &ForbiddenError{Message: message} }
func IsForbidden(err error) bool {
	_, ok := errors.AsType[*ForbiddenError](err)
	return ok
}

type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }
func NewNotFound(message string) error { return &NotFoundError{Message: message} }
func IsNotFound(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}
