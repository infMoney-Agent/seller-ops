package screening

import "net/http"

type errorKind string

const (
	errUnauthenticated errorKind = "UNAUTHENTICATED"
	errForbidden       errorKind = "PERMISSION_DENIED"
	errNotFound        errorKind = "NOT_FOUND"
	errConflict        errorKind = "CONFLICT"
	errPayloadTooLarge errorKind = "PAYLOAD_TOO_LARGE"
	errValidation      errorKind = "INVALID_ARGUMENT"
	errInternal        errorKind = "INTERNAL"
)

type domainError struct {
	kind    errorKind
	message string
}

func (e domainError) Error() string { return e.message }

func unauthenticated(message string) error { return domainError{errUnauthenticated, message} }
func forbidden(message string) error       { return domainError{errForbidden, message} }
func notFound(message string) error        { return domainError{errNotFound, message} }
func conflict(message string) error        { return domainError{errConflict, message} }
func payloadTooLarge(message string) error { return domainError{errPayloadTooLarge, message} }
func invalid(message string) error         { return domainError{errValidation, message} }
func internalFailure(message string) error { return domainError{errInternal, message} }

func httpStatusForKind(kind errorKind) int {
	switch kind {
	case errUnauthenticated:
		return http.StatusUnauthorized
	case errForbidden:
		return http.StatusForbidden
	case errNotFound:
		return http.StatusNotFound
	case errConflict:
		return http.StatusConflict
	case errPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case errValidation:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
