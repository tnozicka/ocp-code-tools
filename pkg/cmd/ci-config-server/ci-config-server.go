package ci_config_collector

import (
	"context"
	"flag"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tnozicka/ocp-code-tools/pkg/cmd/genericclioptions"
	cmdutil "github.com/tnozicka/ocp-code-tools/pkg/cmd/util"
	"github.com/tnozicka/ocp-code-tools/pkg/serving"
	"github.com/tnozicka/ocp-code-tools/pkg/signals"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

type Options struct {
	genericclioptions.IOStreams

	configPath    string
	staticDirPath string
}

func NewOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,

		configPath:    "",
		staticDirPath: "",
	}
}

func NewConfigCollectorServer(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	// Parent command to which all subcommands are added.
	rootCmd := &cobra.Command{
		Use:   "config-server",
		Short: "config-server is a controller for serving OpenShift release configuration and analysis",
		Long:  "config-server is a controller for serving OpenShift release configuration and analysis",
		RunE: func(cmd *cobra.Command, args []string) error {
			defer klog.Flush()

			err := o.Validate()
			if err != nil {
				return err
			}

			err = o.Complete()
			if err != nil {
				return err
			}

			err = o.Run(cmd, streams)
			if err != nil {
				return err
			}

			return nil
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.PersistentFlags().StringVarP(&o.configPath, "config", "", o.configPath, "Path to a file holding release configuration.")
	rootCmd.PersistentFlags().StringVarP(&o.staticDirPath, "static-dir", "", o.staticDirPath, "Path to the directory with static files.")

	cmdutil.InstallKlog(rootCmd)

	return rootCmd
}

func (o *Options) Validate() error {
	var errs []error

	if len(o.configPath) == 0 {
		errs = append(errs, fmt.Errorf("config path can't be empty"))
	}

	if len(o.staticDirPath) == 0 {
		errs = append(errs, fmt.Errorf("static dir path can't be empty"))
	}

	return errors.NewAggregate(errs)
}

func (o *Options) Complete() error {
	return nil
}

func (o *Options) Run(cmd *cobra.Command, streams genericclioptions.IOStreams) error {
	klog.Infof("loglevel is set to %q", cmdutil.GetLoglevel())

	stopCh := signals.StopChannel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stopCh
		cancel()
	}()

	return serving.RunServer(ctx, o.staticDirPath, o.configPath)
}
