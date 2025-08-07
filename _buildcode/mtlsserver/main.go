package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sra/vat/internal/util"
	"time"
)

type versionResponse struct {
	Code int `json:"code"`
	Data struct {
		CurrentVersion string `json:"currentVersion"`
		Error          string `json:"error"`
	} `json:"data"`
}

// generateCertsForTest creates a CA, a server certificate/key, and a client certificate/key for testing purposes.
func generateCertsForTest() (caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM []byte, err error) {
	// Create a new private key for the CA
	caPubKey, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to generate CA key: %w", err)
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
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, caPubKey, caKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Create a new private key for the server
	serverPubKey, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to generate server key: %w", err)
	}
	pkcs8Server, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to marshal server key: %w", err)
	}
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: pkcs8Server})

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
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTpl, caCert, serverPubKey, caKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create server certificate: %w", err)
	}
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})

	// Create a new private key for the client
	clientPubKey, clientKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to generate client key: %w", err)
	}
	pkcs8Client, err := x509.MarshalPKCS8PrivateKey(clientKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to marshal client key: %w", err)
	}
	clientKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Client})

	// Create the client certificate
	clientTpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test Client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTpl, caCert, clientPubKey, caKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create client certificate: %w", err)
	}
	clientCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})

	return caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM, nil
}

func main() {
	caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM, err := generateCertsForTest()
	if err != nil {
		fmt.Printf("Failed to generate certificates: %v\n", err)
		return
	}

	fmt.Println("Generated Client Certificate (client.crt):")
	fmt.Println(string(clientCertPEM))
	os.WriteFile("mtls-test-client.crt", clientCertPEM, 0600)
	fmt.Println("\nGenerated Client Key (client.key):")
	fmt.Println(string(clientKeyPEM))
	os.WriteFile("mtls-test-client.key", clientKeyPEM, 0600)
	fmt.Println("\nGenerated CA Certificate (ca.crt):")
	fmt.Println(string(caPEM))
	os.WriteFile("mtls-test-ca.crt", caPEM, 0600)

	// Create a CA pool for the server to verify client certificates
	clientCaPool := x509.NewCertPool()
	if !clientCaPool.AppendCertsFromPEM(caPEM) {
		fmt.Println("Failed to append CA cert to client CA pool.")
		return
	}

	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		fmt.Printf("Failed to load server certificate/key pair: %v\n", err)
		return
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert, // Require and verify client certificates
		ClientCAs:    clientCaPool,
	}

	mux := http.NewServeMux()

	mux.HandleFunc(util.VERSION_PATH, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != util.VERSION_PATH {
			// This should ideally not be reached if mux routes correctly, but as a safeguard.
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		response := versionResponse{
			Code: 200,
			Data: struct {
				CurrentVersion string `json:"currentVersion"`
				Error          string `json:"error"`
			}{
				CurrentVersion: "mtls-test",
				Error:          "",
			},
		}
		json.NewEncoder(w).Encode(response)
	})

	// All other paths return HTTP 500
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != util.VERSION_PATH {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	server := &http.Server{
		Addr:      ":8443", // Listen on port 8443
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	fmt.Println("Starting mTLS server on https://localhost:8443")
	fmt.Println("Use the generated client.crt and client.key for mTLS client authentication.")
	fmt.Println("Use the generated ca.crt as the CA certificate for client validation.")

	// ListenAndServeTLS with empty cert and key files uses the TLSConfig
	if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		fmt.Printf("Server failed to start: %v\n", err)
	}
}
