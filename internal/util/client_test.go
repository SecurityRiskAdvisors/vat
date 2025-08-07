package util

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// generateCertsForTest creates a CA, a server certificate/key, and a client certificate/key for testing purposes.
func generateCertsForTest(t *testing.T) (caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM []byte) {
	// Create a new private key for the CA
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	// Create the CA certificate
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA certificate: %v", err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	// Create a new private key for the server
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate server key: %v", err)
	}
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	// Create the server certificate
	serverTpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTpl, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create server certificate: %v", err)
	}
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})

	// Create a new private key for the client
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate client key: %v", err)
	}
	clientKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})

	// Create the client certificate
	clientTpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test Client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTpl, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create client certificate: %v", err)
	}
	clientCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})

	return caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM
}

func TestVectrVersionHandler(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	// Generate all certificates needed for tests
	caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM := generateCertsForTest(t)

	const correctAuthKey = "my-secret-key"
	const correctVersion = "v99.9.9"
	expectedAuthHeader := fmt.Sprintf("VEC1 %s", correctAuthKey)

	// Create a test handler that mimics the VECTR version endpoint
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != VERSION_PATH {
			http.NotFound(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != expectedAuthHeader {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// For the client cert test, explicitly check for peer certs
		if r.URL.Query().Get("require_cert") == "true" {
			if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, "Client certificate required but not provided", http.StatusForbidden)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		response := versionResponse{
			Code: 200,
			Data: struct {
				CurrentVersion string `json:"currentVersion"`
				Error          string `json:"error"`
			}{
				CurrentVersion: correctVersion,
			},
		}
		json.NewEncoder(w).Encode(response)
	})

	// Create a CA pool that the test server can use to verify client certs
	clientCAPool := x509.NewCertPool()
	if !clientCAPool.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to append CA cert to pool")
	}

	// Create and start the TLS test server
	server := httptest.NewUnstartedServer(handler)
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("failed to create server key pair: %v", err)
	}
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven, // Use this to allow tests without client certs to run
		ClientCAs:    clientCAPool,
	}
	server.StartTLS()
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse server URL: %v", err)
	}

	testCases := []struct {
		name          string
		authKey       string
		tlsParams     CustomTlsParams
		expectErr     bool
		errContains   string
		expectVersion string
	}{
		{
			name:          "Success with valid CA",
			authKey:       correctAuthKey,
			tlsParams:     CustomTlsParams{CaCertFiles: [][]byte{caPEM}},
			expectErr:     false,
			expectVersion: correctVersion,
		},
		{
			name:          "Success with InsecureSkipVerify",
			authKey:       correctAuthKey,
			tlsParams:     CustomTlsParams{InsecureConnect: true},
			expectErr:     false,
			expectVersion: correctVersion,
		},
		{
			name:        "Failure due to untrusted CA",
			authKey:     correctAuthKey,
			tlsParams:   CustomTlsParams{}, // No CA provided
			expectErr:   true,
			errContains: "unknown authority",
		},
		{
			name:        "Failure due to bad auth",
			authKey:     "bad-key",
			tlsParams:   CustomTlsParams{CaCertFiles: [][]byte{caPEM}},
			expectErr:   true,
			errContains: ErrInvalidAuth.Error(),
		},
		{
			name:    "Success with ClientCert",
			authKey: correctAuthKey,
			tlsParams: CustomTlsParams{
				ClientCertFile: clientCertPEM,
				ClientKeyFile:  clientKeyPEM,
				CaCertFiles:    [][]byte{caPEM},
			},
			expectErr:     false,
			expectVersion: correctVersion,
		},
		{
			name:    "Failure when ClientCert required but not provided",
			authKey: correctAuthKey,
			tlsParams: CustomTlsParams{
				CaCertFiles: [][]byte{caPEM},
			},
			expectErr:   true,
			errContains: "unexpected response: 403",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, versionHandler, err := SetupVectrClient(serverURL.Host, tc.authKey, &tc.tlsParams)
			if err != nil {
				t.Fatalf("did not expect an error but got one: %s", err)
			}

			// The httptest server gives a URL with an IP, so we need to fix the version handler's path
			// to use the correct hostname for the request, otherwise TLS validation fails on hostname mismatch.
			versionHandler.versionPath.Host = net.JoinHostPort("localhost", serverURL.Port())

			if strings.Contains(tc.name, "ClientCert") {
				q := versionHandler.versionPath.Query()
				q.Set("require_cert", "true")
				versionHandler.versionPath.RawQuery = q.Encode()
			}

			version, err := versionHandler.Get(context.Background())

			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected an error, but got nil")
				}
				if tc.errContains != "" {
					if !strings.Contains(err.Error(), tc.errContains) && !errors.Is(err, ErrInvalidAuth) {
						t.Errorf("expected error to contain %q, but got: %v", tc.errContains, err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, but got: %v", err)
				}
				if tc.expectVersion != version {
					t.Errorf("expected version %q, but got %q", tc.expectVersion, version)
				}
			}
		})
	}
}
