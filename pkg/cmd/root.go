package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	cobra.OnInitialize(initConfig)
}

func logIfError(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func makeRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tekton-ci",
		Short: "Tekton CI",
	}
	cmd.AddCommand(makeHTTPCmd())
	cmd.AddCommand(makeConvertCmd())
	return cmd
}

func initConfig() {
	viper.AutomaticEnv()
}

// Execute is the main entry point into this component.
func Execute() {
	if err := makeRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}
