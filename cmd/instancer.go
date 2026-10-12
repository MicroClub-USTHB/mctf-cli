package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
	"github.com/MicroClub-USTHB/mctf-cli/internal/instancer"
	"github.com/MicroClub-USTHB/mctf-cli/internal/plugin"
)

var instancerCmd = &cobra.Command{
	Use:   "instancer",
	Short: "Run the per-team challenge instancer daemon (local dev; deprecated)",
	Long: "Runs the built-in, single-host instancer daemon.\n\n" +
		"DEPRECATED: for production, run the dedicated MCTF Instancer service\n" +
		"(github.com/MicroClub-USTHB/mctf-instancer) and point the CLI at it with\n" +
		"instancer.url in mctf.config.yml — `mctf instance list` / `destroy` then\n" +
		"drive it over its admin API. This built-in daemon remains for offline\n" +
		"local development and will be removed in a future release.",
}

var (
	flagServeAddr     string
	flagServeInterval time.Duration
	flagServeQuiet    bool
)

var instancerServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the instancer HTTP daemon",
	Long: "Starts an HTTP daemon that exposes the spawn / destroy / extend\n" +
		"endpoints used by the platform integration. A background\n" +
		"reaper goroutine cleans up expired instances on every tick.\n\n" +
		"Auth: bearer tokens read from MCTF_INSTANCER_TOKEN (one or more,\n" +
		"comma-separated). The platform integration includes one of these\n" +
		"in `Authorization: Bearer <token>` headers.\n\n" +
		"Routes:\n" +
		"  GET  /healthz                     no-auth liveness probe\n" +
		"  GET  /instances                   list every live spawn\n" +
		"  GET  /instances/{id}              fetch one\n" +
		"  POST /spawn                       {team_id, challenge_slug, flag?}\n" +
		"  POST /destroy/{id}                tear down + remove from store\n" +
		"  POST /extend/{id}                 {by_seconds: N} (default 900)\n\n" +
		"Examples:\n" +
		"  export MCTF_INSTANCER_TOKEN=my-secret\n" +
		"  mctf instancer serve --addr :8080\n" +
		"  mctf instancer serve --addr 127.0.0.1:8080 --reap-interval 10s",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(flagDir)
		if err != nil {
			return err
		}
		challs, _, err := challenge.Discover(flagDir, cfg.ChallengesDir)
		if err != nil {
			return err
		}

		tokens := splitTokens(os.Getenv("MCTF_INSTANCER_TOKEN"))
		if len(tokens) == 0 {
			return errors.New("MCTF_INSTANCER_TOKEN is empty — set at least one bearer token before serving")
		}

		abs, _ := filepath.Abs(flagDir)
		store, err := instancer.NewStore(filepath.Join(abs, ".mctf", "instances.json"))
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		pm := plugin.Load(ctx, cfg.Plugins, plugin.Options{HostVersion: version, Dir: flagDir})
		defer pm.Close()

		var runner instancer.Runner
		if flagServeQuiet {
			runner = instancer.NewQuietDockerRunner()
		} else {
			runner = instancer.NewDockerRunner()
		}

		srv := instancer.NewServer(cfg, store, runner, challs, instancer.Options{
			Tokens:        tokens,
			PluginManager: pm,
		})

		// Build the catalog for the reaper.
		idx := map[string]*challenge.Challenge{}
		for _, c := range challs {
			idx[c.CategoryDir+"/"+c.Slug] = c
		}
		reaper := &instancer.Reaper{
			Store:    store,
			Runner:   runner,
			ChalIdx:  idx,
			Interval: flagServeInterval,
			Logger:   os.Stdout,
		}

		go func() {
			if err := reaper.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "reaper exited: %v\n", err)
			}
		}()

		httpSrv := &http.Server{Addr: flagServeAddr, Handler: srv.Handler()}
		errCh := make(chan error, 1)
		go func() {
			fmt.Printf("instancer listening on %s (reap every %s, %d challenge(s))\n",
				flagServeAddr, reaper.Interval, len(challs))
			err := httpSrv.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
				return
			}
			errCh <- nil
		}()

		select {
		case <-ctx.Done():
			fmt.Println("\nshutdown signal received")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("graceful shutdown: %w", err)
			}
			return nil
		case err := <-errCh:
			return err
		}
	},
}

// splitTokens parses MCTF_INSTANCER_TOKEN into a list. Commas separate
// multiple tokens so operators can rotate without a restart.
func splitTokens(raw string) []string {
	var out []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func init() {
	instancerServeCmd.Flags().StringVar(&flagServeAddr, "addr", ":8080", "address to listen on")
	instancerServeCmd.Flags().DurationVar(&flagServeInterval, "reap-interval", 30*time.Second, "how often to reap expired instances")
	instancerServeCmd.Flags().BoolVar(&flagServeQuiet, "quiet", false, "discard docker compose stdout/stderr (recommended in production)")
	instancerCmd.AddCommand(instancerServeCmd)
	rootCmd.AddCommand(instancerCmd)
}
