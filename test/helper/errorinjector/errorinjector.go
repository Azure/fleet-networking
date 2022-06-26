/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package errorinjector provides a simple helper tool that injects errors into controller-runtime client.Client calls
// for easier corner case testing.
// TO-DO (chenyu1): this package will become obsolete when (and if) the fake client provided within the
// controller-runtime package has error injection capabilities.
package errorinjector

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetAction is an action to take when processing a Get call.
type GetAction struct {
	// Do helps inject an error in a Get call; it receives the same arguments passed to the Get call.
	Do func(ctx context.Context, key client.ObjectKey, obj client.Object) error
}

// ListAction is an action to take when processing a List call.
type ListAction struct {
	// Do helps inject an error in a List call; it receives the same arguments passed to the List call.
	Do func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

// CreateAction is an action to take when processing a Create call.
type CreateAction struct {
	// Do helps inject an error in a Create call; it receives the same arguments passed to the Create call.
	Do func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
}

// DeleteAction is an action to take when processing a Delete call.
type DeleteAction struct {
	// Do helps inject an error in a Delete call; it receives the same arguments passed to the Delete call.
	Do func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
}

// UpdateAction is an action to take when processing an Update call.
type UpdateAction struct {
	// Do helps inject an error in a Update call; it receives the same arguments passed to the Update call.
	Do func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
}

// StatusUpdateAction is an action to take when processing an Update call on a Status subresource.
type StatusUpdateAction UpdateAction

// PatchAction is an action to take when processing a Patch call.
type PatchAction struct {
	// Do helps inject an error in a Patch call; ; it receives the same arguments passed to the Patch call.
	Do func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
}

// StatusPatchAction is an action to take when processing a Patch call on a Status subresource.
type StatusPatchAction PatchAction

// DeleteAllOfAction is an action to take when processing a DeleteAllOf call.
type DeleteAllOfAction struct {
	// Do helps inject an error in a DeleteAllOf call; it receives the same arguments passed to the DeleteAllOf call.
	Do func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error
}

// StatusWriterWithErrorInjection implements the client.StatusWriter interface and delegates method calls to
// the delegated client.StatusWriter with actions enabled.
type StatusWriterWithErrorInjection struct {
	DelegatedStatusWriter client.StatusWriter

	UpdateAction StatusUpdateAction
	PatchAction  StatusPatchAction
}

// Update implements client.StatusWriter.Update method.
func (s *StatusWriterWithErrorInjection) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if s.UpdateAction.Do != nil {
		err := s.UpdateAction.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return s.DelegatedStatusWriter.Update(ctx, obj, opts...)
}

// Update implements client.StatusWriter.Patch method.
func (s *StatusWriterWithErrorInjection) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if s.PatchAction.Do != nil {
		err := s.PatchAction.Do(ctx, obj, patch, opts...)
		if err != nil {
			return err
		}
	}

	return s.DelegatedStatusWriter.Patch(ctx, obj, patch, opts...)
}

// ClientWithErrorInjection implements the client.Client interface and delegates all method calls to the delegated
// client.Client with actions enabled.
type ClientWithErrorInjection struct {
	DelegatedClient       client.Client
	DelegatedStatusWriter *StatusWriterWithErrorInjection

	GetAction          GetAction
	ListAction         ListAction
	CreateAction       CreateAction
	DeleteAction       DeleteAction
	UpdateAction       UpdateAction
	StatusUpdateAction StatusUpdateAction
	PatchAction        PatchAction
	StatusPatchAction  StatusUpdateAction
	DeleteAllOfAction  DeleteAllOfAction
}

// New returns a ClientWithErrorInjection.
func New(delegatedClient client.Client) *ClientWithErrorInjection {
	delegatedStatusWriter := &StatusWriterWithErrorInjection{
		DelegatedStatusWriter: delegatedClient.Status(),
	}

	return &ClientWithErrorInjection{
		DelegatedClient:       delegatedClient,
		DelegatedStatusWriter: delegatedStatusWriter,
	}
}

// Get implements client.Client.Get method.
func (c *ClientWithErrorInjection) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	if c.GetAction.Do != nil {
		err := c.GetAction.Do(ctx, key, obj)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.Get(ctx, key, obj)
}

// List implements client.Client.List method.
func (c *ClientWithErrorInjection) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.ListAction.Do != nil {
		err := c.ListAction.Do(ctx, list, opts...)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.List(ctx, list, opts...)
}

// Create implements client.Client.Create method.
func (c *ClientWithErrorInjection) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.CreateAction.Do != nil {
		err := c.CreateAction.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.Create(ctx, obj, opts...)
}

// Delete implements client.Client.Delete method.
func (c *ClientWithErrorInjection) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if c.DeleteAction.Do != nil {
		err := c.DeleteAction.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.Delete(ctx, obj, opts...)
}

// Update implements client.Client.Update method.
func (c *ClientWithErrorInjection) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if c.UpdateAction.Do != nil {
		err := c.UpdateAction.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.Update(ctx, obj, opts...)
}

// Patch implements client.Client.Patch method.
func (c *ClientWithErrorInjection) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if c.PatchAction.Do != nil {
		err := c.PatchAction.Do(ctx, obj, patch, opts...)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.Patch(ctx, obj, patch, opts...)
}

// DeleteAllOf implements client.Client.DeleteAllOf method.
func (c *ClientWithErrorInjection) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	if c.DeleteAllOfAction.Do != nil {
		err := c.DeleteAllOfAction.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.DelegatedClient.DeleteAllOf(ctx, obj, opts...)
}

// Status implements the client.Client.Status method.
func (c *ClientWithErrorInjection) Status() client.StatusWriter {
	return c.DelegatedStatusWriter
}

// Scheme implements the client.Client.Scheme method.
func (c *ClientWithErrorInjection) Scheme() *runtime.Scheme {
	return c.DelegatedClient.Scheme()
}

// RESTMapper implements the client.Client.RESTMapper method.
func (c *ClientWithErrorInjection) RESTMapper() meta.RESTMapper {
	return c.DelegatedClient.RESTMapper()
}
