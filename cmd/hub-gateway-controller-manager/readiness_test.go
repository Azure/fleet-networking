/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestGatewayCacheReadiness(t *testing.T) {
	type testCase struct {
		name      string
		unsynced  string
		stopped   string
		cancel    bool
		wantError string
	}
	objects := requiredReadinessObjects()
	tests := make([]testCase, 0, 2+2*len(objects))
	tests = append(tests,
		testCase{name: "all required caches synced"},
		testCase{name: "shutdown", cancel: true, wantError: "canceled"})
	for _, object := range objects {
		kind := reflect.TypeOf(object).Elem().Name()
		tests = append(tests,
			testCase{name: "unsynced " + kind, unsynced: kind, wantError: kind},
			testCase{name: "stopped " + kind, stopped: kind, wantError: kind})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mgr := newReadinessTestManager(t)
			for kind, informer := range mgr.informers.informers {
				informer.synced = kind.Elem().Name() != tt.unsynced
				informer.stopped = kind.Elem().Name() == tt.stopped
			}
			if err := registerGatewayCacheReadiness(ctx, mgr); err != nil {
				t.Fatal(err)
			}
			if mgr.check == nil {
				t.Fatal("enabled setup did not register a cache-readiness check")
			}
			if len(mgr.informers.requests) != len(requiredReadinessObjects()) {
				t.Fatalf("informer requests = %v, want each of the seven required kinds", mgr.informers.requests)
			}
			for _, object := range requiredReadinessObjects() {
				if mgr.informers.requests[reflect.TypeOf(object)] != 1 {
					t.Errorf("informer requests for %T = %d, want 1", object, mgr.informers.requests[reflect.TypeOf(object)])
				}
			}
			if tt.cancel {
				cancel()
			}
			select {
			case <-mgr.Elected():
				t.Fatal("test manager unexpectedly became leader")
			default:
			}
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			err := mgr.check(request)
			wantStatus := http.StatusOK
			if tt.wantError != "" {
				wantStatus = http.StatusInternalServerError
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("readiness error = %v, want containing %q", err, tt.wantError)
				}
			} else if err != nil {
				t.Fatalf("readiness before leader election = %v", err)
			}
			handler := &healthz.Handler{Checks: map[string]healthz.Checker{mgr.checkName: mgr.check}}
			response := httptest.NewRecorder()
			http.StripPrefix("/readyz", handler).ServeHTTP(response, request)
			if response.Code != wantStatus {
				t.Errorf("readiness HTTP status = %d, want %d", response.Code, wantStatus)
			}
			for kind, count := range mgr.informers.requests {
				if count != 1 {
					t.Errorf("probe re-requested %v informer: %d calls", kind, count)
				}
			}
		})
	}
}

func TestGatewayCacheReadinessTracksSynchronization(t *testing.T) {
	mgr := newReadinessTestManager(t)
	if err := registerGatewayCacheReadiness(context.Background(), mgr); err != nil {
		t.Fatal(err)
	}
	if mgr.check == nil {
		t.Fatal("enabled setup did not register a cache-readiness check")
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	if err := mgr.check(request); err == nil {
		t.Fatal("empty, unsynced informers reported ready")
	}
	for i, object := range requiredReadinessObjects() {
		mgr.informers.informers[reflect.TypeOf(object)].synced = true
		err := mgr.check(request)
		if i < len(requiredReadinessObjects())-1 && err == nil {
			t.Fatal("partially synced informers reported ready")
		}
		if i == len(requiredReadinessObjects())-1 && err != nil {
			t.Fatalf("all informers synced but readiness failed: %v", err)
		}
	}
	mgr.informers.informers[reflect.TypeOf(&gatewayv1.Gateway{})].stopped = true
	if err := mgr.check(request); err == nil {
		t.Fatal("stopped informer retained readiness after initial synchronization")
	}
}

func TestGatewayCacheReadinessFailures(t *testing.T) {
	for _, object := range requiredReadinessObjects() {
		t.Run(reflect.TypeOf(object).Elem().Name(), func(t *testing.T) {
			mgr := newReadinessTestManager(t)
			wantError := errors.New("informer initialization failed")
			mgr.informers.failKind = reflect.TypeOf(object)
			mgr.informers.initError = wantError
			err := registerGatewayCacheReadiness(context.Background(), mgr)
			if !errors.Is(err, wantError) || !strings.Contains(err.Error(), reflect.TypeOf(object).Elem().Name()) {
				t.Fatalf("setup error = %v, want contextual informer initialization error", err)
			}
			if mgr.check != nil {
				t.Fatal("initialization failure registered readiness")
			}
		})
	}
	t.Run("check registration", func(t *testing.T) {
		mgr := newReadinessTestManager(t)
		wantError := errors.New("readiness registration failed")
		mgr.checkError = wantError
		err := registerGatewayCacheReadiness(context.Background(), mgr)
		if !errors.Is(err, wantError) {
			t.Fatalf("setup error = %v, want readiness registration error", err)
		}
	})
	t.Run("initialization cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		mgr := newReadinessTestManager(t)
		err := registerGatewayCacheReadiness(ctx, mgr)
		if !errors.Is(err, context.Canceled) || mgr.check != nil {
			t.Fatalf("cancelled initialization error = %v, want cancellation without readiness registration", err)
		}
	})
}

func requiredReadinessObjects() []client.Object {
	return []client.Object{
		&gatewayv1.GatewayClass{},
		&gatewayv1.Gateway{},
		&gatewayv1.HTTPRoute{},
		&gatewayv1beta1.ReferenceGrant{},
		&fleetnetv1alpha1.ServiceImport{},
		&fleetnetv1alpha1.InternalServiceExport{},
		&corev1.Namespace{},
	}
}

func newReadinessTestManager(t *testing.T) *readinessTestManager {
	t.Helper()
	return &readinessTestManager{Manager: newTestGatewayManager(t), informers: newReadinessTestCache()}
}

func newReadinessTestCache() *readinessTestCache {
	informers := &readinessTestCache{
		informers: make(map[reflect.Type]*readinessTestInformer),
		requests:  make(map[reflect.Type]int),
	}
	for _, object := range requiredReadinessObjects() {
		informers.informers[reflect.TypeOf(object)] = &readinessTestInformer{}
	}
	return informers
}

type readinessTestManager struct {
	ctrl.Manager
	informers    *readinessTestCache
	check        healthz.Checker
	checkName    string
	checkError   error
	recorderName string
}

func (m *readinessTestManager) GetEventRecorderFor(name string) record.EventRecorder {
	m.recorderName = name
	return record.NewFakeRecorder(10)
}

func (m *readinessTestManager) GetCache() cache.Cache {
	return m.informers
}

func (m *readinessTestManager) AddReadyzCheck(name string, check healthz.Checker) error {
	if m.checkError != nil {
		return m.checkError
	}
	m.checkName = name
	m.check = check
	return nil
}

type readinessTestCache struct {
	cache.Cache
	informers map[reflect.Type]*readinessTestInformer
	requests  map[reflect.Type]int
	failKind  reflect.Type
	initError error
}

func (c *readinessTestCache) GetInformer(ctx context.Context, object client.Object, options ...cache.InformerGetOption) (cache.Informer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	getOptions := &cache.InformerGetOptions{}
	for _, option := range options {
		option(getOptions)
	}
	if getOptions.BlockUntilSynced == nil || *getOptions.BlockUntilSynced {
		return nil, errors.New("readiness informer initialization must not wait for cache sync")
	}
	kind := reflect.TypeOf(object)
	c.requests[kind]++
	if kind == c.failKind {
		return nil, c.initError
	}
	informer, ok := c.informers[kind]
	if !ok {
		return nil, fmt.Errorf("unexpected readiness informer %T", object)
	}
	return informer, nil
}

type readinessTestInformer struct {
	cache.Informer
	synced  bool
	stopped bool
}

func (i *readinessTestInformer) HasSynced() bool {
	return i.synced
}

func (i *readinessTestInformer) IsStopped() bool {
	return i.stopped
}
