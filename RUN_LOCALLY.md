# Running the Controller Locally

This guide explains how to run the Forgejo Actions Runner Controller locally for development and testing.

## Prerequisites

1. A Kubernetes cluster (kind, minikube, or any other cluster)
2. `kubectl` configured to access your cluster
3. Go 1.24.6+ installed
4. Make installed
5. Access to your Forgejo server (or a test instance)

## Step 1: Generate Code and Install CRDs

First, generate the necessary code and install the CRDs into your cluster:

```bash
# Generate code (deepcopy, CRDs, RBAC)
make generate
make manifests

# Install CRDs into your cluster
make install
```

## Step 2: Create a Secret with Your Forgejo Token

Create a Kubernetes secret containing your Forgejo API token:

```bash
kubectl create secret generic forgejo-token \
  --from-literal=token="$(cat ~/.forgejo/danmanners)" \
  -n default
```

Or create the secret manually:

```bash
kubectl create secret generic forgejo-token \
  --from-literal=token="your-token-here" \
  -n default
```

**Note**: The secret must be in the same namespace as your RunnerDeployment.

## Step 3: Create a Sample RunnerDeployment

Create a sample RunnerDeployment resource. Update `config/samples/forgejo.actions.io_v1alpha1_runnerdeployment.yaml`:

```yaml
apiVersion: forgejo.actions.io.github.com/v1alpha1
kind: RunnerDeployment
metadata:
  name: runnerdeployment-sample
  namespace: default
spec:
  forgejoServer: "https://git.cloud.danmanners.com"
  organization: "faro"
  labels: "docker"
  tokenSecretRef:
    name: forgejo-token
    namespace: default
  pollInterval: "10s"
  # Optional: Customize listener pod template
  # listenerTemplate:
  #   spec:
  #     containers:
  #     - name: listener
  #       image: your-image:tag
  #       command: ["/listener"]
  # Optional: Customize runner pod template
  # runnerTemplate:
  #   spec:
  #     containers:
  #     - name: runner
  #       image: catthehacker/ubuntu:act-latest
```

Then apply it:

```bash
kubectl apply -f config/samples/forgejo.actions.io_v1alpha1_runnerdeployment.yaml
```

## Step 4: Run the Controller Locally

Run the controller directly on your machine (it will connect to your cluster):

```bash
make run
```

Or run it directly with Go:

```bash
go run ./cmd/main.go
```

The controller will:
- Connect to your Kubernetes cluster (using `~/.kube/config`)
- Start watching for RunnerDeployment resources
- Create listener Deployments when RunnerDeployments are created
- Start watching for ActRunner resources
- Create Kubernetes Jobs when ActRunners are created

## Step 5: Verify It's Working

### Check the Controller Logs

The controller should start and show logs like:

```
INFO    controller-runtime.metrics      Starting metrics server
INFO    setup   Starting manager
INFO    controller   Starting EventSource    {"controller": "runnerdeployment"}
INFO    controller   Starting Controller     {"controller": "runnerdeployment"}
INFO    controller   Starting workers        {"controller": "runnerdeployment", "worker count": 1}
```

### Check the RunnerDeployment Status

```bash
kubectl get runnerdeployment runnerdeployment-sample -o yaml
```

You should see:
- Status with `listenerPodName` set
- Status with `activeActRunners` count

### Check the Listener Deployment

```bash
kubectl get deployment -l app=forgejo-listener
kubectl get pods -l app=forgejo-listener
```

**Note**: The listener pod will fail to start because the listener binary doesn't exist yet in the container image. You'll need to either:

1. Build a custom image that includes the listener binary
2. Run the listener separately (see below)

### Check ActRunner Resources

```bash
kubectl get actrunners
```

These should be created by the listener pod when it finds pending jobs.

### Check Kubernetes Jobs

```bash
kubectl get jobs -l forgejo.actions.io/actrunner
```

## Running the Listener Separately (Development)

Since the listener is a separate binary, you can run it locally for development:

### Build the Listener Binary

```bash
go build -o bin/listener ./internal/listener/main.go
```

### Run the Listener Locally

You'll need to provide the required flags:

```bash
./bin/listener \
  -forgejo-server="https://git.cloud.danmanners.com" \
  -organization="faro" \
  -labels="docker" \
  -token-secret-name="forgejo-token" \
  -token-secret-key="token" \
  -namespace="default" \
  -runner-deployment-name="runnerdeployment-sample" \
  -poll-interval="10s"
```

The listener needs access to your Kubernetes cluster (via `~/.kube/config`) to:
- Read the token secret
- Read the RunnerDeployment
- List and create ActRunner resources

## Troubleshooting

### Controller Can't Connect to Cluster

Ensure your `kubectl` is configured correctly:
```bash
kubectl cluster-info
kubectl get nodes
```

### CRDs Not Found

Make sure you ran `make install`:
```bash
kubectl get crd runnerdeployments.forgejo.actions.io.github.com
kubectl get crd actrunners.forgejo.actions.io.github.com
```

### RBAC Errors

The controller needs various permissions. The RBAC is generated from annotations. If you see permission errors, check:

```bash
kubectl get clusterrole forgejo-act-runner-controller-manager-role
kubectl describe clusterrole forgejo-act-runner-controller-manager-role
```

### Listener Pod Not Starting

The listener pod requires the listener binary to be in the container image. For now, you can:

1. Run the listener locally (as shown above)
2. Build a custom Docker image that includes both the controller and listener binaries
3. Update the `listenerTemplate` in RunnerDeployment to use your custom image

### No ActRunners Being Created

1. Check if there are pending jobs in Forgejo:
   ```bash
   curl -H "Authorization: token $(cat ~/.forgejo/danmanners)" \
     "https://git.cloud.danmanners.com/api/v1/orgs/faro/actions/runners/jobs?labels=docker"
   ```

2. Check listener logs (if running locally) for errors
3. Verify the token secret is correct
4. Verify the Forgejo server URL, organization, and labels are correct

## Building a Complete Docker Image

To build an image that includes both the controller and listener:

1. Update the Dockerfile to build both binaries
2. Build the image:
   ```bash
   make docker-build IMG=your-registry/forgejo-act-runner-controller:dev
   ```
3. Update the listener template to use this image

## Next Steps

Once everything is running:
1. Create a test workflow in Forgejo that uses the labels you specified
2. Trigger the workflow
3. Watch as ActRunners are created and Kubernetes Jobs are scheduled
4. Check the job logs to see the execution

