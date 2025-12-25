# Fixing CRD Annotation Size Error

If you encounter the error:
```
The CustomResourceDefinition "..." is invalid: metadata.annotations: Too long: may not be more than 262144 bytes
```

This happens because `PodTemplateSpec` fields create very large CRD schemas, and `kubectl apply` adds a `kubectl.kubernetes.io/last-applied-configuration` annotation that stores the entire CRD.

## Solution: Use kubectl create/replace (Now Default)

The `make install` command now uses `kubectl create` (with `kubectl replace` for updates) instead of `kubectl apply`, which prevents the annotation size issue.

If you still encounter the error:

1. Delete existing CRDs (if they exist with errors):
   ```bash
   kubectl delete crd runnerdeployments.forgejo.actions.io.github.com actrunners.forgejo.actions.io.github.com 2>/dev/null || true
   ```

2. Run `make install` again, or manually install:
   ```bash
   make manifests
   cd config/crd && kustomize build . | kubectl create -f -
   ```

For updates, `make install` will automatically use `kubectl replace`.

## Solution 2: Use kubectl apply --server-side

Use server-side apply which doesn't add the annotation:

```bash
make generate
make manifests
cd config/crd && kustomize build . | kubectl apply --server-side -f -
```

## Solution 3: Patch CRD to Reduce Schema Size (Advanced)

The `hack/patch-crd-size.sh` script (if you have `yq` or `python3` installed) will add `x-kubernetes-preserve-unknown-fields: true` to PodTemplateSpec fields, which tells Kubernetes to allow arbitrary fields without storing the full schema. This significantly reduces CRD size.

```bash
# Run the patch script after generating manifests
make manifests
bash hack/patch-crd-size.sh
make install
```

## Quick Fix for Current Issue

If you're stuck with the error right now:

1. Delete the existing CRD (if it exists):
   ```bash
   kubectl delete crd runnerdeployments.forgejo.actions.io.github.com || true
   kubectl delete crd actrunners.forgejo.actions.io.github.com || true
   ```

2. Install using kubectl create:
   ```bash
   cd config/crd && kustomize build . | kubectl create -f -
   ```

This will install the CRDs without the problematic annotation.

