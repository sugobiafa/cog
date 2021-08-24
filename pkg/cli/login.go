package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

type VerifyResponse struct {
	Username string `json:"username"`
}

func newLoginCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:        "login",
		SuggestFor: []string{"auth", "authenticate", "authorize"},
		Short:      "Log in to Replicate Docker registry",
		RunE:       login,
		Args:       cobra.MaximumNArgs(0),
	}

	cmd.Flags().String("registry", global.ReplicateRegistryHost, "Registry host")
	_ = cmd.Flags().MarkHidden("registry")

	return cmd
}

func login(cmd *cobra.Command, args []string) error {
	registryHost, err := cmd.Flags().GetString("registry")
	if err != nil {
		return err
	}
	url, err := getDisplayTokenURL(registryHost)
	if err != nil {
		return err
	}
	console.Infof("This command will authenticate Docker with Replicate's '%s' Docker registry. You will need a Replicate account.", registryHost)
	console.Info("")

	// TODO(bfirsh): if you have defined a registry in cog.yaml that is not r8.im, suggest to use 'docker login'

	console.Info("Hit enter to get started. A browser will open with an authentication token that you need to paste here.")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return err
	}

	console.Info("If it didn't open automatically, open this URL in a web browser:")
	console.Info(url)
	maybeOpenBrowser(url)

	console.Info("")
	console.Info("Once you've signed in, copy the authentication token from that web page, paste it here, then hit enter:")
	token, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)

	username, err := verifyToken(registryHost, token)
	if err != nil {
		return err
	}

	if err := docker.SaveLoginToken(registryHost, username, token); err != nil {
		return err
	}

	console.Infof("You've successfully authenticated as %s! You can now use the '%s' registry.", username, registryHost)

	return nil
}

func getDisplayTokenURL(registryHost string) (string, error) {
	resp, err := http.Get(addressWithScheme(registryHost) + "/cog/v1/display-token-url")
	if err != nil {
		return "", fmt.Errorf("Failed to log in to %s: %w", registryHost, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s is not the Replicate registry\nPlease log in using 'docker login'", registryHost)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned HTTP status %d", registryHost, resp.StatusCode)
	}
	body := &struct {
		URL string `json:"url"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", err
	}
	return body.URL, nil
}

func addressWithScheme(address string) string {
	if strings.Contains(address, "://") {
		return address
	}
	return "https://" + address
}

func maybeOpenBrowser(url string) {
	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	}
}

func verifyToken(registryHost string, token string) (username string, err error) {
	resp, err := http.PostForm(addressWithScheme(registryHost)+"/cog/v1/verify-token", url.Values{
		"token": []string{token},
	})
	if err != nil {
		return "", fmt.Errorf("Failed to verify token: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("User does not exist")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Failed to verify token, got status %d", resp.StatusCode)
	}
	body := &struct {
		Username string `json:"username"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return "", err
	}
	return body.Username, nil
}
