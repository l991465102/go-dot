package tracking

import (
	"errors"
)

type CategoryLevel string

const (
	Domain CategoryLevel = "DOMAIN"
	Module CategoryLevel = "MODULE"
	Scene  CategoryLevel = "SCENE"
)

var ErrCategoryNotFound = errors.New("CATEGORY_NOT_FOUND")

type Category struct {
	ID          uint64        `json:"id"`
	ProjectID   uint64        `json:"project_id"`
	ParentID    *uint64       `json:"parent_id,omitempty"`
	Code        string        `json:"code"`
	Name        string        `json:"name"`
	Level       CategoryLevel `json:"level"`
	Description string        `json:"description,omitempty"`
	SortOrder   int           `json:"sort_order"`
	Active      bool          `json:"active"`
	CreatedBy   uint64        `json:"created_by"`
}

type CategoryRef struct {
	ID    uint64        `json:"id"`
	Code  string        `json:"code"`
	Name  string        `json:"name"`
	Level CategoryLevel `json:"level"`
}

type CreateCategory struct {
	ProjectID   uint64        `json:"project_id"`
	ParentID    *uint64       `json:"parent_id,omitempty"`
	Code        string        `json:"code"`
	Name        string        `json:"name"`
	Level       CategoryLevel `json:"level"`
	Description string        `json:"description,omitempty"`
	SortOrder   int           `json:"sort_order"`
}

type UpdateCategory struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sort_order"`
	Active      bool   `json:"active"`
}

func (in CreateCategory) validate(parent *Category) error {
	if in.ProjectID == 0 || in.Code == "" || in.Name == "" {
		return coded("CATEGORY_INVALID", "project_id, code and name are required")
	}
	switch in.Level {
	case Domain:
		if in.ParentID != nil {
			return coded("CATEGORY_INVALID", "DOMAIN cannot have a parent")
		}
	case Module:
		if parent == nil || parent.Level != Domain {
			return coded("CATEGORY_INVALID", "MODULE parent must be DOMAIN")
		}
	case Scene:
		if parent == nil || parent.Level != Module {
			return coded("CATEGORY_INVALID", "SCENE parent must be MODULE")
		}
	default:
		return coded("CATEGORY_INVALID", "invalid category level")
	}
	if parent != nil && (parent.ProjectID != in.ProjectID || !parent.Active) {
		return coded("CATEGORY_INVALID", "parent must be active and in the same project")
	}
	return nil
}
