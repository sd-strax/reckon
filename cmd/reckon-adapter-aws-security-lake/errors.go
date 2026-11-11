package main

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"

	"github.com/sd-strax/reckon/capability"
)

// classedError is a failure the adapter has already placed in the 03 §6.2
// taxonomy: a refusal of its own (no window, no resource) or an Athena query
// outcome. classify reads it back unchanged.
type classedError struct {
	class capability.ErrorClass
	msg   string
	err   error
}

func (e *classedError) Error() string {
	if e.err != nil {
		return e.msg + ": " + e.err.Error()
	}
	return e.msg
}

func (e *classedError) Unwrap() error { return e.err }

// fatal is a refusal the caller cannot cure by retrying or by another binding
// answering the same call shape: a malformed call or a query Athena rejects.
func fatal(msg string) error { return &classedError{class: capability.ErrFatal, msg: msg} }

// withClass tags err with class, keeping it in the chain for its AWS code.
func withClass(class capability.ErrorClass, msg string, err error) error {
	return &classedError{class: class, msg: msg, err: err}
}

// retryCodes are AWS error codes for a transient condition: the same call
// can succeed when repeated.
var retryCodes = map[string]bool{
	"ThrottlingException":             true,
	"Throttling":                      true,
	"TooManyRequestsException":        true,
	"RequestLimitExceeded":            true,
	"ProvisionedThroughputExceeded":   true,
	"InternalServerException":         true,
	"InternalFailure":                 true,
	"InternalServiceException":        true,
	"ServiceUnavailable":              true,
	"ServiceUnavailableException":     true,
	"RequestTimeout":                  true,
	"RequestTimeoutException":         true,
	"ConcurrentModificationException": true,
}

// unhealthyCodes are AWS error codes that make the instance unusable until an
// operator acts: denied access, bad or expired credentials, and a database or
// table that is not there.
var unhealthyCodes = map[string]bool{
	"AccessDenied":                true,
	"AccessDeniedException":       true,
	"UnrecognizedClientException": true,
	"InvalidClientTokenId":        true,
	"InvalidSignatureException":   true,
	"SignatureDoesNotMatch":       true,
	"IncompleteSignature":         true,
	"MissingAuthenticationToken":  true,
	"AuthFailure":                 true,
	"ExpiredToken":                true,
	"ExpiredTokenException":       true,
	"RegionDisabledException":     true,
	"EntityNotFoundException":     true, // Glue: database or table missing
	"MetadataException":           true, // Athena: catalog object missing
	"ResourceNotFoundException":   true, // Athena: workgroup or catalog missing
}

// classify maps an error onto the 03 §6.2 taxonomy. Anything unrecognised is
// RETRY: an unexpected failure from a managed service is more often
// transient than a contract break, the resolver bounds retries by its own
// budget, and FATAL would end the verb for a condition nobody diagnosed.
func classify(err error) capability.ErrorClass {
	if err == nil {
		return ""
	}
	var ce *classedError
	if errors.As(err, &ce) {
		return ce.class
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return capability.ErrRetry
	}
	var canceled *aws.RequestCanceledError
	if errors.As(err, &canceled) {
		return capability.ErrRetry
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		switch {
		case retryCodes[code]:
			return capability.ErrRetry
		case unhealthyCodes[code], strings.HasPrefix(code, "ExpiredToken"):
			return capability.ErrUnhealthy
		case code == "InvalidRequestException":
			return invalidRequestClass(apiErr.ErrorMessage())
		}
	}
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		switch s := status.HTTPStatusCode(); {
		case s == 429 || s >= 500:
			return capability.ErrRetry
		case s == 401 || s == 403:
			return capability.ErrUnhealthy
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return capability.ErrRetry
	}
	return capability.ErrRetry
}

// invalidRequestClass splits Athena's InvalidRequestException: one naming the
// workgroup, the output location or a missing catalog object is the
// instance's configuration (UNHEALTHY); any other is a request the adapter
// built wrong (FATAL).
func invalidRequestClass(msg string) capability.ErrorClass {
	m := strings.ToLower(msg)
	for _, s := range []string{"workgroup", "output location", "outputlocation", "not found", "does not exist"} {
		if strings.Contains(m, s) {
			return capability.ErrUnhealthy
		}
	}
	return capability.ErrFatal
}

// errorCode is the AWS error code in err's chain, "" when there is none.
func errorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return ""
}

// wireErr is err on the wire with its class (03 §6.2).
func wireErr(err error) *wireError {
	return classErr(-32000, err.Error(), string(classify(err)))
}
