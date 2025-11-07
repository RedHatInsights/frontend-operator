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

## Quick Start

### Using the Frontend Operator

The operator is generally available in the Consoledot Ephemeral and Dev Clusters. To use the Frontend Operator, you only need to apply a Frontend custom resource to a namespace that you manage using Bonfire.

### Deploying a Frontend with Bonfire in Ephemeral

[Bonfire](https://github.com/RedHatInsights/bonfire#bonfire-) is the consoledot tool used to interact with Kubernetes clusters.

Simply login to the ephemeral cluster and run:
```bash
bonfire deploy $MYAPP --frontends true -d 8h
```

This will give you access to your own ephemeral environment.

If your app does not have an entry into app-interface yet, `bonfire namespace reserve` will supply you with a bootstrapped namespace to deploy your application:
```bash
bonfire namespace reserve
oc apply -f $My-Frontend-CRD.yaml -n $NS
```

For detailed configuration options and examples, see the [FrontendEnvironment Configuration Guide](docs/antora/modules/ROOT/pages/frontendenvironment-guide.adoc).

## Local Development for Contributors

**Note**: We only recommend this method for local development on the **operator** **itself**.

Please use the above section to develop an app that depends on this operator.  

### Running Locally

You need to run kubernetes locally, we recommend [minikube](https://minikube.sigs.k8s.io/docs/).

The frontend operator is dependent on [Clowder](https://github.com/RedHatInsights/clowder#getting-clowder). 
Follow those directions to get Clowder running and continue along.  

Once Clowder is up and running (`oc get pod -n clowder-system` has a running `controller-manager`), there are two
options we can use to proceed. 

1. Create the boot namespace
```
$ make create-boot-namespace
```

2. Install the resources:
```
$ make install-resources
```

3. Run:
```
$ make run-local
```

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

The pushcache job or [valpop](https://github.com/RedHatInsights/valpop) will be disabled by default for all frontends.

To disable the pushcache job altogether through the Frontend Operator, set `enablePushCache` to `false` in the frontend enviornment of the FEO.

For local development purposes, the minio or AWS bucket secrets are stored under `examples/minio-bucket-secret.yaml`.

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

To run the kuttl tests you'll need to be running the operator and Clowder in minikube as shown in the directions above. You also need to make sure you [have kuttl installed on your machine](https://kuttl.dev/docs/cli.html#setup-the-kuttl-kubectl-plugin).

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
