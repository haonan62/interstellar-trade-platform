package domain

import "net/http"

type AppError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Reason is for logs and audit only; it is never sent to the client, so a
	// failed login can be recorded precisely without telling an attacker why.
	Reason string `json:"-"`
}

func (e *AppError) Error() string { return e.Message }

func BadRequest(msg string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}
func Unauthorized(msg string) *AppError {
	return &AppError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}
func Forbidden(msg string) *AppError {
	return &AppError{Status: http.StatusForbidden, Code: "forbidden", Message: msg}
}
func NotFound(msg string) *AppError {
	return &AppError{Status: http.StatusNotFound, Code: "not_found", Message: msg}
}
func Conflict(msg string) *AppError {
	return &AppError{Status: http.StatusConflict, Code: "conflict", Message: msg}
}
func InternalErr(msg string) *AppError {
	return &AppError{Status: http.StatusInternalServerError, Code: "internal_error", Message: msg}
}
