package util

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
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

type CustomTlsParams struct {
	ClientKeyFile  []byte
	ClientCertFile []byte
	CaCertFiles    [][]byte
}

// VectrVersionHandler manages HTTP requests to retrieve the current version of the VECTR application.
//
// Fields:
//   - httpClient: An HTTP client used to perform requests.
//   - versionPath: URL for the version check endpoint.
type VectrVersionHandler struct {
	httpClient  http.Client
	versionPath url.URL
}

var ErrInvalidAuth = errors.New("credentials invalid")

// Get retrieves the current version of the VECTR application.
//
// This function performs an HTTP GET request to the version check endpoint.
// It handles authentication and parses the response to extract the current version.
//
// Parameters:
//   - ctx: Context for managing request deadlines, cancellations, and other request-scoped values.
//
// Returns:
//   - A string representing the current version of the VECTR application.
//   - An error if the request or response parsing fails.
//
// Errors:
//   - Returns `ErrInvalidAuth` if the response status is unauthorized.
//   - Returns an error if the request cannot be completed or the response cannot be parsed.
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

// authTransport is a custom HTTP transport that adds authentication headers to requests.
//
// Fields:
//   - key: The authentication key used for VECTR API requests.
//   - wrapped: The underlying HTTP RoundTripper to be wrapped.
type authTransport struct {
	key     string
	wrapped http.RoundTripper
}

// RoundTrip executes a single HTTP transaction, adding authentication headers to the request.
//
// This method reads the request body, adds the authorization header, and logs request and response details if debugging is enabled.
//
// Parameters:
//   - req: The HTTP request to be executed.
//
// Returns:
//   - A pointer to an HTTP response.
//   - An error if the request execution or response reading fails.
//
// Errors:
//   - Returns an error if the request body cannot be read or the response body cannot be processed.
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

// SetupVectrClient initializes a GraphQL client and a VectrVersionHandler for interacting with the VECTR API.
//
// This function configures the HTTP client with authentication and optional insecure connection settings.
// It sets up the URL for API requests and version checks.
//
// Parameters:
//   - hostname: The hostname of the VECTR instance.
//   - key: The authentication key for API requests.
//   - insecureConnect: A boolean indicating whether to ignore TLS certificate errors.
//   - tlsParams: A struct containing custom TLS configuration byte slices for certs and keys.
//
// Returns:
//   - A GraphQL client configured for API requests.
//   - A VectrVersionHandler for version checks.
func SetupVectrClient(hostname, key string, insecureConnect bool, tlsParams *CustomTlsParams) (graphql.Client, *VectrVersionHandler) {
	slog.Info("Setting up connection to VECTR", "url", hostname)
	transport := http.DefaultTransport.(*http.Transport).Clone()

	tlsConfig := &tls.Config{}
	tlsConfigured := false

	if len(tlsParams.ClientCertFile) > 0 && len(tlsParams.ClientKeyFile) > 0 {
		cert, err := tls.X509KeyPair(tlsParams.ClientCertFile, tlsParams.ClientKeyFile)
		if err != nil {
			slog.Error("Failed to load client certificate/key pair", "error", err)
			os.Exit(1)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		tlsConfigured = true
	}

	if insecureConnect {
		slog.Warn("Ignoring cert errors to VECTR", "url", hostname)
		tlsConfig.InsecureSkipVerify = true
		tlsConfigured = true
	} else if len(tlsParams.CaCertFiles) > 0 {
		caCertPool := x509.NewCertPool()
		for _, caCert := range tlsParams.CaCertFiles {
			if !caCertPool.AppendCertsFromPEM(caCert) {
				slog.Error("Failed to append CA certificate from PEM")
				os.Exit(1)
			}
		}
		tlsConfig.RootCAs = caCertPool
		tlsConfigured = true
	}

	if tlsConfigured {
		transport.TLSClientConfig = tlsConfig
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
