package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/internal/config"
	httpserver "github.com/perfect-panel/backend/internal/transport/http/server"
	"github.com/perfect-panel/backend/pkg/conf"
	"github.com/spf13/cobra"
)

var (
	healthcheckConfigPath string
	healthcheckTimeout    time.Duration
)

func init() {
	rootCmd.AddCommand(healthcheckCmd)
	healthcheckCmd.Flags().StringVar(&healthcheckConfigPath, "config", "etc/ppanel.yaml", "ppanel.yaml to read the listener address from")
	healthcheckCmd.Flags().DurationVar(&healthcheckTimeout, "timeout", 3*time.Second, "how long to wait for the answer")
}

// healthcheckCmd is the Docker HEALTHCHECK of the scratch image, which has
// no curl or wget: it asks the running server's liveness endpoint.
var healthcheckCmd = &cobra.Command{
	Use:   "healthcheck",
	Short: "check that the running server answers its liveness endpoint",
	Long: `healthcheck asks GET /healthz of the server the configuration file describes
and exits 0 when it answers 200 within the timeout, 1 otherwise. The address is
the file's Port on 127.0.0.1, or on Host when the server binds one interface.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return healthcheck(cmd.Context(), healthcheckConfigPath, healthcheckTimeout)
	},
}

// healthcheck reports whether the server described by the configuration
// file at configPath answers its liveness endpoint within timeout.
func healthcheck(ctx context.Context, configPath string, timeout time.Duration) error {
	var c config.File
	if err := conf.Load(configPath, &c); err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}
	target := healthURL(c)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: timeout}
	if c.TLS.Enable {
		// The certificate names the site, not the loopback address the
		// check connects to; whether the process answers is all that is
		// asked here.
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // loopback liveness check, see above
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	// The body is not read; closing it cannot lose data.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", target, response.Status)
	}
	return nil
}

// healthURL is the liveness endpoint of the server c describes: on
// 127.0.0.1 when the server binds every interface, on its Host otherwise.
func healthURL(c config.File) string {
	host := c.Host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	scheme := "http"
	if c.TLS.Enable {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(c.Port)) + httpserver.HealthzPath
}
