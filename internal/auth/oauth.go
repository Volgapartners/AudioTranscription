// Package auth provides Google OAuth2 authentication for CLI applications.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"

	"transcription-cli/internal/logging"
)

// Authenticate performs the OAuth2 flow and returns an authenticated HTTP client.
// It tries to load a cached token first; if unavailable or expired, it opens the browser.
func Authenticate(ctx context.Context, credentialsPath, tokenCachePath string) (*http.Client, error) {
	slog.Info("loading OAuth credentials", logging.FieldPath, credentialsPath)

	b, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("reading credentials file %s: %w", credentialsPath, err)
	}

	config, err := google.ConfigFromJSON(b, drive.DriveScope)
	if err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}

	// Try cached token first
	token, err := loadToken(tokenCachePath)
	if err == nil && token.Valid() {
		slog.Info("using cached OAuth token")
		return config.Client(ctx, token), nil
	}

	// Try refreshing if we have a refresh token
	if err == nil && token.RefreshToken != "" {
		slog.Info("refreshing expired OAuth token")
		src := config.TokenSource(ctx, token)
		newToken, refreshErr := src.Token()
		if refreshErr == nil {
			saveToken(tokenCachePath, newToken)
			return config.Client(ctx, newToken), nil
		}
		slog.Warn("token refresh failed, re-authenticating", logging.FieldError, refreshErr)
	}

	// Full OAuth flow
	token, err = doOAuthFlow(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("OAuth flow: %w", err)
	}

	saveToken(tokenCachePath, token)
	return config.Client(ctx, token), nil
}

// doOAuthFlow runs the full browser-based OAuth2 flow with PKCE.
func doOAuthFlow(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Allocate a free port for the callback server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("allocating port for callback: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", port)

	// Generate PKCE verifier and CSRF state
	verifier := oauth2.GenerateVerifier()
	state := generateState()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("invalid state parameter (possible CSRF)")
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("OAuth denied: %s", errMsg)
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code in callback")
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family:sans-serif;text-align:center;margin-top:80px">
			<h1 style="color:#22c55e">&#10003; Authenticated</h1>
			<p>You can close this window and return to the terminal.</p>
			</body></html>`)
		codeCh <- code
	})

	server := &http.Server{Handler: mux}
	go func() {
		if serveErr := server.Serve(listener); serveErr != http.ErrServerClosed {
			errCh <- serveErr
		}
	}()

	authURL := config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
	)

	fmt.Fprintf(os.Stderr, "Opening browser for Google login...\n")
	fmt.Fprintf(os.Stderr, "If the browser does not open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	// Wait for callback
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		_ = server.Shutdown(ctx)
		return nil, err
	case <-time.After(3 * time.Minute):
		_ = server.Shutdown(ctx)
		return nil, fmt.Errorf("timed out waiting for authorization (3 minutes)")
	case <-ctx.Done():
		_ = server.Shutdown(ctx)
		return nil, ctx.Err()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)

	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}

	return token, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}

func generateState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func loadToken(path string) (*oauth2.Token, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tok oauth2.Token
	if err := json.NewDecoder(f).Decode(&tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func saveToken(path string, token *oauth2.Token) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		slog.Warn("unable to cache OAuth token", logging.FieldPath, path, logging.FieldError, err)
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(token); err != nil {
		slog.Warn("unable to write OAuth token", logging.FieldError, err)
	}
}
