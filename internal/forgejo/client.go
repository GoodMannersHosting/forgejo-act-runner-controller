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

package forgejo

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Job represents a Forgejo Actions job from the API
type Job struct {
	ID      int64    `json:"id"`
	RepoID  int64    `json:"repo_id"`
	OwnerID int64    `json:"owner_id"`
	Name    string   `json:"name"`
	Needs   []string `json:"needs,omitempty"`
	RunsOn  []string `json:"runs_on"`
	TaskID  int64    `json:"task_id"`
	Status  string   `json:"status"`
}

// Client is a client for interacting with the Forgejo API
type Client struct {
	serverURL  string
	token      string
	httpClient *http.Client
}

// NewClient creates a new Forgejo API client
func NewClient(serverURL, token string) *Client {
	return NewClientWithTLS(serverURL, token, false)
}

// NewClientWithTLS creates a new Forgejo API client with TLS configuration
func NewClientWithTLS(serverURL, token string, skipTLSVerify bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipTLSVerify,
		},
	}

	return &Client{
		serverURL: serverURL,
		token:     token,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// GetPendingJobs fetches pending jobs from the Forgejo API for the specified organization and labels
func (c *Client) GetPendingJobs(ctx context.Context, org, labels string) ([]Job, error) {
	url := fmt.Sprintf("%s/api/v1/orgs/%s/actions/runners/jobs?labels=%s", c.serverURL, org, labels)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle null response
	if len(body) == 0 || string(body) == "null" {
		return []Job{}, nil
	}

	var jobs []Job
	if err := json.Unmarshal(body, &jobs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Filter for jobs with status "waiting"
	var waitingJobs []Job
	for _, job := range jobs {
		if job.Status == "waiting" {
			waitingJobs = append(waitingJobs, job)
		}
	}

	return waitingJobs, nil
}

// RegistrationTokenResponse represents the response from the registration token API
type RegistrationTokenResponse struct {
	Token string `json:"token"`
}

// GetRegistrationToken fetches a registration token for the specified organization
func (c *Client) GetRegistrationToken(ctx context.Context, org string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/orgs/%s/actions/runners/registration-token", c.serverURL, org)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var tokenResponse RegistrationTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if tokenResponse.Token == "" {
		return "", fmt.Errorf("registration token is empty in response")
	}

	return tokenResponse.Token, nil
}

// Repository represents a Forgejo repository
type Repository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

// GetRepository fetches repository information by ID from the organization
func (c *Client) GetRepository(ctx context.Context, org string, repoID int64) (*Repository, error) {
	url := fmt.Sprintf("%s/api/v1/orgs/%s/repos", c.serverURL, org)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var repos []Repository
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Find repository by ID
	for _, repo := range repos {
		if repo.ID == repoID {
			return &repo, nil
		}
	}

	return nil, fmt.Errorf("repository with ID %d not found", repoID)
}

// Run represents a Forgejo Actions run
type Run struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Repository  Repository `json:"repository"`
	WorkflowID  string     `json:"workflow_id"`
	PrettyRef   string     `json:"prettyref"`
	TriggerUser struct {
		Login string `json:"login"`
	} `json:"trigger_user"`
	TriggerEvent string `json:"trigger_event"`
	Status       string `json:"status"`
	HTMLURL      string `json:"html_url"`
}

// GetRun fetches run information by ID from a repository
func (c *Client) GetRun(ctx context.Context, owner, repo string, runID int64) (*Run, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/actions/runs/%d", c.serverURL, owner, repo, runID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", c.token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var run Run
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &run, nil
}
