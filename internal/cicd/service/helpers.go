package service

import (
	projectrepo "bedrock/internal/project/repository"
)

func errorsNew(msg string) error {
	return &validationError{msg}
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func nilIfZero(p *uint) *uint {
	if p == nil || *p == 0 {
		return nil
	}
	return p
}

// resolveProjectID validates project exists (soft-deleted 视为不存在); 0/nil → nil.
func resolveProjectID(projects *projectrepo.ProjectRepository, id *uint) (*uint, error) {
	id = nilIfZero(id)
	if id == nil {
		return nil, nil
	}
	if projects == nil {
		return nil, errorsNew("所属项目不存在")
	}
	if _, err := projects.FindProject(*id); err != nil {
		return nil, errorsNew("所属项目不存在")
	}
	return id, nil
}
