package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
)

// contentTypeJSON is the Content-Type of every response.
const contentTypeJSON = "application/json; charset=utf-8"

// maxBodyBytes limits the size of a request body.
const maxBodyBytes = 1 << 20

// errorResponse is the body of every error response.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON writes v as the JSON body of a response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Nothing is written yet, so the client still gets a proper error.
		log.Printf("encode response: %v", err)
		status = http.StatusInternalServerError
		body, _ = json.Marshal(errorResponse{Error: "internal server error"})
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// writeError writes {"error": msg} with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// writeInternalError logs err and answers 500 without its details, which
// are of no use to the client and may reveal internals.
func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// Errors of request bodies that encoding/json accepts.
var (
	errNotObject    = errors.New("not a JSON object")
	errTrailingData = errors.New("data after the JSON object")
)

// decodeJSON reads the request body, a single JSON object, into a value of
// type T. Unknown fields are an error, so a typo in a field name is not
// silently ignored. On failure decodeJSON writes an error response and
// returns false.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	// Decoding into a pointer tells null, which leaves it nil, from an object.
	var v *T
	err := dec.Decode(&v)
	if err == nil && v == nil {
		err = errNotObject
	}
	if err == nil {
		err = expectEOF(dec)
	}
	if err != nil {
		status, msg := decodeErrorResponse(err)
		writeError(w, status, msg)
		var zero T
		return zero, false
	}
	return *v, true
}

// expectEOF returns nil if dec has no more data, apart from spaces.
func expectEOF(dec *json.Decoder) error {
	_, err := dec.Token()
	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil:
		return errTrailingData
	default:
		return err
	}
}

// decodeErrorResponse returns the status and the message of the response
// to a request body that decodeJSON could not decode. The messages name the
// JSON field and the expected JSON type, not the Go types.
func decodeErrorResponse(err error) (int, string) {
	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, fmt.Sprintf("request body is too large: the limit is %d bytes", tooLarge.Limit)
	case errors.Is(err, io.EOF):
		return http.StatusBadRequest, "request body is empty"
	case errors.Is(err, errTrailingData):
		return http.StatusBadRequest, "request body must contain a single JSON object"
	case errors.Is(err, errNotObject), errors.As(err, &typeErr) && typeErr.Field == "":
		return http.StatusBadRequest, "request body must be a JSON object"
	case errors.As(err, &typeErr) && strings.HasPrefix(typeErr.Value, "number "):
		// A number that does not fit into float64, such as 1e400.
		return http.StatusBadRequest, fmt.Sprintf("field %q is out of range", typeErr.Field)
	case errors.As(err, &typeErr):
		// The request fields are numbers and strings only.
		want := "a number"
		if typeErr.Type.Kind() == reflect.String {
			want = "a string"
		}
		return http.StatusBadRequest, fmt.Sprintf("field %q must be %s", typeErr.Field, want)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		// encoding/json has no error type for unknown fields.
		return http.StatusBadRequest, strings.TrimPrefix(err.Error(), "json: ")
	default:
		// Syntax errors, a body cut short and failed reads.
		return http.StatusBadRequest, "invalid JSON"
	}
}
