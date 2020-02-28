# Dioscuri
The Dioscuri were regarded as helpers of humankind and held to be patrons of travellers and of sailors in particular, who invoked them to seek favourable winds

> Note: This is currently only suitable to run in Openshift

# Usage
## OpenShift
### Route Migration
Any routes that need to be migrated between the source and destination namespaces need to be labelled.
Routes flagged in both the source and destination namespaces will be swapped between them.

The examples below, when triggered, will:
* swap `standby.example.com` from `namespaceB` to `namespaceA`
* swap `www.example.com` from `namespaceA` to `namespaceB`
* leave `namespacea.example.com` and `namespaceb.example.com` alone
###### NamespaceB
```
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: standby-route
  namespace: namespaceB
  labels:
    dioscuri.amazee.io/migrate: 'true'
spec:
  host: standby.example.com
  ...
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: always-namespaceb
  namespace: namespaceB
spec:
  host: namespaceb.example.com
  ...
```
###### NamespaceA
```
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: production-www-route
  namespace: namespaceA
  labels:
    dioscuri.amazee.io/migrate: 'true'
spec:
  host: www.example.com
  ...
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: production-route
  namespace: namespaceA
  labels:
    dioscuri.amazee.io/migrate: 'true'
spec:
  host: production.example.com
  ...
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: always-namespacea
  namespace: namespaceA
spec:
  host: namespacea.example.com
  ...
```

### Triggering a migration
Create a resource of kind `RouteMigrate` in the source namespace, containing the `spec.destinationNamespace` where the routes should migrate or swap between.
```
apiVersion: dioscuri.amazee.io/v1
kind: RouteMigrate
metadata:
  name: active-standby
  namespace: namespaceA
  annotations:
    dioscuri.amazee.io/migrate: 'true'
spec:
  destinationNamespace: namespaceB
```