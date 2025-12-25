# Forgejo Act Runner Controller

The Forgejo Act Runner Controller (FARC) runs as a Kubernetes Operator to provison one-time use Action Runners for use with Forgejo Actions.

## Description

The FARC operator takes action on the `ActDeployment` CRD. It provisions a listener which watches a specified organization for specified labels on a given interval. When a job is detected as queued, it creates an `ActRunner` CRD, which then creates a one-time use pod.

> [!WARNING]
> Forgejo does not support ephemral runners yet, so one-time use offline runners will "build up" unless you trigger the administrative cronjob to remove offline runners. Not ideal, perhaps we can get a PR in for that.

The requirements for getting FARC deployed and operational:

- Operator Container
- Listener Container
- Runner Container
- CRDs

> [!NOTE]
> Early-access images will be available from `harbor.cloud.danmanners.com`. Once this is more stable, I'll publish the images to my Harbor, ghcr.io, Codeberg, and DockerHub.

## Getting Started

### Development Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push \
  IMG=<some-registry>/forgejo-act-runner-controller:tag
```

> [!NOTE]
> Make sure you have the proper permission to your registry target if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make generate && make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy \
  IMG=<some-registry>/forgejo-act-runner-controller:tag
```

> [!NOTE]
> If you encounter RBAC errors, you'll need to make sure that your controller (or local Kubeconfig) has the appropriate Role/RoleBinding.
> This will be handled automatically in the near future with the Helm Template.

**Create instances of your solution**

You can reference the samples from the config/sample directory, or use `kubectl explain ActDeployment` for specifics on configuration requirements.

## Contributing

// TODO(user): Add detailed information on how you I would like others to contribute to this project

## License

Copyright 2025 Dan Manners.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

