package util

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/Khan/genqlient/graphql"
)

const API_PATH string = "/sra-purpletools-rest/graphql/"
const VERSION_PATH string = "/sra-purpletools-rest/update/versionCheck"

type versionResponse struct {
	Code int `json:"code"`
	Data struct {
		CurrentVersion string `json:"currentVersion"`
		Error          string `json:"error"`
	} `json:"data"`
}

type VectrVersionHandler struct {
	httpClient  http.Client
	versionPath url.URL
}

var ErrInvalidAuth = errors.New("credentials invalid")

func (v *VectrVersionHandler) Get(ctx context.Context) (string, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.versionPath.String(), nil)
	if err != nil {
		return "", fmt.Errorf("could not create method: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not complete request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrInvalidAuth
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected response: %d", resp.StatusCode)
	}

	var parsedResponse versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsedResponse); err != nil {
		return "", fmt.Errorf("could not parse response: %w", err)
	}

	return parsedResponse.Data.CurrentVersion, nil
}

type authTransport struct {
	key     string
	wrapped http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			panic(err)
		}
		if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
			fmt.Fprintf(os.Stderr, "Request Method: %s; URL: %s; Header: %#v\n", req.Method, req.URL, req.Header)
			fmt.Fprintln(os.Stderr, string(body))
		}
		//slog.Debug("request body", "body", string(body))
		req.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	req.Header.Set("Authorization", "VEC1 "+t.key)
	resp, err := t.wrapped.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.Body != nil {
		respbody, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp, err
		}
		if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
			fmt.Fprintf(os.Stderr, "Response Code: %d; Status: %s, Header: %#v\n", resp.StatusCode, resp.Status, resp.Header)
			fmt.Fprintln(os.Stderr, string(respbody))
		}
		//slog.Debug("response body", "body", string(respbody))
		resp.Body = io.NopCloser(bytes.NewBuffer(respbody))
	}
	return resp, err

}

func SetupVectrClient(hostname, key string, insecureConnect bool) (graphql.Client, *VectrVersionHandler) {
	slog.Info("Setting up connection to VECTR", "url", hostname)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureConnect {
		slog.Warn("Ignoring cert errors to VECTR", "url", hostname)
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient := http.Client{
		Transport: &authTransport{
			key:     key,
			wrapped: transport,
		},
	}
	u := url.URL{
		Host:   hostname,
		Scheme: "https",
		Path:   API_PATH,
	}

	v := &VectrVersionHandler{
		httpClient: httpClient,
		versionPath: url.URL{
			Host:   hostname,
			Scheme: "https",
			Path:   VERSION_PATH,
		},
	}

	return graphql.NewClient(u.String(), &httpClient), v

}
