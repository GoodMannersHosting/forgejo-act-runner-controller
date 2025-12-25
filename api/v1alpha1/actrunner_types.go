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

// JobData represents the full job payload from the Forgejo API
type JobData struct {
	// ID is the Forgejo job ID
	ID int64 `json:"id"`

	// RepoID is the repository ID
	RepoID int64 `json:"repo_id"`

	// OwnerID is the owner ID
	OwnerID int64 `json:"owner_id"`

	// Name is the job name
	Name string `json:"name"`

	// Needs lists the job dependencies
	// +optional
	Needs []string `json:"needs,omitempty"`

	// RunsOn specifies the runner labels/environment (e.g., ["ubuntu-22.04:docker://node:20-bullseye"])
	RunsOn []string `json:"runs_on"`

	// TaskID is the task ID
	TaskID int64 `json:"task_id"`

	// Status is the job status (e.g., "waiting", "running", "success", "failure")
	Status string `json:"status"`
}

// ActRunnerSpec defines the desired state of ActRunner
type ActRunnerSpec struct {
	// ForgejoJobID is the Forgejo job ID to execute
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	ForgejoJobID int64 `json:"forgejoJobID"`

	// ForgejoServer is the Forgejo server URL (inherited from RunnerDeployment)
	// +kubebuilder:validation:Required
	ForgejoServer string `json:"forgejoServer"`

	// Organization is the Forgejo organization name
	// +kubebuilder:validation:Required
	Organization string `json:"organization"`

	// TokenSecretRef is a reference to a Secret containing the Forgejo API token
	TokenSecretRef corev1.SecretReference `json:"tokenSecretRef"`

	// RegistrationTokenSecretRef is a reference to a Secret containing the runner registration token
	RegistrationTokenSecretRef corev1.SecretReference `json:"registrationTokenSecretRef"`

	// RunnerImage is the container image for the runner
	// +optional
	RunnerImage string `json:"runnerImage,omitempty"`

	// DockerInDockerImage is the Docker-in-Docker sidecar image
	// +optional
	DockerInDockerImage string `json:"dockerInDockerImage,omitempty"`

	// DockerConfigMapRef is an optional reference to a ConfigMap containing Docker config.json
	// +optional
	DockerConfigMapRef *corev1.LocalObjectReference `json:"dockerConfigMapRef,omitempty"`

	// JobData is the full job payload from Forgejo API
	JobData JobData `json:"jobData"`

	// JobTemplate is the pod template for the Kubernetes Pod that will execute this runner
	// +optional
	JobTemplate corev1.PodTemplateSpec `json:"jobTemplate,omitempty"`
}

// ActRunnerPhase represents the phase of an ActRunner
type ActRunnerPhase string

const (
	// ActRunnerPhasePending means the ActRunner is waiting to start
	ActRunnerPhasePending ActRunnerPhase = "Pending"

	// ActRunnerPhaseRunning means the ActRunner is currently running
	ActRunnerPhaseRunning ActRunnerPhase = "Running"

	// ActRunnerPhaseSucceeded means the ActRunner completed successfully
	ActRunnerPhaseSucceeded ActRunnerPhase = "Succeeded"

	// ActRunnerPhaseFailed means the ActRunner failed
	ActRunnerPhaseFailed ActRunnerPhase = "Failed"
)

// ActRunnerStatus defines the observed state of ActRunner
type ActRunnerStatus struct {
	// Phase represents the current phase of the ActRunner
	// +optional
	Phase ActRunnerPhase `json:"phase,omitempty"`

	// KubernetesJobName is the name of the Kubernetes Job created for this ActRunner
	// +optional
	KubernetesJobName string `json:"kubernetesJobName,omitempty"`

	// StartedAt is the timestamp when job execution started
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is the timestamp when job execution completed
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// RepositoryFullName is the full name of the repository (e.g., "owner/repo")
	// +optional
	RepositoryFullName string `json:"repositoryFullName,omitempty"`

	// TriggerUser is the login name of the user who triggered the run
	// +optional
	TriggerUser string `json:"triggerUser,omitempty"`

	// PrettyRef is the branch or tag reference (e.g., "main", "refs/heads/main")
	// +optional
	PrettyRef string `json:"prettyRef,omitempty"`

	// TriggerEvent is the event that triggered the run (e.g., "workflow_dispatch", "push")
	// +optional
	TriggerEvent string `json:"triggerEvent,omitempty"`

	// Conditions represent the current state of the ActRunner resource
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Job ID",type="integer",JSONPath=".spec.forgejoJobID"
// +kubebuilder:printcolumn:name="Repository",type="string",JSONPath=".status.repositoryFullName"
// +kubebuilder:printcolumn:name="User",type="string",JSONPath=".status.triggerUser"
// +kubebuilder:printcolumn:name="Ref",type="string",JSONPath=".status.prettyRef"
// +kubebuilder:printcolumn:name="Event",type="string",JSONPath=".status.triggerEvent"
// +kubebuilder:printcolumn:name="K8s Pod",type="string",JSONPath=".status.kubernetesJobName"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ActRunner is the Schema for the actrunners API
type ActRunner struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ActRunner
	// +required
	Spec ActRunnerSpec `json:"spec"`

	// status defines the observed state of ActRunner
	// +optional
	Status ActRunnerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ActRunnerList contains a list of ActRunner
type ActRunnerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ActRunner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ActRunner{}, &ActRunnerList{})
}
