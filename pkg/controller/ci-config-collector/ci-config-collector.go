package route

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	gitplumbing "github.com/go-git/go-git/v5/plumbing"
	"github.com/tnozicka/ocp-code-tools/pkg/api"
	"github.com/tnozicka/ocp-code-tools/pkg/gittools"
	"github.com/tnozicka/ocp-code-tools/pkg/helpers"
	ciapi "github.com/tnozicka/ocp-code-tools/third_party/github.com/openshift/ci-tools/pkg/api"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	queueKey       = "git"
)

type CIConfigController struct {
	gitURL, gitRef       string
	configSubpath        string
	configmapName        string
	controllerNamespace  string
	allowedStreamsRegexp *regexp.Regexp
	configFileRegexp     *regexp.Regexp
	resyncEvery          time.Duration

	repoCache *gittools.RepoCache

	kubeClient kubernetes.Interface

	recorder record.EventRecorder

	queue workqueue.RateLimitingInterface
}

func NewCIConfigController(
	cacheDir string,
	gitURL, gitRef string,
	configSubpath string,
	configmapName string,
	controllerNamespace string,
	allowedStreamsRegexp *regexp.Regexp,
	configFileRegexp *regexp.Regexp,
	resyncEvery time.Duration,
	kubeClient kubernetes.Interface,
) *CIConfigController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	cc := &CIConfigController{
		gitURL:               gitURL,
		gitRef:               gitRef,
		configSubpath:        configSubpath,
		configmapName:        configmapName,
		controllerNamespace:  controllerNamespace,
		allowedStreamsRegexp: allowedStreamsRegexp,
		configFileRegexp:     configFileRegexp,
		resyncEvery:          resyncEvery,

		repoCache: gittools.NewRepoCache(cacheDir),

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
	select {
	case <-ctx.Done():
		return nil // shutting down
	default:
	}

	klog.V(4).Infof("Started syncing key %q", key)
	defer func() {
		klog.V(4).Infof("Finished syncing key %q", key)
	}()

	r, err := cc.repoCache.OpenOrClone(ctx, cc.gitURL)
	if err != nil {
		return fmt.Errorf("can't get repository %q: %w", cc.gitURL, err)
	}

	reference := "refs/remotes/" + git.DefaultRemoteName + "/" + cc.gitRef

	oldResolvedRef, err := r.Reference(gitplumbing.ReferenceName(reference), true)
	if err != nil {
		return fmt.Errorf("can't resolve reference %q in repository %q: %v", reference, cc.gitURL, err)
	}
	oldHash := oldResolvedRef.Hash()

	err = r.FetchContext(ctx, &git.FetchOptions{
		RemoteName: git.DefaultRemoteName,
		Force:      true,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", cc.gitRef, git.DefaultRemoteName, cc.gitRef)),
		},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("can't fetch reference %q in repository %q: %w", cc.gitRef, cc.gitURL, err)
	}

	resolvedRef, err := r.Reference(gitplumbing.ReferenceName(reference), true)
	if err != nil {
		return fmt.Errorf("can't resolve reference %q in repository %q: %v", reference, cc.gitURL, err)
	}
	hash := resolvedRef.Hash()

	klog.V(4).Infof("%q: resolved revision %q to hash %q", cc.gitURL, reference, hash.String())

	if hash.String() != oldHash.String() {
		klog.V(2).Infof("%q: revision %q hash changed from %q to %q", cc.gitURL, reference, oldHash.String(), hash.String())
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	err = w.Checkout(&git.CheckoutOptions{
		Hash: hash,
	})
	if err != nil {
		return err
	}

	rc := &api.Config{
		GitURL: cc.gitURL,
		GitSha: hash.String(),
	}

	err = helpers.WalkParallel(w.Filesystem, cc.configSubpath, func(path string, info os.FileInfo) error {
		select {
		case <-ctx.Done():
			return nil // shutting down
		default:
		}

		if info.IsDir() {
			return nil
		}

		if !cc.configFileRegexp.MatchString(path) {
			return nil
		}

		klog.V(5).Infof("Processing file %q", path)

		f, err := w.Filesystem.Open(path)
		if err != nil {
			return fmt.Errorf("can't open file %q: %w", path, err)
		}
		defer func() {
			err := f.Close()
			if err != nil {
				klog.Error(err)
			}
		}()

		buffer := make([]byte, info.Size())
		// TODO: smarter read with retries
		_, err = f.Read(buffer)
		if err != nil {
			return err
		}

		err = cc.processConfig(ctx, rc, path, buffer)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Given we are traversing in parallel we need to sort the arrays for the DeepEqual later
	sort.Slice(rc.Releases, func(i, j int) bool {
		return rc.Releases[i].StreamName < rc.Releases[j].StreamName
	})

	for _, r := range rc.Releases {
		sort.Slice(r.RepositoryConfigs, func(i, j int) bool {
			if r.RepositoryConfigs[i].Name < r.RepositoryConfigs[j].Name {
				return true
			} else if r.RepositoryConfigs[i].Name == r.RepositoryConfigs[j].Name {
				return r.RepositoryConfigs[i].GitRef < r.RepositoryConfigs[j].GitRef
			}
			return false
		})
	}

	rcBytes, err := yaml.Marshal(rc)
	if err != nil {
		return err
	}

	updateConfigMap := func(cm *corev1.ConfigMap) {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data["release-config"] = string(rcBytes)
	}

	existingCM, err := cc.kubeClient.CoreV1().ConfigMaps(cc.controllerNamespace).Get(ctx, cc.configmapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: cc.configmapName,
			},
		}
		updateConfigMap(cm)
		klog.V(2).Infof("ConfigMap %s/%s doesn't exist yet, creating.", cc.controllerNamespace, cm.Name)
		cm, err = cc.kubeClient.CoreV1().ConfigMaps(cc.controllerNamespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	cm := existingCM.DeepCopy()
	updateConfigMap(cm)

	if !apiequality.Semantic.DeepEqual(cm, existingCM) {
		klog.V(2).Infof("Updating ConfigMap %s/%s because the data changed.", cc.controllerNamespace, cm.Name)
		cm, err = cc.kubeClient.CoreV1().ConfigMaps(cc.controllerNamespace).Update(ctx, cm, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (cc *CIConfigController) processConfig(ctx context.Context, rc *api.Config, path string, configBytes []byte) error {
	ciConfig := &ciapi.ReleaseBuildConfiguration{}
	err := yaml.Unmarshal(configBytes, ciConfig)
	if err != nil {
		// If the file contains unparsable data, log a warning and skip it
		klog.Warningf("Can't decode file %q, skipping.")
		return nil
	}

	streamName := "unknown"
	if ciConfig.PromotionConfiguration != nil {
		streamName = ciConfig.PromotionConfiguration.Namespace + "/" + ciConfig.PromotionConfiguration.Name
	}

	if !cc.allowedStreamsRegexp.MatchString(streamName) {
		return nil
	}

	var release *api.ReleaseConfig
	for i := range rc.Releases {
		r := &rc.Releases[i]
		if streamName == r.StreamName {
			release = r
		}
	}
	if release == nil {
		rc.Releases = append(rc.Releases, api.ReleaseConfig{
			StreamName: streamName,
		})
		release = &rc.Releases[len(rc.Releases)-1]
	}

	repoID := strings.Join([]string{"github.com", ciConfig.Metadata.Org, ciConfig.Metadata.Repo}, "/")
	repoConfig := &api.RepositoryConfig{
		Name:               repoID,
		GithubOrganisation: ciConfig.Metadata.Org,
		GithubRepository:   ciConfig.Metadata.Repo,
		GitURL:             "https://" + repoID,
		GitRef:             ciConfig.Metadata.Branch,
		GitSha:             "", // resolved bellow
		ReleaseConfigPath:  path,
	}

	currentRepo, err := cc.repoCache.OpenOrClone(ctx, repoConfig.GitURL)
	if err != nil {
		return err
	}

	reference := "refs/remotes/" + git.DefaultRemoteName + "/" + repoConfig.GitRef

	oldResolvedRef, err := currentRepo.Reference(gitplumbing.ReferenceName(reference), true)
	if err != nil {
		return fmt.Errorf("can't resolve reference %q in repository %q: %v", reference, cc.gitURL, err)
	}
	oldHash := oldResolvedRef.Hash()

	err = currentRepo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: git.DefaultRemoteName,
		Force:      true,
		Depth:      1,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", repoConfig.GitRef, git.DefaultRemoteName, repoConfig.GitRef)),
		},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("can't fetch repository %q: %v", repoConfig.GitURL, err)
	}

	resolvedRef, err := currentRepo.Reference(gitplumbing.ReferenceName(reference), true)
	if err != nil {
		// TODO: make it just a warning as it may be bad config and we just fetched
		return fmt.Errorf("can't resolve reference %q in repository %q: %v", reference, repoConfig.GitURL, err)
	}
	hash := resolvedRef.Hash()

	klog.V(5).Infof("%q: resolved revision %q to hash %q", repoConfig.GitURL, reference, hash.String())

	if hash.String() != oldHash.String() {
		klog.V(2).Infof("%q: revision %q hash changed from %q to %q", repoConfig.GitURL, reference, oldHash.String(), hash.String())
	}

	repoConfig.GitSha = hash.String()

	release.RepositoryConfigs = append(release.RepositoryConfigs, *repoConfig)

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

	utilruntime.HandleError(fmt.Errorf("syncing key %v failed with : %v", key, err))
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
