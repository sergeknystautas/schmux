package repo

import (
	"errors"

	"github.com/sergeknystautas/schmux/internal/state"
)

var (
	// ErrNotFound is returned when no repo entry has the given name.
	ErrNotFound = state.ErrRepoNotFound
	// ErrDuplicateName is returned when an entry with the same name is stored.
	ErrDuplicateName = state.ErrRepoDuplicate
	// ErrInvalid is returned when the entry cannot be stored as given.
	ErrInvalid = errors.New("invalid repo")
)
