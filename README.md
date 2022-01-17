# Insights Frontend Operator

### Local development

You need to run kubernetess locally, we rocommend using [minikube](https://minikube.sigs.k8s.io/docs/).

0. start minikube [Prerequisite]

```
minikube start --addons=ingress
```

1. apply frontend CRD

```
kubectl apply -f config/crd/bases/cloud.redhat.com_frontends.yaml
```

2. apply bundle CRD

```
kubectl apply -f config/crd/bases/cloud.redhat.com_bundles.yaml
```

3. create boot namespace

```
kubectl create namespace boot
```

4. create frontend env

```
kubectl apply -f environment.yaml -n boot
```

5. create custom object inventory

```
kubectl apply -f inventory.yml -n boot
```

6. create bundle

```
kubectl apply -f bundle.yaml -n boot
```

7. create bundle

```
kubectl apply -f chrome.yml -n boot
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
