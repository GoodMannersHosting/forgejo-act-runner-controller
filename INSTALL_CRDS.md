# Installing CRDs

This guide explains how to generate and install the Custom Resource Definitions (CRDs) for the Forgejo Actions Runner Controller.

## Quick Start

```bash
# Generate code and CRDs, then install them
make generate
make manifests
make install
```

Or as a single command (if dependencies are set up correctly):
```bash
make install
```

## Step-by-Step

### 1. Generate DeepCopy Code

This generates the necessary DeepCopy methods for your custom resources:

```bash
make generate
```

This will:
- Generate deepcopy code in `api/v1alpha1/zz_generated.deepcopy.go`
- Required for CRD generation

### 2. Generate CRD Manifests

This generates the CRD YAML files:

```bash
make manifests
```

This will:
- Generate CRDs in `config/crd/bases/`
- Generate RBAC manifests in `config/rbac/`
- Create files like:
  - `config/crd/bases/forgejo.actions.io.github.com_runnerdeployments.yaml`
  - `config/crd/bases/forgejo.actions.io.github.com_actrunners.yaml`

### 3. Install CRDs into Cluster

This installs the CRDs into your Kubernetes cluster:

```bash
make install
```

This will:
- Use kustomize to build the CRD manifests from `config/crd/`
- Apply them to your cluster using `kubectl apply`
- Install both `RunnerDeployment` and `ActRunner` CRDs

## Verify Installation

Check that the CRDs are installed:

```bash
kubectl get crd runnerdeployments.forgejo.actions.io.github.com
kubectl get crd actrunners.forgejo.actions.io.github.com
```

Or get details:

```bash
kubectl get crd runnerdeployments.forgejo.actions.io.github.com -o yaml
kubectl get crd actrunners.forgejo.actions.io.github.com -o yaml
```

## View Generated CRDs

To see what will be installed (without installing):

```bash
cd config/crd && kustomize build .
```

Or view individual CRD files:

```bash
cat config/crd/bases/forgejo.actions.io.github.com_runnerdeployments.yaml
cat config/crd/bases/forgejo.actions.io.github.com_actrunners.yaml
```

## Uninstall CRDs

To remove the CRDs from your cluster:

```bash
make uninstall
```

This will delete both CRD resources from your cluster.

## Troubleshooting

### CRDs Already Exist

If CRDs already exist and you're updating them, `make install` will update them in place. However, CRD updates may be restricted if there are existing resources. You may need to:

1. Delete existing resources first (if safe)
2. Delete old CRD versions
3. Install new CRDs

### Permission Errors

Ensure you have cluster-admin permissions or the necessary RBAC:

```bash
kubectl auth can-i create crd
```

If not, you may need to grant yourself permissions or use a user with appropriate access.

### Generation Errors

If `make generate` or `make manifests` fails:

1. Ensure `controller-gen` is installed (make will install it automatically)
2. Check that your Go types have proper kubebuilder markers
3. Verify that all imports are correct

