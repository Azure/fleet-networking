# First HTTP conformance milestone

The opt-in runner executes the unchanged Gateway API v1.2.1
[HTTPRouteSimpleSameNamespace](https://github.com/kubernetes-sigs/gateway-api/blob/v1.2.1/conformance/tests/httproute-simple-same-namespace.go).
It checks acceptance, resolved references, and HTTP 200 from the expected
`infra-backend-v1` Pods in `gateway-conformance-infra`.

## Setup boundary

`baseFixtureKeys` selects prerequisites from upstream's embedded base YAML. The
current baseline selects four:
the infrastructure Namespace, `same-namespace` HTTP/80 Gateway, and the original
backend Service and Deployment. No fixture is changed into a LoadBalancer and
no member workload replaces the test backend. Shared HTTPS Gateways and self-signed
Secrets are intentionally excluded. This is a single-test milestone, not a full
HTTP profile report or conformance certification.

Each selected test's unchanged manifests are staged before execution. Shared
helpers discover its forwarding HTTPRoute references across the selected fixture
namespaces, including multiple Gateways, Services, and ports. The runner
independently verifies each Gateway-owned AFD profile and companion origin,
approves only exact correlated requests, and pins their IDs. All origins are
approved before warmup so an unavailable backend does not starve other approvals.
Warmup derives requests from the selected route rule, exact path/header/query
matches, method, and listener/route hostname intersection rather than assuming
one root request. This staging avoids racing Azure propagation against the
upstream test's 30-second HTTP consistency window; that window is not increased.
Upstream test assertions still validate backend identity and namespace.

## Adding tests

- Add the upstream test to `requiredTests`. Its declared feature set is enabled
  for that case, preventing accidental unsupported-feature skips.
- Add any additional upstream base Namespace/Gateway/Service/Deployment keys to
  `baseFixtureKeys`. Missing or ambiguous prerequisites fail setup explicitly.
- The same staging, discovery, approval, warmup, and cleanup code handles each case.
  No copied test implementation or separate approval loop is required.
- Cases run sequentially with fresh base fixtures. Owned namespaces must be unused
  initially; cleanup verifies namespace UID, waits for Gateway finalizers before
  backend withdrawal, and waits for namespace deletion before the next case.
- Invalid references or denied attachments receive no approval or success warmup;
  the original upstream assertions still decide whether those negative cases pass.
- HTTPS/custom-domain or regex warmup needs explicit test adaptation. Tests that
  create/change backend references inside their own execution need a further
  approval-observer hook; initial fixture staging does not cover those transitions.
- Increase the command/pipeline timeout when adding long-running cases. Preparation
  currently has a 75-minute bound per case and cleanup a separate 45-minute bound.

These changes do not add production capabilities or claim additional passing tests.

## Prerequisites

- An explicitly approved, workload-capable AKS test cluster, not an inaccessible
  managed Fleet hub.
- Matching Gateway API CRDs and a running AFD controller with accepted
  `azure-fleet-afd`, AFD enabled, and local Service origins explicitly enabled.
- AKS CCM support for selectorless companion internal LoadBalancer Services and PLS.
- Controller Reader access to its configured, verified AKS node resource group,
  plus existing AFD write permissions. The controller never approves connections.
- A separate authorized test caller with access to read the AFD profile and PLS and
  approve the exact private endpoint connection. No generic PLS auto-approval is used.
- Network policy permitting probes and requests to the original backend endpoints.

Local compilation and fixture/approval validation require no cluster or Azure:

```sh
go test -mod=readonly ./test/conformance
go test -mod=readonly -tags=conformance ./test/conformance -run '^$'
```

Live execution creates billable Azure infrastructure. Only after approval of the
subscription, resource group, cluster context, and connection-approval scope:

```sh
ENABLE_GATEWAY_CONFORMANCE=true \
ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL=true \
KUBECONFIG=/path/to/test-kubeconfig \
AZURE_SUBSCRIPTION_ID=<approved-subscription> \
AZURE_RESOURCE_GROUP=<approved-afd-group> \
LOCAL_SERVICE_RESOURCE_GROUP=<verified-hub-node-group> \
go test -mod=readonly -tags=conformance ./test/conformance \
  -run '^TestAFDConformance$' -count=1 -v -timeout=140m \
  -args -conformance-context=hub-admin
```

Use unused fixture namespaces and no concurrent runs.
The runner requests deletion of fixture Gateways and waits for their Azure cleanup
before backend fixture withdrawal. Companion Services retain CCM cleanup finalizers.
Collect controller/Azure diagnostics before deleting an approved retained environment.

## ADO integration

[The E2E pipeline](../../.pipeline/e2e-tests.yaml) adds a default-off
`runLocalServiceConformance` parameter. When explicitly selected for the AFD scenario,
setup enables local companions and grants scoped discovery access; a separate
command runs this test using the same service connection after the ordinary
Fleet E2E tests succeed. All five existing E2E scenarios are enabled. Setting
`runLocalServiceConformance=false` omits only the additional conformance step and
local-origin opt-in; normal E2E provisioning and tests still run.
Go JSON results are stored in the existing log artifact. Test failures fail the job;
existing always-run log collection and scoped environment cleanup remain active.

No live HTTP conformance pass is established by compile/unit checks.

## Failure diagnostics

Preparation logs changing resolver and approval wait reasons. Before upstream
cleanup removes routes or namespaces, the runner captures Services, EndpointSlices,
Gateway/HTTPRoute status, events, and Pod status (not Pod specifications or Secrets).
When `LOG_DIR` is set, snapshots and Gateway-manager logs are written to the existing
pipeline artifact. Gateway-manager logs are also included by normal E2E log collection.
