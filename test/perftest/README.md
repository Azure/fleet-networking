# Fleet Networking Performance Test Suite

This package features the performance test suite for Fleet networking controllers.

To run this test:

1. Bootstrap a fleet of AKS clusters as the test environment.

    ```sh
    # Replace YOUR-SUBSCRIPTION-ID with a value of your own.
    export AZURE_SUBSCRIPTION_ID=YOUR-SUBSCRIPTION-ID
    export AZURE_NETWORK_SETTING=perf-test

    . ./test/scripts/bootstrap.sh
    ```

    It should take approximately 40 minutes to complete the setup.

2. Pick a test suite and run it with Ginkgo:

    ```sh
    # Replace TEST_SUITE_PATH with a value of your own
    go test TEST_SUITE_PATH --ginkgo.v -test.v -timeout 1h | tee perf_test.log
    ```

    The list of currently available test suites are:

    | Path        | Description |
    | ----------- | ----------- |
    | `test/perftest/latency/peak` | A test suite for evaluating export/import latencies under peak workloads |
    | `test/perftest/latency/sustained` | A test suite for evaluating export/import latencies under sustained workloads |

    It is strongly recommended that **each test suite runs in its own environment**.

After finishing the suite, find the results, e.g., service export latencies and `endpointSlice`
export/import latencies in the output and/or the log file. To collect other metrics for further analysis:

1. Pick a cluster and find the public IP address for the Prometheus dashboard:

    ```sh
    # Replace YOUR-CONTEXT with a value of your own. It should be one of
    # `member-1-admin`, `member-2-admin`, `member-3-admin`, `member-4-admin`, and `hub-admin`.
    kubectl config use-context YOUR-CONTEXT
    kubectl get svc -n monitoring
    ```

    In the list of services, write down the external IP address of the service `metrics-dashboard`.

2. Open a browser and type in the address, `http://YOUR-IP-ADDRESS:9090` (replace `YOUR-IP-ADDRESS` with the public
IP of the `metrics-dashboard` service). The Prometheus dashboard will open for metric queries.

    A few metrics that might be of particular interests in this test suite are:

    * `workqueue_depth` (a Prometheus gauge)
    * `workqueue_work_duration_seconds` (a Prometheus histogram)
    * `workqueue_queue_duration_seconds` (a Prometheus histogram)

    Note that due to AKS's architecture, metrics about the Kubernetes API server, the etcd backend, and a number of
    other control plane components are not available through the `kube-prometheus-stack` installed; find these data through AKS instead.
