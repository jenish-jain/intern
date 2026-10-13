package cloudrun

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeGoogle(t *testing.T, runStatus int, runBody string) (*Sleeper, *[]string) {
	t.Helper()
	var patches []string
	mux := http.NewServeMux()
	meta := map[string]string{
		"/meta/project/project-id":                      "realm-asgard",
		"/meta/instance/region":                         "projects/815704181749/regions/asia-south1",
		"/meta/instance/service-accounts/default/token": `{"access_token":"tok","expires_in":3599,"token_type":"Bearer"}`,
	}
	for path, body := range meta {
		body := body
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Metadata-Flavor") != "Google" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, body)
		})
	}
	mux.HandleFunc("/run/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		patches = append(patches, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+" "+r.Header.Get("Authorization")+" "+string(b))
		w.WriteHeader(runStatus)
		_, _ = io.WriteString(w, runBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Sleeper{Service: "ai-intern-agent", MetadataURL: srv.URL + "/meta", RunAPI: srv.URL + "/run"}, &patches
}

func TestSleep_ScalesOwnServiceToZero(t *testing.T) {
	s, patches := fakeGoogle(t, 200, `{"name":"op"}`)
	if err := s.Sleep(context.Background()); err != nil {
		t.Fatalf("Sleep: %v", err)
	}
	want := "PATCH /run/projects/realm-asgard/locations/asia-south1/services/ai-intern-agent?updateMask=scaling Bearer tok " +
		`{"scaling":{"scalingMode":"MANUAL","manualInstanceCount":0}}`
	if len(*patches) != 1 || (*patches)[0] != want {
		t.Errorf("request = %q\nwant %q", *patches, want)
	}
	if got := s.WakeCommand(context.Background()); got != "gcloud run services update ai-intern-agent --project realm-asgard --region asia-south1 --scaling=auto" {
		t.Errorf("WakeCommand = %q", got)
	}
	if got := s.SleepCommand(context.Background()); !strings.HasSuffix(got, "--scaling=0") {
		t.Errorf("SleepCommand = %q", got)
	}
}

func TestSleep_SurfacesPermissionError(t *testing.T) {
	s, _ := fakeGoogle(t, 403, `{"error":{"code":403,"message":"Permission 'run.services.update' denied"}}`)
	err := s.Sleep(context.Background())
	if err == nil || !strings.Contains(err.Error(), "run.services.update") {
		t.Errorf("want the API's permission error, got %v", err)
	}
}
