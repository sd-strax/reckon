package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxBodyBytes bounds every JSON request body except the interpretation
// route's (maxInterpretationBodyBytes). Commands, verb inputs and SOP documents
// are far below it; the bound exists so a buggy or hostile client cannot stream
// an unbounded body.
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeBody decodes the request's JSON body into dst, the one decode path for
// every route (adr/0219). The body is bounded by limit, must be exactly one
// JSON value, and may carry no field dst does not declare, so a misspelled key
// is an error rather than a silently zero field. On failure it writes the
// response (413 over the limit, 400 otherwise) and returns false.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	return decode(w, r, dst, limit, false)
}

// decodeOptionalBody is decodeBody for a route whose body may be absent: an
// empty body leaves dst zero and returns true. A body that is present is held
// to every rule decodeBody applies.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	return decode(w, r, dst, limit, true)
}

func decode(w http.ResponseWriter, r *http.Request, dst any, limit int64, optional bool) bool {
	if r.Body == nil {
		if optional {
			return true
		}
		writeJSONError(w, http.StatusBadRequest, "request body is required")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if errors.Is(err, io.EOF) {
		if optional {
			return true
		}
		writeJSONError(w, http.StatusBadRequest, "request body is required")
		return false
	}
	if err == nil {
		// A second value after the first is trailing data, not a body.
		if derr := dec.Decode(&json.RawMessage{}); !errors.Is(derr, io.EOF) {
			err = derr
			if err == nil {
				err = errors.New("unexpected data after the JSON value")
			}
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds %d bytes", tooLarge.Limit))
			return false
		}
		writeJSONError(w, http.StatusBadRequest, "bad request body: "+err.Error())
		return false
	}
	return true
}
