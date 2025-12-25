# Dockerfiles

This directory contains multi-stage Docker builds for the Forgejo Actions Runner Controller.

## Available Dockerfiles

### Dockerfile.controller
Builds only the controller binary (main program).
- Builds from `cmd/main.go`
- Compresses with UPX
- Uses scratch base image

### Dockerfile.listener
Builds only the listener binary.
- Builds from `internal/listener/main.go`
- Compresses with UPX
- Uses scratch base image

### Dockerfile.combined
Builds both binaries in a single image.
- Includes both `/manager` (controller) and `/listener` binaries
- Both binaries are compressed with UPX
- Uses scratch base image
- Default entrypoint is `/manager` (controller)

## Building

### Build Controller Image
```bash
docker build -f Dockerfiles/Dockerfile.controller -t forgejo-act-runner-controller:latest .
```

### Build Listener Image
```bash
docker build -f Dockerfiles/Dockerfile.listener -t forgejo-act-runner-listener:latest .
```

### Build Combined Image
```bash
docker build -f Dockerfiles/Dockerfile.combined -t forgejo-act-runner-controller:latest .
```

### Build for Specific Platform
```bash
docker build --platform linux/amd64 -f Dockerfiles/Dockerfile.combined -t forgejo-act-runner-controller:latest .
docker build --platform linux/arm64 -f Dockerfiles/Dockerfile.combined -t forgejo-act-runner-controller:arm64 .
```

## Usage

### Using the Combined Image

When using the combined image, you can run either binary:

**Run the controller:**
```bash
docker run --rm forgejo-act-runner-controller:latest /manager
```

**Run the listener:**
```bash
docker run --rm forgejo-act-runner-controller:latest /listener \
  -forgejo-server="https://git.cloud.danmanners.com" \
  -organization="faro" \
  -labels="docker" \
  -token-secret-name="forgejo-token" \
  -token-secret-key="token" \
  -namespace="default" \
  -runner-deployment-name="runnerdeployment-sample" \
  -poll-interval="10s"
```

### In Kubernetes

For the listener Deployment, specify the listener binary:

```yaml
spec:
  listenerTemplate:
    spec:
      containers:
      - name: listener
        image: forgejo-act-runner-controller:latest
        command: ["/listener"]
        args:
        - -forgejo-server="https://git.cloud.danmanners.com"
        - -organization="faro"
        # ... other args
```

## Notes

- UPX compression may fail on some architectures. The `|| true` ensures the build continues even if UPX fails.
- The binaries are built with `-ldflags="-w -s"` to reduce binary size before UPX compression.
- Both binaries are statically linked (`CGO_ENABLED=0`) for maximum compatibility.
- The scratch base image provides the smallest possible image size but requires statically linked binaries.

