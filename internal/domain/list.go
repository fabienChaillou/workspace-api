package domain

import (
	"fmt"
	"strings"
)

// Pagination defaults and bounds.
const (
	DefaultPage  = 1
	DefaultLimit = 10
	MaxLimit     = 100
)

// SortField is a column the workspace list can be ordered by.
type SortField string

const (
	// SortByActionsUpdatedAt orders by the most recent UpdatedAt of the
	// workspace's actions (default). Workspaces without actions come last.
	SortByActionsUpdatedAt SortField = "actions_updated_at"
	SortByUpdatedAt        SortField = "updated_at"
	SortByCreatedAt        SortField = "created_at"
	SortByName             SortField = "name"
)

// SortOrder is asc or desc.
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// QueryParams are the filters of the workspace list. Empty fields are ignored.
type QueryParams struct {
	Name       string          // case-insensitive "contains" match
	Status     WorkspaceStatus // exact match
	Namespace  string          // exact match
	ActionType ActionType      // workspaces having at least one live action of this type
}

// Pagination describes a page of results. Page is 1-based.
type Pagination struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalRows  int64 `json:"total_rows"`
	TotalPages int   `json:"total_pages"`
}

// Offset returns the number of rows to skip for this page.
func (p Pagination) Offset() int { return (p.Page - 1) * p.Limit }

// WithTotal returns a copy of p with TotalRows and TotalPages filled.
func (p Pagination) WithTotal(total int64) Pagination {
	p.TotalRows = total
	p.TotalPages = 0
	if p.Limit > 0 {
		p.TotalPages = int((total + int64(p.Limit) - 1) / int64(p.Limit))
	}
	return p
}

// ListWorkspacesOptions groups filters, sorting and pagination.
type ListWorkspacesOptions struct {
	Filter     QueryParams
	Sort       SortField
	Order      SortOrder
	Pagination Pagination
}

// WorkspacePage is one page of the workspace list.
type WorkspacePage struct {
	Items      []Workspace
	Pagination Pagination
}

// Normalize applies defaults and validates the options.
func (o ListWorkspacesOptions) Normalize() (ListWorkspacesOptions, error) {
	o.Filter.Name = strings.TrimSpace(o.Filter.Name)
	o.Filter.Namespace = strings.TrimSpace(o.Filter.Namespace)
	if o.Filter.Status != "" && !o.Filter.Status.Valid() {
		return o, fmt.Errorf("status %q: %w", o.Filter.Status, ErrInvalid)
	}
	if o.Filter.ActionType != "" && !o.Filter.ActionType.Valid() {
		return o, fmt.Errorf("actionType %q: %w", o.Filter.ActionType, ErrInvalid)
	}

	switch o.Sort {
	case "":
		o.Sort = SortByActionsUpdatedAt
	case SortByActionsUpdatedAt, SortByUpdatedAt, SortByCreatedAt, SortByName:
	default:
		return o, fmt.Errorf("sort %q: %w", o.Sort, ErrInvalid)
	}
	switch o.Order {
	case "":
		o.Order = SortDesc
	case SortAsc, SortDesc:
	default:
		return o, fmt.Errorf("order %q: %w", o.Order, ErrInvalid)
	}

	if o.Pagination.Page == 0 {
		o.Pagination.Page = DefaultPage
	}
	if o.Pagination.Limit == 0 {
		o.Pagination.Limit = DefaultLimit
	}
	if o.Pagination.Page < 1 {
		return o, fmt.Errorf("page must be >= 1: %w", ErrInvalid)
	}
	if o.Pagination.Limit < 1 || o.Pagination.Limit > MaxLimit {
		return o, fmt.Errorf("limit must be between 1 and %d: %w", MaxLimit, ErrInvalid)
	}
	return o, nil
}
