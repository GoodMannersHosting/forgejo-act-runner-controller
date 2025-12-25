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

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	forgejoactionsiov1alpha1 "github.com/goodmannershosting/forgejo-act-runner-controller/api/v1alpha1"
	"github.com/goodmannershosting/forgejo-act-runner-controller/internal/forgejo"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(forgejoactionsiov1alpha1.AddToScheme(scheme))
}

func main() {
	// Helper to get value from env or use default
	getEnvOrEmpty := func(key string) string {
		if val := os.Getenv(key); val != "" {
			return val
		}
		return ""
	}
	getEnvOrDefault := func(key, defaultValue string) string {
		if val := os.Getenv(key); val != "" {
			return val
		}
		return defaultValue
	}

	getEnvOrBool := func(key string, defaultValue bool) bool {
		if val := os.Getenv(key); val != "" {
			return val == "true" || val == "1" || val == "yes"
		}
		return defaultValue
	}

	var (
		forgejoServer     = flag.String("forgejo-server", getEnvOrEmpty("FORGEJO_SERVER"), "Forgejo server URL (required, can also be set via FORGEJO_SERVER env var)")
		organization      = flag.String("organization", getEnvOrEmpty("ORGANIZATION"), "Forgejo organization name (required, can also be set via ORGANIZATION env var)")
		labels            = flag.String("labels", getEnvOrEmpty("LABELS"), "Label filter for jobs (required, can also be set via LABELS env var)")
		tokenSecretName   = flag.String("token-secret-name", getEnvOrEmpty("TOKEN_SECRET_NAME"), "Name of the secret containing the token (required, can also be set via TOKEN_SECRET_NAME env var)")
		tokenSecretKey    = flag.String("token-secret-key", getEnvOrDefault("TOKEN_SECRET_KEY", "token"), "Key in the secret containing the token (can also be set via TOKEN_SECRET_KEY env var)")
		namespace         = flag.String("namespace", getEnvOrEmpty("NAMESPACE"), "Kubernetes namespace (required, can also be set via NAMESPACE env var)")
		actDeploymentName = flag.String("act-deployment-name", getEnvOrEmpty("ACT_DEPLOYMENT_NAME"), "Name of the ActDeployment resource (required, can also be set via ACT_DEPLOYMENT_NAME env var)")
		skipTLSVerify     = flag.Bool("skip-tls-verify", getEnvOrBool("SKIP_TLS_VERIFY", false), "Skip TLS certificate verification (can also be set via SKIP_TLS_VERIFY env var)")
	)

	// Handle poll-interval separately since it's a duration
	pollIntervalStr := getEnvOrDefault("POLL_INTERVAL", "10s")
	pollIntervalDefault, err := time.ParseDuration(pollIntervalStr)
	if err != nil {
		pollIntervalDefault = 10 * time.Second
	}
	pollIntervalFlag := flag.Duration("poll-interval", pollIntervalDefault, "Polling interval (can also be set via POLL_INTERVAL env var)")

	flag.Parse()

	// Use the flag value (which may have been overridden from env var or command line)
	pollInterval := *pollIntervalFlag

	// Set up logger
	zapLog, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
	logger := zapr.NewLogger(zapLog)

	if *forgejoServer == "" || *organization == "" || *labels == "" || *tokenSecretName == "" || *namespace == "" || *actDeploymentName == "" {
		logger.Error(fmt.Errorf("missing required flags"), "missing required flags")
		flag.Usage()
		os.Exit(1)
	}

	// Create Kubernetes client
	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		logger.Error(err, "failed to create Kubernetes client")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown for SIGINT (Ctrl-C) and SIGTERM
	// Note: SIGKILL (signal 9) cannot be caught and will force-kill the process
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start signal handler in background
	go func() {
		sig := <-sigChan
		logger.Info("received signal, initiating graceful shutdown", "signal", sig.String())
		cancel()
	}()

	// Run the listener
	if err := runListener(ctx, logger, k8sClient, *forgejoServer, *organization, *labels, *tokenSecretName, *tokenSecretKey, *namespace, *actDeploymentName, pollInterval, *skipTLSVerify); err != nil {
		// Check if error is due to context cancellation (graceful shutdown)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Info("listener stopped gracefully")
			return
		}
		logger.Error(err, "listener failed")
		os.Exit(1)
	}

	logger.Info("listener stopped")
}

func runListener(ctx context.Context, logger logr.Logger, k8sClient client.Client, forgejoServer, organization, labels, tokenSecretName, tokenSecretKey, namespace, actDeploymentName string, pollInterval time.Duration, skipTLSVerify bool) error {
	// Load token from secret (with retries)
	token, err := loadTokenWithRetry(ctx, logger, k8sClient, namespace, tokenSecretName, tokenSecretKey)
	if err != nil {
		// Don't wrap context cancellation errors
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("failed to load token: %w", err)
	}

	// Create Forgejo client
	forgejoClient := forgejo.NewClientWithTLS(forgejoServer, token, skipTLSVerify)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.Info("starting listener", "server", forgejoServer, "org", organization, "labels", labels, "interval", pollInterval)
	logger.Info("connected successfully", "server", forgejoServer, "org", organization)

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutdown requested, stopping listener")
			return nil
		case <-ticker.C:
			// Reload ActDeployment on each poll to pick up changes (e.g., runnerImage updates)
			actDeployment, err := loadActDeployment(ctx, logger, k8sClient, namespace, actDeploymentName)
			if err != nil {
				logger.Error(err, "failed to load ActDeployment, skipping poll")
				continue
			}

			// Update existing ActRunner resources if ActDeployment spec has changed
			if err := updateExistingActRunners(ctx, logger, k8sClient, namespace, actDeployment); err != nil {
				logger.Error(err, "failed to update existing ActRunners")
				// Continue anyway - we can still create new ones
			}

			if err := pollAndCreateActRunners(ctx, logger, k8sClient, forgejoClient, organization, labels, namespace, actDeployment); err != nil {
				// Don't log errors if context was cancelled
				if ctx.Err() != nil {
					return nil
				}
				logger.Error(err, "error polling or creating ActRunners")
			}
		}
	}
}

func loadTokenWithRetry(ctx context.Context, logger logr.Logger, k8sClient client.Client, namespace, secretName, key string) (string, error) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second
	loggedWaiting := false

	for {
		token, err := loadToken(ctx, k8sClient, namespace, secretName, key)
		if err == nil {
			if loggedWaiting {
				logger.Info("secret found", "secret", secretName, "namespace", namespace)
			}
			return token, nil
		}

		// Only retry on NotFound errors - other errors (key missing, empty token) are fatal
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("failed to load token from secret %s/%s: %w", namespace, secretName, err)
		}

		// Log once when starting to wait, then silently retry
		if !loggedWaiting {
			logger.Info("secret not found, waiting for it to be created...", "secret", secretName, "namespace", namespace)
			loggedWaiting = true
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func loadActDeployment(ctx context.Context, logger logr.Logger, k8sClient client.Client, namespace, actDeploymentName string) (*forgejoactionsiov1alpha1.ActDeployment, error) {
	actDeployment := &forgejoactionsiov1alpha1.ActDeployment{}
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: actDeploymentName}, actDeployment)
	if err != nil {
		return nil, fmt.Errorf("failed to get ActDeployment %s/%s: %w", namespace, actDeploymentName, err)
	}
	return actDeployment, nil
}

func loadActDeploymentWithRetry(ctx context.Context, logger logr.Logger, k8sClient client.Client, namespace, actDeploymentName string) (*forgejoactionsiov1alpha1.ActDeployment, error) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second
	loggedWaiting := false

	for {
		actDeployment, err := loadActDeployment(ctx, logger, k8sClient, namespace, actDeploymentName)
		if err == nil {
			if loggedWaiting {
				logger.Info("ActDeployment found", "name", actDeploymentName, "namespace", namespace)
			}
			return actDeployment, nil
		}

		// Only retry on NotFound errors - other errors (permissions, etc.) should fail fast
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		// Log once when starting to wait, then silently retry
		if !loggedWaiting {
			logger.Info("ActDeployment not found, waiting for it to be created...", "name", actDeploymentName, "namespace", namespace)
			loggedWaiting = true
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// updateExistingActRunners updates existing ActRunner resources when ActDeployment spec changes
// This ensures that pending/running runners get updated with new configuration (e.g., runnerImage)
func updateExistingActRunners(ctx context.Context, logger logr.Logger, k8sClient client.Client, namespace string, actDeployment *forgejoactionsiov1alpha1.ActDeployment) error {
	// List all ActRunners owned by this ActDeployment
	actRunners := &forgejoactionsiov1alpha1.ActRunnerList{}
	if err := k8sClient.List(ctx, actRunners, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list ActRunners: %w", err)
	}

	updatedCount := 0
	for i := range actRunners.Items {
		ar := &actRunners.Items[i]

		// Check if this ActRunner is owned by the ActDeployment
		isOwned := false
		for _, ownerRef := range ar.OwnerReferences {
			if ownerRef.Kind == "ActDeployment" && ownerRef.Name == actDeployment.Name && ownerRef.UID == actDeployment.UID {
				isOwned = true
				break
			}
		}
		if !isOwned {
			continue
		}

		// Check if spec needs updating
		needsUpdate := false
		if ar.Spec.RunnerImage != actDeployment.Spec.RunnerImage {
			ar.Spec.RunnerImage = actDeployment.Spec.RunnerImage
			needsUpdate = true
		}
		if ar.Spec.DockerInDockerImage != actDeployment.Spec.DockerInDockerImage {
			ar.Spec.DockerInDockerImage = actDeployment.Spec.DockerInDockerImage
			needsUpdate = true
		}
		// Update DockerConfigMapRef if changed (compare pointers)
		if (ar.Spec.DockerConfigMapRef == nil) != (actDeployment.Spec.DockerConfigMapRef == nil) ||
			(ar.Spec.DockerConfigMapRef != nil && actDeployment.Spec.DockerConfigMapRef != nil &&
				ar.Spec.DockerConfigMapRef.Name != actDeployment.Spec.DockerConfigMapRef.Name) {
			ar.Spec.DockerConfigMapRef = actDeployment.Spec.DockerConfigMapRef
			needsUpdate = true
		}

		// For Pending runners (no pod created yet), also update JobTemplate to ensure they get latest RunnerTemplate
		// This ensures pending runners pick up any changes to RunnerTemplate (e.g., dnsPolicy, hostAliases, etc.)
		isPending := ar.Status.Phase == forgejoactionsiov1alpha1.ActRunnerPhasePending || ar.Status.KubernetesJobName == ""
		if isPending {
			// Update JobTemplate from RunnerTemplate for pending runners
			// This ensures they get the latest configuration even if other fields didn't change
			ar.Spec.JobTemplate = *actDeployment.Spec.RunnerTemplate.DeepCopy()
			needsUpdate = true
		}

		if needsUpdate {
			// For Pending runners, update the spec and the controller will create pods with new config
			// For Running/Completed runners, we skip updates (they should finish with current configuration)
			if isPending {
				logger.Info("updating ActRunner spec", "actRunner", ar.Name, "phase", ar.Status.Phase, "runnerImage", actDeployment.Spec.RunnerImage)
				if err := k8sClient.Update(ctx, ar); err != nil {
					logger.Error(err, "failed to update ActRunner", "actRunner", ar.Name)
					continue
				}
				updatedCount++
			} else {
				logger.V(1).Info("skipping ActRunner update (pod already exists)", "actRunner", ar.Name, "phase", ar.Status.Phase, "pod", ar.Status.KubernetesJobName)
			}
		}
	}

	if updatedCount > 0 {
		logger.Info("updated ActRunner resources", "count", updatedCount)
	}

	return nil
}

func loadToken(ctx context.Context, k8sClient client.Client, namespace, secretName, key string) (string, error) {
	secret := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret); err != nil {
		return "", err
	}

	tokenBytes, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %s not found in secret %s/%s", key, namespace, secretName)
	}

	if len(tokenBytes) == 0 {
		return "", fmt.Errorf("token key %s in secret %s/%s is empty", key, namespace, secretName)
	}

	return string(tokenBytes), nil
}

func pollAndCreateActRunners(ctx context.Context, logger logr.Logger, k8sClient client.Client, forgejoClient *forgejo.Client, organization, labels, namespace string, actDeployment *forgejoactionsiov1alpha1.ActDeployment) error {
	// Poll Forgejo for pending jobs
	jobs, err := forgejoClient.GetPendingJobs(ctx, organization, labels)
	if err != nil {
		return fmt.Errorf("failed to get pending jobs: %w", err)
	}

	logger.V(1).Info("polled Forgejo", "jobCount", len(jobs))

	// Get all existing ActRunners in the namespace to check limits
	existingActRunners := &forgejoactionsiov1alpha1.ActRunnerList{}
	if err := k8sClient.List(ctx, existingActRunners, client.InNamespace(namespace)); err != nil {
		logger.Error(err, "failed to list ActRunners")
		return fmt.Errorf("failed to list ActRunners: %w", err)
	}

	// Count ActRunners owned by this ActDeployment
	var actDeploymentOwnedRunners []forgejoactionsiov1alpha1.ActRunner
	for _, ar := range existingActRunners.Items {
		for _, ownerRef := range ar.OwnerReferences {
			if ownerRef.Kind == "ActDeployment" && ownerRef.Name == actDeployment.Name && ownerRef.UID == actDeployment.UID {
				actDeploymentOwnedRunners = append(actDeploymentOwnedRunners, ar)
				break
			}
		}
	}
	currentRunnerCount := int32(len(actDeploymentOwnedRunners))

	// Check MaxRunners limit
	maxRunners := int32(0) // 0 means unlimited
	if actDeployment.Spec.MaxRunners != nil && *actDeployment.Spec.MaxRunners > 0 {
		maxRunners = *actDeployment.Spec.MaxRunners
	}

	for _, job := range jobs {
		// Check if ActRunner for this job ID already exists
		found := false
		for _, ar := range actDeploymentOwnedRunners {
			if ar.Spec.ForgejoJobID == job.ID {
				logger.V(1).Info("ActRunner already exists for job", "jobID", job.ID, "actRunner", ar.Name)
				found = true
				break
			}
		}

		if found {
			// Found existing ActRunner, skip
			continue
		}

		// Check MaxRunners limit before creating (re-check in case we've created runners in this loop)
		if maxRunners > 0 && currentRunnerCount >= maxRunners {
			logger.V(1).Info("maximum runner count reached, skipping remaining jobs", "currentCount", currentRunnerCount, "maxRunners", maxRunners)
			break
		}

		// Log that we detected a pending job that needs a runner
		logger.Info("detected pending job requiring runner", "jobID", job.ID, "jobName", job.Name, "repoID", job.RepoID)

		// Fetch repository information (non-blocking - continue even if it fails)
		var repo *forgejo.Repository
		var run *forgejo.Run
		repo, repoErr := forgejoClient.GetRepository(ctx, organization, job.RepoID)
		if repoErr != nil {
			logger.Error(repoErr, "failed to get repository", "jobID", job.ID, "repoID", job.RepoID)
		} else {
			// Parse repository full_name to get owner and repo name
			// full_name format is "owner/repo"
			owner := organization // Default to organization
			repoName := repo.Name
			if parts := strings.Split(repo.FullName, "/"); len(parts) == 2 {
				owner = parts[0]
				repoName = parts[1]
			}

			// Fetch run information (job ID should correspond to run ID)
			var runErr error
			run, runErr = forgejoClient.GetRun(ctx, owner, repoName, job.ID)
			if runErr != nil {
				logger.Error(runErr, "failed to get run details", "jobID", job.ID, "owner", owner, "repo", repoName)
				// Continue anyway - we'll just have empty status fields
			}
		}

		// Fetch registration token for this runner
		registrationToken, err := forgejoClient.GetRegistrationToken(ctx, organization)
		if err != nil {
			logger.Error(err, "failed to get registration token", "jobID", job.ID)
			continue
		}

		// Generate a unique secret name with random component
		randomBytes := make([]byte, 4)
		if _, err := rand.Read(randomBytes); err != nil {
			logger.Error(err, "failed to generate random bytes for secret name", "jobID", job.ID)
			continue
		}
		randomSuffix := hex.EncodeToString(randomBytes)
		registrationSecretName := fmt.Sprintf("actrunner-reg-%d-%s", job.ID, randomSuffix)
		if len(registrationSecretName) > 63 {
			registrationSecretName = registrationSecretName[:63]
		}

		// Create or update the secret (handle already exists gracefully)
		registrationSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      registrationSecretName,
				Namespace: namespace,
				Labels: map[string]string{
					"forgejo.actions.io/job-id":             fmt.Sprintf("%d", job.ID),
					"forgejo.actions.io/registration-token": "true",
				},
			},
			Data: map[string][]byte{
				"token": []byte(registrationToken),
			},
		}

		createErr := k8sClient.Create(ctx, registrationSecret)
		if createErr != nil && apierrors.IsAlreadyExists(createErr) {
			// Secret already exists, update it with new token
			existingSecret := &corev1.Secret{}
			if getErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: registrationSecretName}, existingSecret); getErr != nil {
				logger.Error(getErr, "failed to get existing registration token secret", "jobID", job.ID, "secretName", registrationSecretName)
				continue
			}
			existingSecret.Data = registrationSecret.Data
			if updateErr := k8sClient.Update(ctx, existingSecret); updateErr != nil {
				logger.Error(updateErr, "failed to update registration token secret", "jobID", job.ID, "secretName", registrationSecretName)
				continue
			}
			logger.Info("updated existing registration token secret", "jobID", job.ID, "secretName", registrationSecretName)
		} else if createErr != nil {
			logger.Error(createErr, "failed to create registration token secret", "jobID", job.ID)
			continue
		} else {
			logger.Info("created registration token secret", "jobID", job.ID, "secretName", registrationSecretName)
		}

		// Get proper API version and kind for OwnerReference
		apiVersion := actDeployment.APIVersion
		if apiVersion == "" {
			apiVersion = forgejoactionsiov1alpha1.GroupVersion.String()
		}
		kind := actDeployment.Kind
		if kind == "" {
			kind = "ActDeployment"
		}

		// Ensure JobTemplate has at least one container
		jobTemplate := actDeployment.Spec.RunnerTemplate.DeepCopy()
		if len(jobTemplate.Spec.Containers) == 0 {
			jobTemplate.Spec.Containers = []corev1.Container{
				{
					Name:  "runner",
					Image: "runner-image:latest", // Should be set by user in RunnerTemplate
				},
			}
		}

		// Create new ActRunner
		actRunner := &forgejoactionsiov1alpha1.ActRunner{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("actrunner-%d-%s", job.ID, generateShortHash(job.ID)),
				Namespace: namespace,
				Labels: map[string]string{
					"forgejo.actions.io/job-id": fmt.Sprintf("%d", job.ID),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: apiVersion,
						Kind:       kind,
						Name:       actDeployment.Name,
						UID:        actDeployment.UID,
						Controller: func() *bool { b := true; return &b }(),
					},
				},
			},
			Spec: forgejoactionsiov1alpha1.ActRunnerSpec{
				ForgejoJobID:   job.ID,
				ForgejoServer:  actDeployment.Spec.ForgejoServer,
				Organization:   actDeployment.Spec.Organization,
				TokenSecretRef: actDeployment.Spec.TokenSecretRef,
				RegistrationTokenSecretRef: corev1.SecretReference{
					Name:      registrationSecretName,
					Namespace: namespace,
				},
				RunnerImage:         actDeployment.Spec.RunnerImage,
				DockerInDockerImage: actDeployment.Spec.DockerInDockerImage,
				DockerConfigMapRef:  actDeployment.Spec.DockerConfigMapRef,
				JobData: forgejoactionsiov1alpha1.JobData{
					ID:      job.ID,
					RepoID:  job.RepoID,
					OwnerID: job.OwnerID,
					Name:    job.Name,
					Needs:   job.Needs,
					RunsOn:  job.RunsOn,
					TaskID:  job.TaskID,
					Status:  job.Status,
				},
				JobTemplate: *jobTemplate,
			},
			Status: forgejoactionsiov1alpha1.ActRunnerStatus{
				Phase: forgejoactionsiov1alpha1.ActRunnerPhasePending,
			},
		}

		// Set repository and run information in status if available
		if repo != nil {
			actRunner.Status.RepositoryFullName = repo.FullName
		}
		if run != nil {
			actRunner.Status.TriggerUser = run.TriggerUser.Login
			actRunner.Status.PrettyRef = run.PrettyRef
			actRunner.Status.TriggerEvent = run.TriggerEvent
		}

		if err := k8sClient.Create(ctx, actRunner); err != nil {
			logger.Error(err, "failed to create ActRunner", "jobID", job.ID)
			continue
		}

		// Update status with repository and run information
		if repo != nil || run != nil {
			if err := k8sClient.Status().Update(ctx, actRunner); err != nil {
				logger.Error(err, "failed to update ActRunner status", "jobID", job.ID)
				// Continue - this is not critical
			}
		}

		logger.Info("created ActRunner", "jobID", job.ID, "actRunner", actRunner.Name, "currentRunnerCount", currentRunnerCount+1, "maxRunners", maxRunners)

		// Increment count for next iteration
		currentRunnerCount++
	}

	return nil
}

func generateShortHash(id int64) string {
	// Simple hash function to generate a short identifier
	hash := id % 10000
	if hash < 0 {
		hash = -hash
	}
	return fmt.Sprintf("%04d", hash)
}
