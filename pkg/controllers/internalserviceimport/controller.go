// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package internalserviceimport

type Controller struct {
}

func New() (*Controller, error) {
	return &Controller{}, nil
}
