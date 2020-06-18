package ci_config_collector

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"regexp"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/tnozicka/ocp-code-tools/pkg/cmd/genericclioptions"
	cmdutil "github.com/tnozicka/ocp-code-tools/pkg/cmd/util"
	route "github.com/tnozicka/ocp-code-tools/pkg/controller/ci-config-collector"
	"github.com/tnozicka/ocp-code-tools/pkg/signals"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type Options struct {
	genericclioptions.IOStreams

	GitURL, GitRef         string
	ConfigSubpath          string
	configFileRegexpString string
	ConfigFileRegexp       *regexp.Regexp
	PromotionNamespace     string
	DataDir                string
	ResyncEvery            time.Duration

	Kubeconfig          string
	ControllerNamespace string
	ConfigMapName       string

	restConfig *restclient.Config
	kubeClient kubernetes.Interface
}

func NewOptions(streams genericclioptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,

		GitURL:                 "https://github.com/openshift/release",
		GitRef:                 "master",
		ConfigSubpath:          "ci-operator/config/openshift",
		configFileRegexpString: `.*\.yaml`,
		PromotionNamespace:     "ocp",
		DataDir:                "",
		ResyncEvery:            15 * time.Minute,

		Kubeconfig:          "",
		ControllerNamespace: "",
		ConfigMapName:       "ci-config",
	}
}

func NewConfigCollectorCommand(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	// Parent command to which all subcommands are added.
	rootCmd := &cobra.Command{
		Use:   "config-collector",
		Short: "config-collector is a controller for collecting OpenShift release configs and storing structured data in a ConfigMap",
		Long:  "config-collector is a controller for collecting OpenShift release configs and storing structured data in a ConfigMap",
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

	rootCmd.PersistentFlags().StringVarP(&o.GitURL, "git-url", "", o.GitURL, "Repository URL containing the release configs.")
	rootCmd.PersistentFlags().StringVarP(&o.GitRef, "git-ref", "", o.GitRef, "Repository reference (master, <sha>, ...).")
	rootCmd.PersistentFlags().StringVarP(&o.ConfigSubpath, "config-subpath", "", o.ConfigSubpath, "Location in the repository where to search for configs.")
	rootCmd.PersistentFlags().StringVarP(&o.configFileRegexpString, "config-regexp", "", o.configFileRegexpString, "Regular expression used to match config files.")
	rootCmd.PersistentFlags().StringVarP(&o.PromotionNamespace, "promotion-namespace", "", o.PromotionNamespace, "Accept only configs promoting to this namespace.")
	rootCmd.PersistentFlags().StringVarP(&o.DataDir, "data-dir", "", o.DataDir, "Directory used to store temporary data.")
	rootCmd.PersistentFlags().DurationVarP(&o.ResyncEvery, "resync-every", "", o.ResyncEvery, "Interval to resync the source git repository.")

	rootCmd.PersistentFlags().StringVarP(&o.Kubeconfig, "kubeconfig", "", o.Kubeconfig, "Path to the kubeconfig file")
	rootCmd.PersistentFlags().StringVarP(&o.ControllerNamespace, "controller-namespace", "", o.ControllerNamespace, "Namespace where the controller is running. Autodetected if run inside a cluster.")
	rootCmd.PersistentFlags().StringVarP(&o.ConfigMapName, "configmap-name", "", o.ConfigMapName, "Name of the ConfigMap to store the result into.")

	cmdutil.InstallKlog(rootCmd)

	return rootCmd
}

func (o *Options) Validate() error {
	var errs []error

	// TODO

	return errors.NewAggregate(errs)
}

func (o *Options) Complete() error {
	var err error

	o.ConfigFileRegexp, err = regexp.Compile(o.configFileRegexpString)
	if err != nil {
		return err
	}

	if len(o.Kubeconfig) != 0 {
		klog.V(1).Infof("Using kubeconfig %q.", o.Kubeconfig)
		o.restConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: o.Kubeconfig}, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return fmt.Errorf("can't create config from kubeConfigPath %q: %w", o.Kubeconfig, err)
		}
	} else {
		klog.V(1).Infof("No kubeconfig specified, using InClusterConfig.")
		o.restConfig, err = restclient.InClusterConfig()
		if err != nil {
			return fmt.Errorf("can't create InClusterConfig: %w", err)
		}
	}

	if len(o.ControllerNamespace) == 0 {
		// Autodetect if running inside a cluster
		bytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			return fmt.Errorf("can't autodetect controller namespace: %w", err)
		}
		o.ControllerNamespace = string(bytes)
	}

	o.kubeClient, err = kubernetes.NewForConfig(o.restConfig)
	if err != nil {
		return fmt.Errorf("can't build kubernetes clientset: %w", err)
	}

	return nil
}

func (o *Options) Run(cmd *cobra.Command, streams genericclioptions.IOStreams) error {
	stopCh := signals.StopChannel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stopCh
		cancel()
	}()

	klog.Infof("loglevel is set to %q", cmdutil.GetLoglevel())

	cc := route.NewCIConfigController(
		o.GitURL,
		o.GitRef,
		o.ConfigSubpath,
		o.ConfigFileRegexp,
		o.PromotionNamespace,
		o.DataDir,
		o.ResyncEvery,
		o.kubeClient,
	)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		cc.Run(ctx)
	}()

	<-ctx.Done()

	wg.Wait()

	return nil
}
