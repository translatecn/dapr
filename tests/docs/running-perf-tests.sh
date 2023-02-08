go get sigs.k8s.io/kind
make setup-kind
kubectl create namespace dapr-tests
export DAPR_REGISTRY=docker.io/acejilam
export DAPR_TAG=dev
export DAPR_NAMESPACE=dapr-tests
# Do not set DAPR_TEST_ENV if you do not use minikube
export DAPR_TEST_ENV=minikube
export DAPR_PERF_QPS
export DAPR_TEST_NAMESPACE=mesoid
export DAPR_PERF_CONNECTIONS
export DAPR_TEST_DURATION
export DAPR_PAYLOAD_SIZE
export DAPR_SIDECAR_CPU_LIMIT
export DAPR_SIDECAR_MEMORY_LIMIT
export DAPR_SIDECAR_CPU_REQUEST
export DAPR_SIDECAR_MEMORY_REQUEST
make build-linux
make docker-build
make docker-push
make docker-deploy-k8s
make setup-3rd-party
make setup-app-configurations
export DAPR_DISABLE_TELEMETRY=true
make setup-disable-mtls
make setup-test-components
# build perf apps docker image under apps/
make build-perf-app-all
make push-perf-app-all
make test-perf-all

## Run perf tests through GitHub Actions

#Once a contributor creates a pull request, E2E tests on KinD clusters are automatically executed for faster feedback. In order to run the E2E tests on AKS, ask a maintainer or approver to add /ok-to-perf comment to the Pull Request.
