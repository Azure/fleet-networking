/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementations for testing
type MockProfilesClient struct {
	mock.Mock
}

func (m *MockProfilesClient) Get(ctx context.Context, resourceGroupName string, profileName string, options *armtrafficmanager.ProfilesClientGetOptions) (armtrafficmanager.ProfilesClientGetResponse, error) {
	args := m.Called(ctx, resourceGroupName, profileName, options)
	return args.Get(0).(armtrafficmanager.ProfilesClientGetResponse), args.Error(1)
}

func (m *MockProfilesClient) CreateOrUpdate(ctx context.Context, resourceGroupName string, profileName string, parameters armtrafficmanager.Profile, options *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (armtrafficmanager.ProfilesClientCreateOrUpdateResponse, error) {
	args := m.Called(ctx, resourceGroupName, profileName, parameters, options)
	return args.Get(0).(armtrafficmanager.ProfilesClientCreateOrUpdateResponse), args.Error(1)
}

func (m *MockProfilesClient) Delete(ctx context.Context, resourceGroupName string, profileName string, options *armtrafficmanager.ProfilesClientDeleteOptions) (armtrafficmanager.ProfilesClientDeleteResponse, error) {
	args := m.Called(ctx, resourceGroupName, profileName, options)
	return args.Get(0).(armtrafficmanager.ProfilesClientDeleteResponse), args.Error(1)
}

type MockEndpointsClient struct {
	mock.Mock
}

func (m *MockEndpointsClient) CreateOrUpdate(ctx context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, parameters armtrafficmanager.Endpoint, options *armtrafficmanager.EndpointsClientCreateOrUpdateOptions) (armtrafficmanager.EndpointsClientCreateOrUpdateResponse, error) {
	args := m.Called(ctx, resourceGroupName, profileName, endpointType, endpointName, parameters, options)
	return args.Get(0).(armtrafficmanager.EndpointsClientCreateOrUpdateResponse), args.Error(1)
}

func (m *MockEndpointsClient) Delete(ctx context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, options *armtrafficmanager.EndpointsClientDeleteOptions) (armtrafficmanager.EndpointsClientDeleteResponse, error) {
	args := m.Called(ctx, resourceGroupName, profileName, endpointType, endpointName, options)
	return args.Get(0).(armtrafficmanager.EndpointsClientDeleteResponse), args.Error(1)
}

func TestProfilesClient_Get(t *testing.T) {
	// Skip metrics assertions for now as they require more complex setup in tests
	
	// Create mock client
	mockClient := new(MockProfilesClient)

	// Create test response
	testResponse := armtrafficmanager.ProfilesClientGetResponse{
		Profile: armtrafficmanager.Profile{},
	}

	// Test successful call
	mockClient.On("Get", mock.Anything, "test-rg", "test-profile", (*armtrafficmanager.ProfilesClientGetOptions)(nil)).
		Return(testResponse, nil).Once()

	client := NewProfilesClient(mockClient)
	resp, err := client.Get(context.Background(), "test-rg", "test-profile", nil)

	assert.NoError(t, err)
	assert.Equal(t, testResponse, resp)

	// Test error case
	testError := errors.New("test error")
	mockClient.On("Get", mock.Anything, "test-rg", "error-profile", (*armtrafficmanager.ProfilesClientGetOptions)(nil)).
		Return(armtrafficmanager.ProfilesClientGetResponse{}, testError).Once()

	resp, err = client.Get(context.Background(), "test-rg", "error-profile", nil)

	assert.Error(t, err)
	assert.Equal(t, testError, err)

	mockClient.AssertExpectations(t)
}

func TestProfilesClient_CreateOrUpdate(t *testing.T) {
	// Skip metrics assertions for now as they require more complex setup in tests
	
	// Create mock client
	mockClient := new(MockProfilesClient)

	// Create test response
	testResponse := armtrafficmanager.ProfilesClientCreateOrUpdateResponse{
		Profile: armtrafficmanager.Profile{},
	}

	// Test successful call
	testProfile := armtrafficmanager.Profile{}
	mockClient.On("CreateOrUpdate", mock.Anything, "test-rg", "test-profile", testProfile, (*armtrafficmanager.ProfilesClientCreateOrUpdateOptions)(nil)).
		Return(testResponse, nil).Once()

	client := NewProfilesClient(mockClient)
	resp, err := client.CreateOrUpdate(context.Background(), "test-rg", "test-profile", testProfile, nil)

	assert.NoError(t, err)
	assert.Equal(t, testResponse, resp)

	mockClient.AssertExpectations(t)
}

func TestEndpointsClient_CreateOrUpdate(t *testing.T) {
	// Skip metrics assertions for now as they require more complex setup in tests
	
	// Create mock client
	mockClient := new(MockEndpointsClient)

	// Create test response
	testResponse := armtrafficmanager.EndpointsClientCreateOrUpdateResponse{
		Endpoint: armtrafficmanager.Endpoint{},
	}

	// Test successful call
	testEndpoint := armtrafficmanager.Endpoint{}
	mockClient.On("CreateOrUpdate", mock.Anything, "test-rg", "test-profile", armtrafficmanager.EndpointTypeAzureEndpoints, "test-endpoint", testEndpoint, (*armtrafficmanager.EndpointsClientCreateOrUpdateOptions)(nil)).
		Return(testResponse, nil).Once()

	client := NewEndpointsClient(mockClient)
	resp, err := client.CreateOrUpdate(context.Background(), "test-rg", "test-profile", armtrafficmanager.EndpointTypeAzureEndpoints, "test-endpoint", testEndpoint, nil)

	assert.NoError(t, err)
	assert.Equal(t, testResponse, resp)

	mockClient.AssertExpectations(t)
}

func TestEndpointsClient_Delete(t *testing.T) {
	// Skip metrics assertions for now as they require more complex setup in tests
	
	// Create mock client
	mockClient := new(MockEndpointsClient)

	// Create test response
	testResponse := armtrafficmanager.EndpointsClientDeleteResponse{}

	// Test successful call
	mockClient.On("Delete", mock.Anything, "test-rg", "test-profile", armtrafficmanager.EndpointTypeAzureEndpoints, "test-endpoint", (*armtrafficmanager.EndpointsClientDeleteOptions)(nil)).
		Return(testResponse, nil).Once()

	client := NewEndpointsClient(mockClient)
	resp, err := client.Delete(context.Background(), "test-rg", "test-profile", armtrafficmanager.EndpointTypeAzureEndpoints, "test-endpoint", nil)

	assert.NoError(t, err)
	assert.Equal(t, testResponse, resp)

	mockClient.AssertExpectations(t)
}