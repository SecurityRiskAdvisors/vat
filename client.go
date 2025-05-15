package vat

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/Khan/genqlient/graphql"
)

const API_PATH string = "/sra-purpletools-rest/graphql/"

type authTransport struct {
	key     string
	wrapped http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

	req.Header.Set("Authorization", "VEC1 "+t.key)
	resp, err := t.wrapped.RoundTrip(req)
	if err != nil {
		return resp, err
	}
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
	return resp, err

}

// TODO: make this just a hostanme
func SetupVectrClient(hostname, key string, insecureConnect bool) graphql.Client {
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
	url := url.URL{
		Host:   hostname,
		Scheme: "https",
		Path:   API_PATH,
	}
	return graphql.NewClient(url.String(), &httpClient)

}
