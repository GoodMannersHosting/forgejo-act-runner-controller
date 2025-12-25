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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgejoactionsiov1alpha1 "github.com/goodmannershosting/forgejo-act-runner-controller/api/v1alpha1"
)

// ActRunnerReconciler reconciles an ActRunner object
type ActRunnerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=forgejo.actions.io.github.com,resources=actrunners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forgejo.actions.io.github.com,resources=actrunners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forgejo.actions.io.github.com,resources=actrunners/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ActRunnerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	actRunner := &forgejoactionsiov1alpha1.ActRunner{}
	if err := r.Get(ctx, req.NamespacedName, actRunner); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion - clean up registration token secret
	if !actRunner.DeletionTimestamp.IsZero() {
		if err := r.cleanupRegistrationSecret(ctx, log, actRunner); err != nil {
			log.Error(err, "failed to cleanup registration secret during deletion")
			// Don't return error - we still want deletion to proceed
		}
		return ctrl.Result{}, nil
	}

	// Determine current phase based on Kubernetes Pod status
	var k8sPod *corev1.Pod
	if actRunner.Status.KubernetesJobName != "" {
		k8sPod = &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: actRunner.Namespace, Name: actRunner.Status.KubernetesJobName}, k8sPod); err != nil {
			if client.IgnoreNotFound(err) == nil {
				// Pod was deleted, reset status
				actRunner.Status.KubernetesJobName = ""
				actRunner.Status.Phase = forgejoactionsiov1alpha1.ActRunnerPhasePending
				if err := r.Status().Update(ctx, actRunner); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	}

	// Update phase based on Pod status
	newPhase := r.determinePhase(k8sPod)
	if actRunner.Status.Phase != newPhase {
		actRunner.Status.Phase = newPhase

		now := metav1.Now()
		if newPhase == forgejoactionsiov1alpha1.ActRunnerPhaseRunning && actRunner.Status.StartedAt == nil {
			actRunner.Status.StartedAt = &now
		}
		if (newPhase == forgejoactionsiov1alpha1.ActRunnerPhaseSucceeded || newPhase == forgejoactionsiov1alpha1.ActRunnerPhaseFailed) && actRunner.Status.CompletedAt == nil {
			actRunner.Status.CompletedAt = &now
		}

		if err := r.Status().Update(ctx, actRunner); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If pending, create Kubernetes Pod
	if actRunner.Status.Phase == forgejoactionsiov1alpha1.ActRunnerPhasePending {
		if err := r.createKubernetesPod(ctx, actRunner); err != nil {
			log.Error(err, "failed to create Kubernetes Pod")
			return ctrl.Result{}, err
		}
		// Requeue to check Pod status
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// If running, periodically check status
	if actRunner.Status.Phase == forgejoactionsiov1alpha1.ActRunnerPhaseRunning {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// If succeeded or failed, clean up registration token secret and stop reconciling
	if actRunner.Status.Phase == forgejoactionsiov1alpha1.ActRunnerPhaseSucceeded ||
		actRunner.Status.Phase == forgejoactionsiov1alpha1.ActRunnerPhaseFailed {
		// Clean up the registration token secret when runner is finished
		// We do this on each reconcile while finished to handle cases where cleanup failed previously
		if err := r.cleanupRegistrationSecret(ctx, log, actRunner); err != nil {
			// Log but don't fail - we'll retry on next reconcile
			log.Error(err, "failed to cleanup registration secret for finished runner")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ActRunnerReconciler) determinePhase(pod *corev1.Pod) forgejoactionsiov1alpha1.ActRunnerPhase {
	if pod == nil {
		return forgejoactionsiov1alpha1.ActRunnerPhasePending
	}

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		return forgejoactionsiov1alpha1.ActRunnerPhaseSucceeded
	case corev1.PodFailed:
		return forgejoactionsiov1alpha1.ActRunnerPhaseFailed
	case corev1.PodRunning:
		return forgejoactionsiov1alpha1.ActRunnerPhaseRunning
	default:
		return forgejoactionsiov1alpha1.ActRunnerPhasePending
	}
}

func (r *ActRunnerReconciler) createKubernetesPod(ctx context.Context, actRunner *forgejoactionsiov1alpha1.ActRunner) error {
	podName := fmt.Sprintf("runner-%d-%s", actRunner.Spec.ForgejoJobID, actRunner.Name)
	if len(podName) > 63 {
		podName = podName[:63]
	}

	// Use JobTemplate from spec as base
	// This allows runnerTemplate to specify pod-level fields (like dnsPolicy, hostAliases, etc.)
	// without requiring a containers section - we'll add a default container if needed
	podTemplate := actRunner.Spec.JobTemplate.DeepCopy()
	if podTemplate.ObjectMeta.Labels == nil {
		podTemplate.ObjectMeta.Labels = make(map[string]string)
	}
	podTemplate.ObjectMeta.Labels["forgejo.actions.io/job-id"] = fmt.Sprintf("%d", actRunner.Spec.ForgejoJobID)
	podTemplate.ObjectMeta.Labels["forgejo.actions.io/actrunner"] = actRunner.Name

	// Set default runner container if not specified in runnerTemplate
	// This allows users to specify pod-level overrides (dnsPolicy, hostAliases, etc.)
	// without having to define a containers section
	if len(podTemplate.Spec.Containers) == 0 {
		runnerImage := actRunner.Spec.RunnerImage
		if runnerImage == "" {
			runnerImage = "runner-image:latest" // Fallback default
		}
		podTemplate.Spec.Containers = []corev1.Container{
			{
				Name:  "runner",
				Image: runnerImage,
			},
		}
	}

	// Configure runner container
	// We'll modify the first container directly (don't use a pointer since we'll be appending to Containers slice)
	runnerContainer := &podTemplate.Spec.Containers[0]
	runnerContainer.Name = "runner"

	// Override image if RunnerImage is specified in spec
	if actRunner.Spec.RunnerImage != "" {
		runnerContainer.Image = actRunner.Spec.RunnerImage
	}

	// Initialize volume mounts early to ensure they're available
	if runnerContainer.VolumeMounts == nil {
		runnerContainer.VolumeMounts = []corev1.VolumeMount{}
	}

	// Add registration token from secret as TOKEN environment variable
	// We use an explicit EnvVar instead of envFrom to ensure the variable name is TOKEN (uppercase)
	runnerContainer.Env = append(runnerContainer.Env,
		corev1.EnvVar{
			Name: "TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: actRunner.Spec.RegistrationTokenSecretRef.Name,
					},
					Key: "token",
				},
			},
		},
	)

	// Add additional environment variables
	if runnerContainer.Env == nil {
		runnerContainer.Env = []corev1.EnvVar{}
	}
	// Build labels string from job data (comma-separated)
	labels := ""
	if len(actRunner.Spec.JobData.RunsOn) > 0 {
		labels = strings.Join(actRunner.Spec.JobData.RunsOn, ",")
	}

	runnerContainer.Env = append(runnerContainer.Env,
		corev1.EnvVar{
			Name:  "FORGEJO_SERVER",
			Value: actRunner.Spec.ForgejoServer,
		},
		corev1.EnvVar{
			Name:  "FORGEJO_ORG",
			Value: actRunner.Spec.Organization,
		},
		corev1.EnvVar{
			Name:  "FORGEJO_LABELS",
			Value: labels,
		},
	)

	// Add repository and run information if available in status
	if actRunner.Status.RepositoryFullName != "" {
		runnerContainer.Env = append(runnerContainer.Env,
			corev1.EnvVar{
				Name:  "FORGEJO_REPOSITORY",
				Value: actRunner.Status.RepositoryFullName,
			},
		)
	}
	if actRunner.Status.TriggerUser != "" {
		runnerContainer.Env = append(runnerContainer.Env,
			corev1.EnvVar{
				Name:  "FORGEJO_TRIGGER_USER",
				Value: actRunner.Status.TriggerUser,
			},
		)
	}
	if actRunner.Status.PrettyRef != "" {
		runnerContainer.Env = append(runnerContainer.Env,
			corev1.EnvVar{
				Name:  "FORGEJO_REF",
				Value: actRunner.Status.PrettyRef,
			},
		)
	}
	if actRunner.Status.TriggerEvent != "" {
		runnerContainer.Env = append(runnerContainer.Env,
			corev1.EnvVar{
				Name:  "FORGEJO_TRIGGER_EVENT",
				Value: actRunner.Status.TriggerEvent,
			},
		)
	}

	// Set DOCKER_HOST to use Unix socket (override if already set in JobTemplate)
	// Remove any existing DOCKER_HOST env var first to avoid duplicates
	envWithoutDockerHost := []corev1.EnvVar{}
	for _, env := range runnerContainer.Env {
		if env.Name != "DOCKER_HOST" {
			envWithoutDockerHost = append(envWithoutDockerHost, env)
		}
	}
	runnerContainer.Env = envWithoutDockerHost
	// Now add our DOCKER_HOST
	runnerContainer.Env = append(runnerContainer.Env,
		corev1.EnvVar{
			Name:  "DOCKER_HOST",
			Value: "unix:///var/docker/docker.sock",
		},
	)

	// Determine DinD image (default if not specified)
	dindImage := actRunner.Spec.DockerInDockerImage
	if dindImage == "" {
		dindImage = "docker.io/library/docker:29.1.3-dind-alpine3.23"
	}

	// Add DinD sidecar container
	// We mount the docker-socket volume at /var/docker, and configure dockerd to create the socket there
	dindContainer := corev1.Container{
		Name:  "dind",
		Image: dindImage,
		SecurityContext: &corev1.SecurityContext{
			Privileged: func() *bool { b := true; return &b }(),
		},
		Env: []corev1.EnvVar{
			{
				Name:  "DOCKER_TLS_CERTDIR",
				Value: "",
			},
		},
		Args: []string{
			"dockerd",
			"--host=unix:///var/docker/docker.sock",
			"--storage-driver=vfs",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "docker-socket",
				MountPath: "/var/docker",
			},
		},
	}

	// Add shared emptyDir volume for Docker socket
	dockerSocketVolume := corev1.Volume{
		Name: "docker-socket",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	if podTemplate.Spec.Volumes == nil {
		podTemplate.Spec.Volumes = []corev1.Volume{}
	}
	podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, dockerSocketVolume)

	// Mount Docker socket volume in runner container (shared emptyDir with DinD)
	// Check if docker-socket volume mount already exists (from JobTemplate) and remove it if present
	// Then add our mount to ensure it's always present with the correct path
	// Note: We must do this BEFORE appending the DinD container, since appending might reallocate the slice
	filteredVolumeMounts := []corev1.VolumeMount{}
	for _, vm := range podTemplate.Spec.Containers[0].VolumeMounts {
		if vm.Name != "docker-socket" {
			filteredVolumeMounts = append(filteredVolumeMounts, vm)
		}
	}
	podTemplate.Spec.Containers[0].VolumeMounts = filteredVolumeMounts
	// Always add the docker-socket mount (this ensures it's always present)
	podTemplate.Spec.Containers[0].VolumeMounts = append(podTemplate.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      "docker-socket",
			MountPath: "/var/docker",
		},
	)

	// Add DinD sidecar container AFTER we've finished modifying the runner container
	// This avoids potential pointer invalidation issues if the slice needs to reallocate
	podTemplate.Spec.Containers = append(podTemplate.Spec.Containers, dindContainer)

	// Mount Docker config.json from ConfigMap if specified
	if actRunner.Spec.DockerConfigMapRef != nil && actRunner.Spec.DockerConfigMapRef.Name != "" {
		// Add volume for Docker config
		dockerConfigVolume := corev1.Volume{
			Name: "docker-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: *actRunner.Spec.DockerConfigMapRef,
					Items: []corev1.KeyToPath{
						{
							Key:  "config.json",
							Path: "config.json",
						},
					},
				},
			},
		}
		podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, dockerConfigVolume)

		// Mount at ~/.docker/config.json (using /root/.docker for root user, or /home/runner/.docker for runner user)
		// Default to /root/.docker/config.json - can be overridden in RunnerTemplate if needed
		runnerContainer.VolumeMounts = append(runnerContainer.VolumeMounts,
			corev1.VolumeMount{
				Name:      "docker-config",
				MountPath: "/root/.docker",
				ReadOnly:  true,
			},
		)
	}

	// Set restart policy to Never if not set
	if podTemplate.Spec.RestartPolicy == "" {
		podTemplate.Spec.RestartPolicy = corev1.RestartPolicyNever
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: actRunner.Namespace,
			Labels: map[string]string{
				"forgejo.actions.io/job-id":    fmt.Sprintf("%d", actRunner.Spec.ForgejoJobID),
				"forgejo.actions.io/actrunner": actRunner.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: actRunner.APIVersion,
					Kind:       actRunner.Kind,
					Name:       actRunner.Name,
					UID:        actRunner.UID,
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
		Spec: podTemplate.Spec,
	}

	if err := ctrl.SetControllerReference(actRunner, pod, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, pod); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Pod already exists, get it and update status accordingly
			existingPod := &corev1.Pod{}
			if getErr := r.Get(ctx, client.ObjectKey{Namespace: actRunner.Namespace, Name: podName}, existingPod); getErr != nil {
				return fmt.Errorf("pod already exists but failed to get it: %w", getErr)
			}
			// Update status to reflect the existing pod
			actRunner.Status.KubernetesJobName = podName
			phase := r.determinePhase(existingPod)
			actRunner.Status.Phase = phase
			if phase == forgejoactionsiov1alpha1.ActRunnerPhaseRunning && actRunner.Status.StartedAt == nil {
				now := metav1.Now()
				actRunner.Status.StartedAt = &now
			}
			if err := r.Status().Update(ctx, actRunner); err != nil {
				return err
			}
			return nil
		}
		return err
	}

	// Update status
	actRunner.Status.KubernetesJobName = podName // Reusing this field name for Pod name
	actRunner.Status.Phase = forgejoactionsiov1alpha1.ActRunnerPhaseRunning
	now := metav1.Now()
	actRunner.Status.StartedAt = &now
	if err := r.Status().Update(ctx, actRunner); err != nil {
		return err
	}

	return nil
}

// cleanupRegistrationSecret deletes the registration token secret associated with the ActRunner
func (r *ActRunnerReconciler) cleanupRegistrationSecret(ctx context.Context, log logr.Logger, actRunner *forgejoactionsiov1alpha1.ActRunner) error {
	if actRunner.Spec.RegistrationTokenSecretRef.Name == "" {
		// No secret to clean up
		return nil
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      actRunner.Spec.RegistrationTokenSecretRef.Name,
			Namespace: actRunner.Namespace,
		},
	}

	if err := r.Delete(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			// Secret already deleted, nothing to do
			log.V(1).Info("registration secret already deleted", "secret", secret.Name)
			return nil
		}
		return fmt.Errorf("failed to delete registration secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	log.Info("deleted registration token secret", "secret", secret.Name, "actRunner", actRunner.Name)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActRunnerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&forgejoactionsiov1alpha1.ActRunner{}).
		Named("actrunner").
		Complete(r)
}
