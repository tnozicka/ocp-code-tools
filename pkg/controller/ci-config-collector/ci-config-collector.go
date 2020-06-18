package route

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	gitplumbing "github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/tnozicka/ocp-code-tools/pkg/helpers"
	ciapi "github.com/tnozicka/ocp-code-tools/third_party/github.com/openshift/ci-tools/pkg/api"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	ControllerName = "ci-config-collector"
)

var (
	queueKey = "git"
)

type CIConfigController struct {
	gitRepo, gitRef  string
	configSubpath    string
	configFileRegexp *regexp.Regexp
	dataDir          string
	resyncEvery      time.Duration

	kubeClient kubernetes.Interface

	recorder record.EventRecorder

	queue workqueue.RateLimitingInterface
}

func NewCIConfigController(
	gitRepo, gitRef string,
	configSubpath string,
	configFileRegexp *regexp.Regexp,
	promotionNamespace string,
	dataDir string,
	resyncEvery time.Duration,
	kubeClient kubernetes.Interface,
) *CIConfigController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	cc := &CIConfigController{
		gitRepo:            gitRepo,
		gitRef:             gitRef,
		configSubpath:      configSubpath,
		configFileRegexp:   configFileRegexp,
		promotionNamespace: promotionNamespace,
		dataDir:            dataDir,
		resyncEvery:        resyncEvery,

		kubeClient: kubeClient,

		recorder: eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: ControllerName}),

		queue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	if len(cc.configSubpath) == 0 {
		cc.configSubpath = "./"
	}

	return cc
}

func (cc *CIConfigController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Started syncing key %q", key)
	defer func() {
		klog.V(4).Infof("Finished syncing key %q", key)
	}()

	klog.V(4).Infof("Cloning repository %q at revision %q into %q", cc.gitRepo, cc.gitRef, cc.dataDir)
	r, err := git.CloneContext(ctx, memory.NewStorage(), memfs.New(), &git.CloneOptions{
		URL: cc.gitRepo,
		// ReferenceName: gitplumbing.ReferenceName(cc.gitRef),
		Depth:        1,
		NoCheckout:   true,
		SingleBranch: true,
	})
	// r, err := git.PlainCloneContext(ctx, cc.dataDir, false, &git.CloneOptions{
	// 	URL:           cc.gitRepo,
	// 	ReferenceName: gitplumbing.ReferenceName(cc.gitRef),
	// 	Depth:         1,
	// 	SingleBranch:  true,
	// })
	// if err != nil {
	// 	return err
	// }
	klog.V(4).Infof("Cloning repository complete")

	ref, err := r.Head()
	if err != nil {
		return err
	}
	klog.V(5).Infof("HEAD is at %q", ref)

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	err = r.FetchContext(ctx, &git.FetchOptions{
		RemoteName: git.DefaultRemoteName,
		Force:      true,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}

	err = w.Checkout(&git.CheckoutOptions{
		Hash: gitplumbing.NewHash(cc.gitRef),
	})
	if err != nil {
		return err
	}

	ref, err = r.Head()
	if err != nil {
		return err
	}
	klog.V(5).Infof("New HEAD is at %q", ref)

	err = helpers.Walk(w.Filesystem, cc.configSubpath, func(path string, info os.FileInfo) error {
		if info.IsDir() {
			return nil
		}

		if !cc.configFileRegexp.MatchString(path) {
			return nil
		}

		klog.V(4).Infof("Processing file %q", path)

		f, err := w.Filesystem.Open(path)
		if err != nil {
			return fmt.Errorf("can't open file %q: %w", path, err)
		}
		defer f.Close()

		buffer := make([]byte, info.Size())
		// TODO: smarter read with retries (but it in memory fs so it shouldn't suffer from early reads due to interrupts)
		_, err = f.Read(buffer)
		ciConfig := &ciapi.ReleaseBuildConfiguration{}
		err = yaml.Unmarshal(buffer, ciConfig)
		if err != nil {
			// If the file contains unparsable data, log a warning and skip it
			klog.Warningf("Can't decode file %q, skipping.")
		}

		klog.V(5).Infof("ciConfig: %#v", ciConfig.Metadata)

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (cc *CIConfigController) processNextItem(ctx context.Context) bool {
	key, quit := cc.queue.Get()
	if quit {
		return false
	}
	defer cc.queue.Done(key)

	err := cc.sync(ctx, key.(string))
	if err == nil {
		cc.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("%v failed with : %v", key, err))
	cc.queue.AddRateLimited(key)

	return true
}

func (cc *CIConfigController) runWorker(ctx context.Context) {
	for cc.processNextItem(ctx) {
	}
}

func (cc *CIConfigController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	var wg sync.WaitGroup
	klog.Info("Starting CI config controller")
	defer func() {
		klog.Info("Shutting down CI config controller")
		cc.queue.ShutDown()
		wg.Wait()
		klog.Info("CI config controller shut down")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		wait.UntilWithContext(ctx, func(ctx context.Context) {
			cc.queue.Add(queueKey)
		}, cc.resyncEvery)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		wait.UntilWithContext(ctx, cc.runWorker, time.Second)
	}()

	<-ctx.Done()
}
