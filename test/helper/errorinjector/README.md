# `errorinjector`

`errorinjector` is a simple helper tool that helps inject arbitrary errors into `client.Client` calls; `client.Client`
is the interface that `controller-runtime` provides for interacting with Kubernetes API server. The tool can make
it easier to test corner cases in the control loops.

## Usage

`client.Client` provides the following methods for operating on the Kubernetes API server:

* `Get`
* `List`
* `Create`
* `Update`
* `Patch`
* `Delete`
* `DeleteAllOf`
* `Update` (Status subresource, `Status().Update()`)
* `Patch` (Status subresource, `Status().Patch()`)

To inject an error in any of the ops above, first wrap a regular `client.Client` into a client with error injection
capabilities with the `New` method in this package:

```go
fakeClient := fake.NewClientBuilder().Build()
clientWithErrorInjection := New(fakeClient)
```

The returned client also implements the `client.Client` interface, which one can use in the same way as a regular
client. It will, by default, delegate all calls to the wrapped client.

Next, register an action with the returned client. An action features a `Do` function, which the client calls
before runnning a specific method; if the function returns an error, it will be returned by the method. The `Do`
function sees the same parameters passed to the method, which one can use to inject errors only into ops with
specific objects. For example, to inject an API server `InternalError` every time one tries to get a `Service`
with the name `app` in the namespace `default`, one can register the action below:

```go
getAction := GetAction{func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
    if key.Namespace == "default" && key.Name == "app" {
        return errors.NewInternalError(fmt.Errorf("injected error"))
    }
    return nil
}}
clientWithErrorInjection.AddGetAction("name of the action", getAction)

svc := corev1.Service{}
err := c.Get(ctx, types.NamespacedName{Namespace: defaultNS, Name: "app"}, &svc)
// err will be an InternalError (errors.IsInternalError(err) == true)
```

Note that getting other Services will not trigger the action. One can unregister an action at any time using its
name; it is also possible to register multiple actions for the same method, which errorinjector will call one
by one in some order.

Available actions and their register/unregister methods are:

* `GetAction` -> `AddGetAction`, `RemoveGetAction`
* `ListAction` -> `AddListAction`, `RemoveListAction`
* `CreateAction` -> `AddCreateAction`, `RemoveCreateAction`
* `UpdateAction` -> `AddUpdateAction`, `RemoveUpdateAction`
* `DeleteAction` -> `AddDeleteAction`, `RemoveDeleteAction`
* `PatchAction` -> `AddPatchAction`, `RemovePatchAction`
* `DeleteAllOfAction` -> `AddDeleteAllOfAction`, `RemoveDeleteAllOfAction`
* `StatusUpdateAction` -> `AddStatusUpdateAction`, `RemoveStatusUpdateAction`
* `StatusPatchAction` -> `AddStatusPatchAction`, `RemoveStatusPatchAction`
