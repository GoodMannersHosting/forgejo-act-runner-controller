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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	forgejoactionsiov1alpha1 "github.com/goodmannershosting/forgejo-act-runner-controller/api/v1alpha1"
)

// ActDeploymentReconciler reconciles an ActDeployment object
type ActDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=forgejo.actions.io,resources=actdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=forgejo.actions.io,resources=actdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=forgejo.actions.io,resources=actdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=forgejo.actions.io,resources=actrunners,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ActDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("reconciling ActDeployment", "name", req.NamespacedName)

	actDeployment := &forgejoactionsiov1alpha1.ActDeployment{}
	if err := r.Get(ctx, req.NamespacedName, actDeployment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("found ActDeployment", "name", actDeployment.Name, "namespace", actDeployment.Namespace)

	// Handle deletion
	if !actDeployment.DeletionTimestamp.IsZero() {
		// Cleanup is handled by owner references on the Deployment
		return ctrl.Result{}, nil
	}

	// Get or create ServiceAccount for listener
	log.Info("reconciling ServiceAccount for listener")
	serviceAccount, err := r.reconcileServiceAccount(ctx, actDeployment)
	if err != nil {
		log.Error(err, "failed to reconcile ServiceAccount")
		return ctrl.Result{}, err
	}
	log.Info("ServiceAccount ready", "name", serviceAccount.Name)

	// Create or update Role and RoleBinding for listener
	log.Info("reconciling Role and RoleBinding for listener")
	if err := r.reconcileListenerRBAC(ctx, actDeployment, serviceAccount.Name); err != nil {
		log.Error(err, "failed to reconcile listener RBAC")
		return ctrl.Result{}, err
	}
	log.Info("listener RBAC ready")

	// Create or update listener Deployment
	log.Info("reconciling listener Deployment")
	deployment, err := r.reconcileListenerDeployment(ctx, actDeployment, serviceAccount.Name)
	if err != nil {
		log.Error(err, "failed to reconcile listener deployment")
		return ctrl.Result{}, err
	}
	log.Info("listener Deployment ready", "name", deployment.Name)

	// Count active ActRunners
	activeCount, err := r.countActiveActRunners(ctx, actDeployment)
	if err != nil {
		log.Error(err, "failed to count active ActRunners")
	} else {
		actDeployment.Status.ActiveActRunners = activeCount
	}

	// Update status
	actDeployment.Status.ListenerPodName = fmt.Sprintf("%s-0", deployment.Name) // Assuming single replica
	actDeployment.Status.ObservedGeneration = actDeployment.Generation
	if err := r.Status().Update(ctx, actDeployment); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ActDeploymentReconciler) reconcileServiceAccount(ctx context.Context, actDeployment *forgejoactionsiov1alpha1.ActDeployment) (*corev1.ServiceAccount, error) {
	serviceAccountName := fmt.Sprintf("%s-listener", actDeployment.Name)
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: actDeployment.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: actDeployment.APIVersion,
					Kind:       actDeployment.Kind,
					Name:       actDeployment.Name,
					UID:        actDeployment.UID,
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(actDeployment, serviceAccount, r.Scheme); err != nil {
		return nil, err
	}

	existing := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Namespace: actDeployment.Namespace, Name: serviceAccountName}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create
			if err := r.Create(ctx, serviceAccount); err != nil {
				return nil, err
			}
			return serviceAccount, nil
		}
		return nil, err
	}

	return existing, nil
}

func (r *ActDeploymentReconciler) reconcileListenerRBAC(ctx context.Context, actDeployment *forgejoactionsiov1alpha1.ActDeployment, serviceAccountName string) error {
	roleName := fmt.Sprintf("%s-listener", actDeployment.Name)
	namespace := actDeployment.Namespace

	// Create Role with necessary permissions for the listener
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: actDeployment.APIVersion,
					Kind:       actDeployment.Kind,
					Name:       actDeployment.Name,
					UID:        actDeployment.UID,
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "create"},
			},
			{
				APIGroups: []string{"forgejo.actions.io"},
				Resources: []string{"actdeployments"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"forgejo.actions.io"},
				Resources: []string{"actrunners"},
				Verbs:     []string{"create", "get", "list", "watch", "update", "patch"},
			},
		},
	}

	if err := ctrl.SetControllerReference(actDeployment, role, r.Scheme); err != nil {
		return err
	}

	existingRole := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: roleName}, existingRole)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create
			if err := r.Create(ctx, role); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		// Update if needed
		existingRole.Rules = role.Rules
		if err := r.Update(ctx, existingRole); err != nil {
			return err
		}
	}

	// Create RoleBinding
	roleBindingName := roleName
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: actDeployment.APIVersion,
					Kind:       actDeployment.Kind,
					Name:       actDeployment.Name,
					UID:        actDeployment.UID,
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: namespace,
			},
		},
	}

	if err := ctrl.SetControllerReference(actDeployment, roleBinding, r.Scheme); err != nil {
		return err
	}

	existingRoleBinding := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: roleBindingName}, existingRoleBinding)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create
			if err := r.Create(ctx, roleBinding); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		// Update if needed
		existingRoleBinding.RoleRef = roleBinding.RoleRef
		existingRoleBinding.Subjects = roleBinding.Subjects
		if err := r.Update(ctx, existingRoleBinding); err != nil {
			return err
		}
	}

	return nil
}

func (r *ActDeploymentReconciler) reconcileListenerDeployment(ctx context.Context, actDeployment *forgejoactionsiov1alpha1.ActDeployment, serviceAccountName string) (*appsv1.Deployment, error) {
	deploymentName := fmt.Sprintf("%s-listener", actDeployment.Name)
	pollInterval := 10 * time.Second
	if actDeployment.Spec.PollInterval != nil {
		pollInterval = actDeployment.Spec.PollInterval.Duration
	}

	// Build pod template from spec or use defaults
	podTemplate := actDeployment.Spec.ListenerTemplate.DeepCopy()
	if podTemplate.Labels == nil {
		podTemplate.Labels = make(map[string]string)
	}
	podTemplate.Labels["app"] = "forgejo-listener"
	podTemplate.Labels["forgejo.actions.io/act-deployment"] = actDeployment.Name

	// Set default container if not specified
	if len(podTemplate.Spec.Containers) == 0 {
		// TODO: Set appropriate listener image (should be the controller image with listener binary)
		podTemplate.Spec.Containers = []corev1.Container{
			{
				Name:    "listener",
				Image:   "controller-image:latest", // Should be set to controller image
				Command: []string{"/listener"},
			},
		}
	}

	// Set environment variables
	container := &podTemplate.Spec.Containers[0]
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  "FORGEJO_SERVER",
			Value: actDeployment.Spec.ForgejoServer,
		},
		corev1.EnvVar{
			Name:  "ORGANIZATION",
			Value: actDeployment.Spec.Organization,
		},
		corev1.EnvVar{
			Name:  "LABELS",
			Value: actDeployment.Spec.Labels,
		},
		corev1.EnvVar{
			Name:  "TOKEN_SECRET_NAME",
			Value: actDeployment.Spec.TokenSecretRef.Name,
		},
		corev1.EnvVar{
			Name:  "TOKEN_SECRET_KEY",
			Value: "token", // Default key name in the secret
		},
		corev1.EnvVar{
			Name:  "NAMESPACE",
			Value: actDeployment.Namespace,
		},
		corev1.EnvVar{
			Name:  "ACT_DEPLOYMENT_NAME",
			Value: actDeployment.Name,
		},
		corev1.EnvVar{
			Name:  "POLL_INTERVAL",
			Value: pollInterval.String(),
		},
	)

	podTemplate.Spec.ServiceAccountName = serviceAccountName

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: actDeployment.Namespace,
			Labels: map[string]string{
				"app":                               "forgejo-listener",
				"forgejo.actions.io/act-deployment": actDeployment.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: actDeployment.APIVersion,
					Kind:       actDeployment.Kind,
					Name:       actDeployment.Name,
					UID:        actDeployment.UID,
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: func() *int32 { i := int32(1); return &i }(),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                               "forgejo-listener",
					"forgejo.actions.io/act-deployment": actDeployment.Name,
				},
			},
			Template: *podTemplate,
		},
	}

	if err := ctrl.SetControllerReference(actDeployment, deployment, r.Scheme); err != nil {
		return nil, err
	}

	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Namespace: actDeployment.Namespace, Name: deploymentName}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Create
			if err := r.Create(ctx, deployment); err != nil {
				return nil, err
			}
			return deployment, nil
		}
		return nil, err
	}

	// Update if needed
	existing.Spec = deployment.Spec
	if err := r.Update(ctx, existing); err != nil {
		return nil, err
	}

	return existing, nil
}

func (r *ActDeploymentReconciler) countActiveActRunners(ctx context.Context, actDeployment *forgejoactionsiov1alpha1.ActDeployment) (int32, error) {
	actRunners := &forgejoactionsiov1alpha1.ActRunnerList{}
	if err := r.List(ctx, actRunners, client.InNamespace(actDeployment.Namespace)); err != nil {
		return 0, err
	}

	count := int32(0)
	for _, ar := range actRunners.Items {
		// Check if owned by this ActDeployment
		for _, ref := range ar.OwnerReferences {
			if ref.UID == actDeployment.UID {
				// Count as active if not succeeded or failed
				if ar.Status.Phase != forgejoactionsiov1alpha1.ActRunnerPhaseSucceeded &&
					ar.Status.Phase != forgejoactionsiov1alpha1.ActRunnerPhaseFailed {
					count++
				}
				break
			}
		}
	}

	return count, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&forgejoactionsiov1alpha1.ActDeployment{}).
		Named("actdeployment").
		Complete(r)
}
