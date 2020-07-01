package gittools

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"k8s.io/klog/v2"
)

type RepoCache struct {
	root string
}

func NewRepoCache(root string) *RepoCache {
	return &RepoCache{
		root: root,
	}
}

func (rc *RepoCache) OpenOrClone(ctx context.Context, repoURL string) (*git.Repository, error) {
	dir := base64.StdEncoding.EncodeToString([]byte(repoURL))

	p := filepath.Join(rc.root, dir)

	repo, err := git.PlainOpenWithOptions(p, &git.PlainOpenOptions{DetectDotGit: false})
	if errors.Is(err, git.ErrRepositoryNotExists) {
		klog.V(4).Infof("Repository %q at path %q doesn't exist yet.", repoURL, p)
	} else if err != nil {
		return nil, err
	} else {
		return repo, nil
	}

	klog.V(3).Infof("Cloning repository %q...", repoURL)
	repo, err = git.PlainCloneContext(ctx, p, false, &git.CloneOptions{
		URL:        repoURL,
		NoCheckout: true,
		/*
				go-git fails fetching correctly if these are set but it would make it faster.
				SingleBranch: false,
			 	Depth:      1,
		*/
	})
	if err != nil {
		return nil, err
	}
	klog.V(3).Infof("Cloning repository %q complete.", repoURL)

	return repo, nil
}
