package session

import "errors"

var (
	ErrNicknameInUse = errors.New("session: nickname already in use")
)
