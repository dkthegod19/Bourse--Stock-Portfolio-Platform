package service

// ValidationError signals a bad request (maps to HTTP 400).
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// InvariantError signals a rejected-but-valid request, e.g. insufficient funds
// or shares (maps to HTTP 422).
type InvariantError struct{ Msg string }

func (e InvariantError) Error() string { return e.Msg }
