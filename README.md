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

### Environment Setup

You need to run kubernetes locally, we recommend [minikube](https://minikube.sigs.k8s.io/docs/).

The frontend operator is dependent on [Clowder](https://github.com/RedHatInsights/clowder#getting-clowder). 
Follow those directions to get Clowder running and continue along.  

Once Clowder is up and running (`oc get pod -n clowder-system` has a running `controller-manager`), there are two
options we can use to proceed. 

0. create boot namespace

```
kubectl create namespace boot
```

1. apply frontend CRD

```
kubectl apply -f config/crd/bases/cloud.redhat.com_frontends.yaml
```

2. apply bundle CRD

```
kubectl apply -f config/crd/bases/cloud.redhat.com_bundles.yaml
```

3. Create the ClowdEnvironment (Clowder CRD)

```
kubectl apply -f examples/clowdenvironment.yaml
```

4. create frontend env

```
kubectl apply -f examples/feenvironment.yaml -n boot
```

5. create custom object inventory

```
kubectl apply -f examples/inventory.yaml -n boot
```

6. create bundle

```
kubectl apply -f examples/bundle.yaml -n boot
```

7. create chrome deployment

```
kubectl apply -f chrome.yaml -n boot
```

8. run the reconciler

```
make run
```

### Access it from your computer

If you want to access the app from your computer, you have to update /etc/hosts where the IP is the one from `minikube ip`

```
192.168.99.102 env-boot
192.168.99.102 env-boot-auth
```

Once you update it you can access the app from `https://env-boot/insights/inventory`
