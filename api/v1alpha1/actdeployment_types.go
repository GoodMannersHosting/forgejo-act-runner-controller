/*
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
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ActDeploymentSpec defines the desired state of ActDeployment
type ActDeploymentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// ForgejoServer is the base URL of the Forgejo server (e.g., "https://git.cloud.danmanners.com")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://`
	ForgejoServer string `json:"forgejoServer"`

	// Organization is the Forgejo organization name to monitor for jobs
	// +kubebuilder:validation:Required
	Organization string `json:"organization"`

	// Labels is the label filter for jobs (e.g., "docker" or "ubuntu-22.04:docker://node:20-bullseye")
	// +kubebuilder:validation:Required
	Labels string `json:"labels"`

	// TokenSecretRef is a reference to a Secret containing the Forgejo API token
	// The secret should contain a key named "token" with the API token value
	TokenSecretRef corev1.SecretReference `json:"tokenSecretRef"`

	// PollInterval is the interval at which the listener pod polls Forgejo for pending jobs
	// Defaults to 10s if not specified
	// +optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`

	// MinRunners is the minimum number of ActRunner resources that should be maintained
	// If the current count is below this, the listener will create new ActRunner resources for pending jobs
	// Defaults to 0 if not specified
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinRunners *int32 `json:"minRunners,omitempty"`

	// MaxRunners is the maximum number of ActRunner resources that can be created concurrently
	// The listener will not create new ActRunner resources if the current count reaches this limit
	// Defaults to unlimited if not specified (0 means unlimited)
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxRunners *int32 `json:"maxRunners,omitempty"`

	// ListenerTemplate is the pod template for the listener pod that polls Forgejo API
	// +optional
	ListenerTemplate corev1.PodTemplateSpec `json:"listenerTemplate,omitempty"`

	// RunnerTemplate is the pod template for runner pods/jobs created by ActRunner resources
	// +optional
	RunnerTemplate corev1.PodTemplateSpec `json:"runnerTemplate,omitempty"`

	// RunnerImage is the default container image for runner pods
	// This will be used if RunnerTemplate does not specify a container image
	// +optional
	RunnerImage string `json:"runnerImage,omitempty"`

	// DockerInDockerImage is the Docker-in-Docker sidecar image for runner pods
	// Defaults to "docker.io/library/docker:29.1.3-dind-alpine3.23" if not specified
	// +optional
	DockerInDockerImage string `json:"dockerInDockerImage,omitempty"`

	// DockerConfigMapRef is an optional reference to a ConfigMap containing Docker config.json
	// If specified, the config.json will be mounted at ~/.docker/config.json in the runner container
	// The ConfigMap should contain a key named "config.json" with the Docker configuration
	// +optional
	DockerConfigMapRef *corev1.LocalObjectReference `json:"dockerConfigMapRef,omitempty"`
}

// ActDeploymentStatus defines the observed state of ActDeployment.
type ActDeploymentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the ActDeployment resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ListenerPodName is the name of the listener pod created for this ActDeployment
	// +optional
	ListenerPodName string `json:"listenerPodName,omitempty"`

	// LastPollTime is the timestamp of the last successful poll from the listener
	// +optional
	LastPollTime *metav1.Time `json:"lastPollTime,omitempty"`

	// ActiveActRunners is the count of active ActRunner resources created by this deployment
	// +optional
	ActiveActRunners int32 `json:"activeActRunners,omitempty"`

	// ObservedGeneration is the generation of the ActDeployment that was last reconciled
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ActDeployment is the Schema for the actdeployments API
type ActDeployment struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ActDeployment
	// +required
	Spec ActDeploymentSpec `json:"spec"`

	// status defines the observed state of ActDeployment
	// +optional
	Status ActDeploymentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ActDeploymentList contains a list of ActDeployment
type ActDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ActDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ActDeployment{}, &ActDeploymentList{})
}
