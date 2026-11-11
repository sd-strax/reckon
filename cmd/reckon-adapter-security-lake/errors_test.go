package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/sd-strax/reckon/capability"
)

func apiErr(code string) error {
	return &smithy.OperationError{ServiceID: "Athena", OperationName: "StartQueryExecution",
		Err: &smithy.GenericAPIError{Code: code, Message: code + " happened"}}
}

func statusErr(status int) error {
	return &smithy.OperationError{ServiceID: "Athena", OperationName: "GetQueryResults",
		Err: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}}, Err: errors.New("boom")}}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		want capability.ErrorClass
	}{
		{apiErr("ThrottlingException"), capability.ErrRetry},
		{apiErr("TooManyRequestsException"), capability.ErrRetry},
		{apiErr("RequestLimitExceeded"), capability.ErrRetry},
		{apiErr("InternalServerException"), capability.ErrRetry},
		{apiErr("AccessDeniedException"), capability.ErrUnhealthy},
		{apiErr("AccessDenied"), capability.ErrUnhealthy},
		{apiErr("UnrecognizedClientException"), capability.ErrUnhealthy},
		{apiErr("InvalidClientTokenId"), capability.ErrUnhealthy},
		{apiErr("ExpiredToken"), capability.ErrUnhealthy},
		{apiErr("ExpiredTokenException"), capability.ErrUnhealthy},
		{apiErr("InvalidSignatureException"), capability.ErrUnhealthy},
		{apiErr("EntityNotFoundException"), capability.ErrUnhealthy},
		{apiErr("MetadataException"), capability.ErrUnhealthy},
		{&smithy.GenericAPIError{Code: "InvalidRequestException", Message: "WorkGroup reckon is not found"}, capability.ErrUnhealthy},
		{&smithy.GenericAPIError{Code: "InvalidRequestException", Message: "Query has 3 parameters but 2 values"}, capability.ErrFatal},
		{statusErr(503), capability.ErrRetry},
		{statusErr(429), capability.ErrRetry},
		{statusErr(403), capability.ErrUnhealthy},
		{context.DeadlineExceeded, capability.ErrRetry},
		{fmt.Errorf("wrapped: %w", context.Canceled), capability.ErrRetry},
		{&aws.RequestCanceledError{Err: errors.New("x")}, capability.ErrRetry},
		{fatal("no window"), capability.ErrFatal},
		{withClass(capability.ErrFallthrough, "x", apiErr("AccessDeniedException")), capability.ErrFallthrough},
		{apiErr("SomethingNew"), capability.ErrRetry},
		{errors.New("unknown"), capability.ErrRetry},
	}
	for _, tc := range cases {
		if got := classify(tc.err); got != tc.want {
			t.Errorf("classify(%v) = %s, want %s", tc.err, got, tc.want)
		}
	}
}

func TestErrorCode(t *testing.T) {
	if got := errorCode(apiErr("AccessDeniedException")); got != "AccessDeniedException" {
		t.Errorf("errorCode = %q", got)
	}
	if got := errorCode(errors.New("x")); got != "" {
		t.Errorf("errorCode = %q", got)
	}
}
