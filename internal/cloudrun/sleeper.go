// Package cloudrun lets intern put the Cloud Run service it runs in to
// sleep: manual scaling with zero instances, which stops all compute (and
// billing) until a person sets scaling back to automatic.
package cloudrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Sleeper scales its own Cloud Run service to zero. The service's runtime
// service account needs run.services.update on the service (for example
// roles/run.developer on it); without it Sleep returns the API's refusal.
type Sleeper struct {
	Service     string // K_SERVICE, set by Cloud Run
	HTTP        *http.Client
	MetadataURL string // the metadata server; tests point it elsewhere
	RunAPI      string // the Cloud Run Admin API v2
}

// FromEnv returns a Sleeper when running on Cloud Run.
func FromEnv() (*Sleeper, bool) {
	svc := os.Getenv("K_SERVICE")
	if svc == "" {
		return nil, false
	}
	return &Sleeper{Service: svc}, true
}

func (s *Sleeper) defaults() {
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if s.MetadataURL == "" {
		s.MetadataURL = "http://metadata.google.internal/computeMetadata/v1"
	}
	if s.RunAPI == "" {
		s.RunAPI = "https://run.googleapis.com/v2"
	}
}

func (s *Sleeper) metadata(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.MetadataURL+"/"+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("metadata %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata %s: HTTP %d", path, resp.StatusCode)
	}
	return strings.TrimSpace(string(body)), nil
}

// location returns the project ID and region the service runs in.
func (s *Sleeper) location(ctx context.Context) (string, string, error) {
	s.defaults()
	project, err := s.metadata(ctx, "project/project-id")
	if err != nil {
		return "", "", err
	}
	region, err := s.metadata(ctx, "instance/region") // projects/<number>/regions/<region>
	if err != nil {
		return "", "", err
	}
	return project, region[strings.LastIndex(region, "/")+1:], nil
}

// WakeCommand is the command that wakes the service again.
func (s *Sleeper) WakeCommand(ctx context.Context) string {
	project, region, err := s.location(ctx)
	if err != nil {
		return fmt.Sprintf("gcloud run services update %s --scaling=auto", s.Service)
	}
	return fmt.Sprintf("gcloud run services update %s --project %s --region %s --scaling=auto", s.Service, project, region)
}

// SleepCommand is the command a person can run to put the service to
// sleep when intern can't do it itself.
func (s *Sleeper) SleepCommand(ctx context.Context) string {
	return strings.Replace(s.WakeCommand(ctx), "--scaling=auto", "--scaling=0", 1)
}

// Sleep switches the service to manual scaling with zero instances. This
// process is shut down soon after.
func (s *Sleeper) Sleep(ctx context.Context) error {
	project, region, err := s.location(ctx)
	if err != nil {
		return err
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	raw, err := s.metadata(ctx, "instance/service-accounts/default/token")
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return fmt.Errorf("decode access token: %w", err)
	}
	url := fmt.Sprintf("%s/projects/%s/locations/%s/services/%s?updateMask=scaling", s.RunAPI, project, region, s.Service)
	body := []byte(`{"scaling":{"scalingMode":"MANUAL","manualInstanceCount":0}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("scale %s to zero: %w", s.Service, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Error.Message != "" {
			return fmt.Errorf("scale %s to zero: HTTP %d: %s", s.Service, resp.StatusCode, e.Error.Message)
		}
		return fmt.Errorf("scale %s to zero: HTTP %d", s.Service, resp.StatusCode)
	}
	return nil
}
