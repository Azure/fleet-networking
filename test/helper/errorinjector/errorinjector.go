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
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetAction is an action to take when processing a Get call.
type GetAction struct {
	// Do helps inject an error in a Get call.
	Do func(ctx context.Context, key client.ObjectKey, obj client.Object) error
}

// ListAction is an action to take when processing a List call.
type ListAction struct {
	// Do helps inject an error in a List call.
	Do func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

// CreateAction is an action to take when processing a Create call.
type CreateAction struct {
	// Do helps inject an error in a Create call.
	Do func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
}

// DeleteAction is an action to take when processing a Delete call.
type DeleteAction struct {
	// Do helps inject an error in a Delete call.
	Do func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
}

// UpdateAction is an action to take when processing an Update call.
type UpdateAction struct {
	// Do helps inject an error in a Update call.
	Do func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
}

// StatusUpdateAction is an action to take when processing an Update call on a Status subresource.
type StatusUpdateAction UpdateAction

// PatchAction is an action to take when processing a Patch call.
type PatchAction struct {
	// Do helps inject an error in a Patch call.
	Do func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
}

// StatusPatchAction is an action to take when processing a Patch call on a Status subresource.
type StatusPatchAction PatchAction

// DeleteAllOfAction is an action to take when processing a DeleteAllOf call.
type DeleteAllOfAction struct {
	// Do helps inject an error in a DeleteAllOf call.
	Do func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error
}

// StatusWriterWithErrorInjection implements the client.StatusWriter interface and delegates method calls to
// the delegated client.StatusWriter with actions enabled.
type StatusWriterWithErrorInjection struct {
	delegatedStatusWriter client.StatusWriter

	mu sync.Mutex

	updateActions map[string]StatusUpdateAction
	patchActions  map[string]StatusPatchAction
}

// Update implements client.StatusWriter.Update method.
func (s *StatusWriterWithErrorInjection) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	for _, action := range s.updateActions {
		err := action.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return s.delegatedStatusWriter.Update(ctx, obj, opts...)
}

// AddUpdateAction registers an UpdateAction.
func (s *StatusWriterWithErrorInjection) AddUpdateAction(name string, action StatusUpdateAction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateActions[name] = action
}

// RemoveUpdateAction unregisters an UpdateAction.
func (s *StatusWriterWithErrorInjection) RemoveUpdateAction(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.updateActions, name)
}

// Update implements client.StatusWriter.Patch method.
func (s *StatusWriterWithErrorInjection) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	for _, action := range s.patchActions {
		err := action.Do(ctx, obj, patch, opts...)
		if err != nil {
			return err
		}
	}

	return s.delegatedStatusWriter.Patch(ctx, obj, patch, opts...)
}

// AddPatchAction registers a PatchAction.
func (s *StatusWriterWithErrorInjection) AddPatchAction(name string, action StatusPatchAction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patchActions[name] = action
}

// RemovePatchAction unregisters a PatchAction.
func (s *StatusWriterWithErrorInjection) RemovePatchAction(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.patchActions, name)
}

// ClientWithErrorInjection implements the client.Client interface and delegates all method calls to the delegated
// client.Client with actions enabled.
type ClientWithErrorInjection struct {
	delegatedClient       client.Client
	delegatedStatusWriter client.StatusWriter

	mu sync.Mutex

	getActions          map[string]GetAction
	listActions         map[string]ListAction
	createActions       map[string]CreateAction
	deleteActions       map[string]DeleteAction
	updateActions       map[string]UpdateAction
	statusUpdateActions map[string]StatusUpdateAction
	patchActions        map[string]PatchAction
	statusPatchActions  map[string]StatusPatchAction
	deleteAllOfActions  map[string]DeleteAllOfAction
}

// New returns a ClientWithErrorInjection.
func New(delegatedClient client.Client) *ClientWithErrorInjection {
	delegatedStatusWriter := &StatusWriterWithErrorInjection{
		delegatedStatusWriter: delegatedClient.Status(),
		updateActions:         map[string]StatusUpdateAction{},
		patchActions:          map[string]StatusPatchAction{},
	}

	return &ClientWithErrorInjection{
		delegatedClient:       delegatedClient,
		delegatedStatusWriter: delegatedStatusWriter,
		getActions:            map[string]GetAction{},
		listActions:           map[string]ListAction{},
		createActions:         map[string]CreateAction{},
		deleteActions:         map[string]DeleteAction{},
		updateActions:         map[string]UpdateAction{},
		statusUpdateActions:   map[string]StatusUpdateAction{},
		patchActions:          map[string]PatchAction{},
		statusPatchActions:    map[string]StatusPatchAction{},
		deleteAllOfActions:    map[string]DeleteAllOfAction{},
	}
}

// Get implements client.Client.Get method.
func (c *ClientWithErrorInjection) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	for _, action := range c.getActions {
		err := action.Do(ctx, key, obj)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.Get(ctx, key, obj)
}

// AddGetAction registers a GetAction.
func (c *ClientWithErrorInjection) AddGetAction(name string, action GetAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getActions[name] = action
}

// RemoveGetAction unregisters a GetAction.
func (c *ClientWithErrorInjection) RemoveGetAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.getActions, name)
}

// List implements client.Client.List method.
func (c *ClientWithErrorInjection) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	for _, action := range c.listActions {
		err := action.Do(ctx, list, opts...)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.List(ctx, list, opts...)
}

// AddListAction registers a ListAction.
func (c *ClientWithErrorInjection) AddListAction(name string, action ListAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listActions[name] = action
}

// RemoveListAction unregisters a ListAction.
func (c *ClientWithErrorInjection) RemoveListAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.listActions, name)
}

// Create implements client.Client.Create method.
func (c *ClientWithErrorInjection) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	for _, action := range c.createActions {
		err := action.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.Create(ctx, obj, opts...)
}

// AddCreateAction registers a CreateAction.
func (c *ClientWithErrorInjection) AddCreateAction(name string, action CreateAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.createActions[name] = action
}

// RemoveListAction unregisters a CreateAction.
func (c *ClientWithErrorInjection) RemoveCreateAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.createActions, name)
}

// Delete implements client.Client.Delete method.
func (c *ClientWithErrorInjection) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	for _, action := range c.deleteActions {
		err := action.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.Delete(ctx, obj, opts...)
}

// AddDeleteAction registers a DeleteAction.
func (c *ClientWithErrorInjection) AddDeleteAction(name string, action DeleteAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteActions[name] = action
}

// RemoveDeleteAction unregisters a DeleteAction.
func (c *ClientWithErrorInjection) RemoveDeleteAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.deleteActions, name)
}

// Update implements client.Client.Update method.
func (c *ClientWithErrorInjection) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	for _, action := range c.updateActions {
		err := action.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.Update(ctx, obj, opts...)
}

// AddUpdateAction registers an UpdateAction.
func (c *ClientWithErrorInjection) AddUpdateAction(name string, action UpdateAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateActions[name] = action
}

// RemoveUpdateAction unregisters an UpdateAction.
func (c *ClientWithErrorInjection) RemoveUpdateAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.updateActions, name)
}

// Patch implements client.Client.Patch method.
func (c *ClientWithErrorInjection) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	for _, action := range c.patchActions {
		err := action.Do(ctx, obj, patch, opts...)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.Patch(ctx, obj, patch, opts...)
}

// AddPatchAction registers a PatchAction.
func (c *ClientWithErrorInjection) AddPatchAction(name string, action PatchAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.patchActions[name] = action
}

// RemovePatchAction unregisters a PatchAction.
func (c *ClientWithErrorInjection) RemovePatchAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.patchActions, name)
}

// DeleteAllOf implements client.Client.DeleteAllOf method.
func (c *ClientWithErrorInjection) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	for _, action := range c.deleteAllOfActions {
		err := action.Do(ctx, obj, opts...)
		if err != nil {
			return err
		}
	}

	return c.delegatedClient.DeleteAllOf(ctx, obj, opts...)
}

// AddDeleteAllOfAction registers a DeleteAllOfAction.
func (c *ClientWithErrorInjection) AddDeleteAllOfAction(name string, action DeleteAllOfAction) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteAllOfActions[name] = action
}

// RemovDeleteAllOfAction unregisters a DeleteAllOfAction.
func (c *ClientWithErrorInjection) RemoveDeleteAllOfAction(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.deleteAllOfActions, name)
}

// AddStatusUpdateAction registers a StatusUpdateAction.
func (c *ClientWithErrorInjection) AddStatusUpdateAction(name string, action StatusUpdateAction) {
	s, _ := c.delegatedStatusWriter.(*StatusWriterWithErrorInjection)
	s.AddUpdateAction(name, action)
}

// RemoveStatusUpdateAction unregisters a StatusUpdateAction.
func (c *ClientWithErrorInjection) RemoveStatusUpdateAction(name string) {
	s, _ := c.delegatedStatusWriter.(*StatusWriterWithErrorInjection)
	s.RemoveUpdateAction(name)
}

// AddStatusPatchAction registers a StatusPatchAction.
func (c *ClientWithErrorInjection) AddStatusPatchAction(name string, action StatusPatchAction) {
	s, _ := c.delegatedStatusWriter.(*StatusWriterWithErrorInjection)
	s.AddPatchAction(name, action)
}

// RemoveStatusPatchAction unregisters a StatusPatchAction.
func (c *ClientWithErrorInjection) RemoveStatusPatchAction(name string) {
	s, _ := c.delegatedStatusWriter.(*StatusWriterWithErrorInjection)
	s.RemovePatchAction(name)
}

// Status implements the client.Client.Status method.
func (c *ClientWithErrorInjection) Status() client.StatusWriter {
	return c.delegatedStatusWriter
}

// Scheme implements the client.Client.Scheme method.
func (c *ClientWithErrorInjection) Scheme() *runtime.Scheme {
	return c.delegatedClient.Scheme()
}

// RESTMapper implements the client.Client.RESTMapper method.
func (c *ClientWithErrorInjection) RESTMapper() meta.RESTMapper {
	return c.delegatedClient.RESTMapper()
}
