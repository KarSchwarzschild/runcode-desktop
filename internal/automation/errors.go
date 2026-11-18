package automation

import "errors"

var (
	ErrConflict   = errors.New("automation: revision conflict")
	ErrNotFound   = errors.New("automation: not found")
	ErrTaskActive = errors.New("automation: task has active run")
)
