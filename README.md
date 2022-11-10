# Insights Frontend Operator

A Kubernetes operator designed to deploy and managed containerized frontends.

## Usage

The operator is generally availble in the Consoledot Ephemeral and Dev Clusters. In order to use the Frontend Operator, 
you only need to apply a Frontend CRD to a namespace that you manage using Bonfire. 

## Deploying a Frontend with Bonfire in ephemeral

[Bonfire](https://github.com/RedHatInsights/bonfire#bonfire-) is the consoledot tool used to interact with Kuberentes clusters.

Simply login to the ephemeral cluster and run `bonfire deploy $MYAPP --frontends true -d 8h` to get access to your own ephemeral environment. 

If your app does not have an entry into app-interface yet, `bonfire namespace reserve` will supply you with a bootstrapped
namespace to deploy your application with `oc apply -f $My-Frontend-CRD.yaml -n $NS`

## Local development for contributors

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
