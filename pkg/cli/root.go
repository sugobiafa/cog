package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/global"
)

func NewRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:     "cog",
		Short:   "",
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/keepsake/main.go
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBuildCommand(),
		newInferCommand(),
		newServerCommand(),
	)

	log.SetLevel(log.DebugLevel)

	return &rootCmd, nil
}

func setPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().BoolVarP(&global.Verbose, "verbose", "v", false, "Verbose output")

}
