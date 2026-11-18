package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	authEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint = "https://oauth2.googleapis.com/token"
	driveScope    = "https://www.googleapis.com/auth/drive"
)

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type tokenError struct {
	Code        string
	Description string
}

func (e *tokenError) Error() string {
	return fmt.Sprintf("token request failed: %s %s", e.Code, e.Description)
}

type callback struct {
	code string
	err  error
}

func login(ctx context.Context) error {
	fmt.Print(`drivesync signs in with your own Google OAuth client, so no third party holds a key to your Drive.
If you don't have one yet (about 5 minutes, free):

  1. https://console.cloud.google.com/projectcreate         create a project
  2. https://console.cloud.google.com/apis/library/drive.googleapis.com   enable the Google Drive API
  3. https://console.cloud.google.com/auth/branding         set an app name, Audience: External, then Publish app
  4. https://console.cloud.google.com/auth/clients/create   type "Desktop app", copy the ID and secret

`)
	input := bufio.NewReader(os.Stdin)
	clientID, err := prompt(input, "OAuth client ID: ")
	if err != nil {
		return err
	}
	clientSecret, err := prompt(input, "OAuth client secret: ")
	if err != nil {
		return err
	}

	verifier := randomToken()
	sum := sha256.Sum256([]byte(verifier))
	state := randomToken()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/", port)

	params := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {driveScope},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	link := authEndpoint + "?" + params.Encode()
	fmt.Printf("\nSign in with Google in your browser:\n\n%s\n\n", link)
	if os.Getenv("SSH_CONNECTION") != "" {
		fmt.Printf("Over SSH, forward the callback port from the machine with the browser first:\n  ssh -L %d:127.0.0.1:%d <this host>\n\n", port, port)
	} else {
		openBrowser(link)
	}

	pending, err := prepareCredentialStore()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			pending.abort()
		}
	}()
	if err := enterSandbox(sandboxPolicy{Read: systemReadPaths(), ConnectPorts: internetPorts, AllowSignals: true}); err != nil {
		return err
	}

	results := make(chan callback, 1)
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			query := r.URL.Query()
			var result callback
			switch {
			case query.Get("state") != state:
				result.err = errors.New("state mismatch in OAuth callback")
			case query.Get("error") != "":
				result.err = fmt.Errorf("sign-in refused: %s", query.Get("error"))
			case query.Get("code") == "":
				result.err = errors.New("OAuth callback without a code")
			default:
				result.code = query.Get("code")
			}
			if result.err != nil {
				http.Error(w, result.err.Error(), http.StatusBadRequest)
			} else {
				fmt.Fprintln(w, "drivesync is signed in. You can close this tab.")
			}
			select {
			case results <- result:
			default:
			}
		}),
	}
	go server.Serve(listener)
	defer server.Close()

	var result callback
	select {
	case result = <-results:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Minute):
		return errors.New("timed out waiting for sign-in")
	}
	if result.err != nil {
		return result.err
	}

	token, err := postToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {result.code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if err != nil {
		return err
	}
	if token.RefreshToken == "" {
		return errors.New("Google returned no refresh token")
	}
	if err := pending.commit(credentials{ClientID: clientID, ClientSecret: clientSecret, RefreshToken: token.RefreshToken}); err != nil {
		committed = true
		return err
	}
	committed = true
	fmt.Println("Signed in. Next: drivesync folders")
	return nil
}

func logout() error {
	if err := clearCredentials(); err != nil {
		return err
	}
	fmt.Println("Signed out. To revoke access fully, remove drivesync at https://myaccount.google.com/connections")
	return nil
}

func prompt(input *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	line, err := input.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if err == nil || err == io.EOF {
			return "", fmt.Errorf("%sempty", label)
		}
		return "", err
	}
	return line, nil
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func postToken(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var token tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		return nil, fmt.Errorf("token response (%s): %w", resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK || token.Error != "" {
		return nil, &tokenError{Code: token.Error, Description: token.ErrorDescription}
	}
	return &token, nil
}

type tokenSource struct {
	mu          sync.Mutex
	creds       credentials
	accessToken string
	expiry      time.Time
}

func (t *tokenSource) token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.accessToken != "" && time.Until(t.expiry) > time.Minute {
		return t.accessToken, nil
	}
	token, err := postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.creds.RefreshToken},
		"client_id":     {t.creds.ClientID},
		"client_secret": {t.creds.ClientSecret},
	})
	if err != nil {
		var tokenErr *tokenError
		if errors.As(err, &tokenErr) && tokenErr.Code == "invalid_grant" {
			return "", errors.New("Google rejected the saved sign-in (expired or revoked); run: drivesync login")
		}
		return "", err
	}
	t.accessToken = token.AccessToken
	t.expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return t.accessToken, nil
}
