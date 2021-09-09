# Insights Frontend Operator

### Local development
You need to run kubernetess locally, we rocommend using [minikube](https://minikube.sigs.k8s.io/docs/).

1) start minikube
```
minikube start --addons=ingress
```

2) apply frontend CRD
```
kubectl apply -f config/crd/bases/cloud.redhat.com_frontends.yaml
```

3) apply bundle CRD
```
kubectl apply -f config/crd/bases/cloud.redhat.com_bunldes.yaml
```

4) create custom object inventory
```
kubectl apply -f inventory.yml
```

5) create custom object
```
kubectl apply -f bundle.yml
```

6) create fon namespace
```
kubect create namespace fon
```

7) run the reconciler
```
make run
```
### Access it from your computer
If you want to access the app from your computer, you have to update /etc/hots where the IP is the one from `minikube ip`

```
192.168.99.102 fon
```

Once you update it you can access the app from `https://fon:32078/` where the port is from `minikube service list` and look for `ingress-nginx-controller`

