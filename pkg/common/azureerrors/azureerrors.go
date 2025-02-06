/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azureerrors defines shared azure error util functions.
package azureerrors

import (
	"errors"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// IsNotFound returns true if the error is a http 404 error returned by the azure server.
func IsNotFound(err error) bool {
	var responseError *azcore.ResponseError
	return errors.As(err, &responseError) && responseError.StatusCode == http.StatusNotFound
}

// IsClientError returns true if the error is a client error (400-499) returned by the azure server.
func IsClientError(err error) bool {
	var responseError *azcore.ResponseError
	return errors.As(err, &responseError) &&
		responseError.StatusCode >= http.StatusBadRequest && responseError.StatusCode < http.StatusInternalServerError
}

// IsConflict determines if the error is a http 409 error returned by the azure server.
func IsConflict(err error) bool {
	var responseError *azcore.ResponseError
	return errors.As(err, &responseError) && responseError.StatusCode == http.StatusConflict
}

// IsThrottled determines if the error is a http 429 error returned by the azure server.
func IsThrottled(err error) bool {
	var responseError *azcore.ResponseError
	return errors.As(err, &responseError) && responseError.StatusCode == http.StatusTooManyRequests
}

// IsForbidden determines if the error is a http 403 error returned by the azure server.
func IsForbidden(err error) bool {
	var responseError *azcore.ResponseError
	return errors.As(err, &responseError) && responseError.StatusCode == http.StatusForbidden
}
