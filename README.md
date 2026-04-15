# Insights Frontend Operator

A Kubernetes operator designed to deploy and manage containerized frontend applications in the Red Hat Insights (consoledot) ecosystem.

## Documentation

Comprehensive documentation is available to help you get started and configure the Frontend Operator:

### For Users

- **[FrontendEnvironment Configuration Guide](docs/antora/modules/ROOT/pages/frontendenvironment-guide.adoc)** - Complete guide for creating and configuring FrontendEnvironment custom resources
  - Step-by-step instructions for basic and advanced configurations
  - Common use cases and practical examples
  - Troubleshooting guide
  - Integration with Bonfire for ephemeral deployments

- **[API Reference](docs/antora/modules/ROOT/pages/api_reference.adoc)** - Complete API specification for all custom resources
  - Detailed field descriptions for FrontendEnvironment and Frontend resources
  - Validation requirements and best practices
  - Cross-references to configuration guides

- **[Documentation Index](docs/antora/modules/ROOT/pages/index.adoc)** - Documentation landing page with navigation to all guides

### For Contributors

See the [Local Development](#local-development-for-contributors) section below for instructions on setting up the operator for local development.

### For AI Agents

- **[AGENTS.md](AGENTS.md)** - Onboarding guide for AI-assisted development: project conventions, structure overview, and documentation index
- **[Testing Guidelines](docs/testing-guidelines.md)** - Unit test patterns, e2e test conventions, CI checks
- **[Operator Development Guidelines](docs/operator-development-guidelines.md)** - CRD types, reconciliation flow, ConfigMap generation, pushcache jobs

## Quick Start

### Using the Frontend Operator

The Frontend Operator is already running in the Consoledot Ephemeral and Dev Clusters — you do not need to deploy it yourself. To use it, you simply apply a Frontend custom resource to a namespace you manage.

### Deploying a Frontend in Ephemeral

[Bonfire](https://github.com/RedHatInsights/bonfire#bonfire-) is the consoledot tool for deploying **applications** into ephemeral namespaces. It is not used to deploy the Frontend Operator itself.

To deploy your app into an ephemeral namespace where the operator is already running:
```bash
bonfire deploy $MYAPP --frontends true -d 8h
```

If your app does not have an entry in app-interface yet, reserve a namespace and apply your Frontend resource manually:
```bash
bonfire namespace reserve
oc apply -f $My-Frontend-CRD.yaml -n $NS
```

For detailed configuration options and examples, see the [FrontendEnvironment Configuration Guide](docs/antora/modules/ROOT/pages/frontendenvironment-guide.adoc).

If you are running a full app stack locally — including backend services managed by [Clowder](https://github.com/RedHatInsights/clowder) — you will need both operators running in your local cluster. See the Clowder repo for setup instructions. Once Clowder's CRDs are installed, apply the example ClowdEnvironment:

```sh
oc apply -f examples/clowdenvironment.yaml
```

## Local Development for Contributors

**Note**: We only recommend this method for local development on the **operator** **itself**.

Please use the above section to develop an app that depends on this operator.  

### Running Locally

You need to run kubernetes locally, we recommend [minikube](https://minikube.sigs.k8s.io/docs/).

You will also need the [OpenShift CLI (`oc`)](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) installed, as the resource commands use `oc` rather than `kubectl`.

The operator creates Ingress resources (defaulting to the `nginx` ingress class) for each Frontend and for the reverse proxy. Enable the ingress addon if you want deployed frontends to be accessible in the browser:

```sh
minikube addons enable ingress
```

```
# Create the `boot` and `env-boot` namespaces (also regenerates manifests):
make create-namespaces

# Install CRDs and example resources:
make install-resources

# Run (defaults to Info log level, use --log-level -1 for Debug)
make run-local
```

The operator supports the following log levels via `--log-level`: `-1` (Debug), `0` (Info), `1` (Warn), `2` (Error). `make run-local` defaults to Info.

If you make changes to the CRDs make sure to install the resources and run again.


### Debug in VS Code
Create `.vscode/launch.json` and put this in that file:

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug Operator",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/",
            "args": [
                "--metrics-bind-address", ":9090",
                "--health-probe-bind-address", ":9091",
            ]
        }
    ]
}
```
Once that is saved you'll see "Debug Operator" in the launch menu in VS Code. Also, before running in VS Code make sure you have created the boot namespace and install the resources as shown above.

### Access it from your computer

If you want to access the app from your computer, you have to update /etc/hosts where the IP is the one from `minikube ip`

```
192.168.99.102 env-boot
192.168.99.102 env-boot-auth
```

Once you update it you can access the app from `https://env-boot/insights/inventory`

### Pushcache (valpop) job

The pushcache job or [valpop](https://github.com/RedHatInsights/valpop) copies frontend assets to an S3 bucket (MinIO locally). It is enabled by default in `examples/feenvironment.yaml` using the valpop image from the Red Hat image registry.

If you enable push cache (`enablePushCache: true`), you must also set `valpopImage` in the FrontendEnvironment, otherwise the operator will error on reconciliation:

```yaml
spec:
  enablePushCache: true
  valpopImage: quay.io/redhat-services-prod/hcc-platex-services-tenant/valpop:latest
```

For local development, MinIO is used as the S3 backend. The bucket secrets are stored under `examples/minio-bucket-secret.yaml`.

### Reverse Proxy

The reverse proxy functionality allows you to deploy a Caddy-based reverse proxy that serves frontend assets from an S3-compatible object storage backend. This is part of an initiative to implement an object storage-based push cache for historical and current frontend assets.

The reverse proxy uses the [frontend-asset-proxy](https://github.com/RedHatInsights/frontend-asset-proxy) container image and supports:

- Reverse proxying requests to S3/Minio
- SPA routing support by serving the main application entrypoint for non-existent asset paths
- Configurable runtime behavior via environment variables
- Health checks via `/healthz` endpoint

To enable the reverse proxy for a frontend environment, configure the following in your FrontendEnvironment:

```yaml
spec:
  reverseProxyImage: quay.io/redhatinsights/frontend-asset-proxy:latest
  reverseProxySPAEntrypointPath: /index.html  # optional, defaults to /index.html
  reverseProxyLogLevel: INFO  # optional, defaults to DEBUG
```

This will create a deployment and service for the reverse proxy, making it accessible within the cluster.

## E2E testing with kuttl

[Kuttl](https://kuttl.dev/) is an end to end testing framework for Kubernetes operators. We hope to provide full test coverage for the Frontend Operator with kuttl.

To run the kuttl tests you'll need to be running the operator in minikube as shown in the directions above. You also need to make sure you [have kuttl installed on your machine](https://github.com/kudobuilder/kuttl/blob/main/docs/cli.md).

Once all that is in place you can run the kuttl tests:

```bash
$ make kuttl
```
Friendly reminder: make sure you have the frontend operator runnning (`make run-local`) before you run the tests or they will never work and you'll go nuts trying to figure out why.

If you want to run a single test you can do this:
```bash
$ kubectl kuttl test --config kuttl-config.yml  ./tests/e2e --test bundles
```
where `bundles` is the name of the directory that contains the test you want to run.
