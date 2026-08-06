package service

import (
	"errors"
	"fmt"
	"strings"

	"bedrock/internal/pkg"
	"bedrock/internal/system/model"
	"bedrock/internal/system/repository"
)

type DictionaryService struct {
	dicts *repository.DictionaryRepository
}

func NewDictionaryService(dicts *repository.DictionaryRepository) *DictionaryService {
	return &DictionaryService{dicts: dicts}
}

func (s *DictionaryService) List(q pkg.ListQuery) ([]model.Dictionary, int64, error) {
	return s.dicts.List(q)
}

func (s *DictionaryService) Get(id uint) (*model.Dictionary, error) {
	return s.dicts.FindByID(id)
}

func (s *DictionaryService) GetByCode(code string) (*model.Dictionary, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("编码不能为空")
	}
	return s.dicts.FindByCode(code)
}

func (s *DictionaryService) Create(name, code, description string, items []model.DictItem) (*model.Dictionary, error) {
	name = strings.TrimSpace(name)
	code = strings.TrimSpace(code)
	if name == "" || code == "" {
		return nil, errors.New("名称与编码不能为空")
	}
	d := &model.Dictionary{Name: name, Code: code, Description: description}
	if err := s.dicts.Create(d); err != nil {
		return nil, fmt.Errorf("创建字典失败: %w", err)
	}
	if len(items) > 0 {
		if err := s.dicts.ReplaceItems(d.ID, items); err != nil {
			return nil, err
		}
	}
	return s.dicts.FindByID(d.ID)
}

func (s *DictionaryService) Update(id uint, name, description string, items *[]model.DictItem) (*model.Dictionary, error) {
	d, err := s.dicts.FindByID(id)
	if err != nil {
		return nil, err
	}
	if name = strings.TrimSpace(name); name != "" {
		d.Name = name
	}
	d.Description = description
	if err := s.dicts.Update(d); err != nil {
		return nil, err
	}
	if items != nil {
		if err := s.dicts.ReplaceItems(id, *items); err != nil {
			return nil, err
		}
	}
	return s.dicts.FindByID(id)
}

func (s *DictionaryService) Delete(id uint) error {
	return s.dicts.Delete(id)
}
