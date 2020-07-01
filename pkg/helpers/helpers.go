package helpers

import (
	"os"
	"sort"
	"sync"

	"github.com/go-git/go-billy/v5"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog"
)

type WalkFunc func(path string, info os.FileInfo) error

func walk(fs billy.Filesystem, path string, info os.FileInfo, walkFn WalkFunc) error {
	err := walkFn(path, info)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return nil
	}

	children, err := fs.ReadDir(path)
	if err != nil {
		return err
	}

	sort.SliceStable(children, func(i, j int) bool {
		return children[i].Name() < children[j].Name()
	})

	for _, child := range children {
		filename := fs.Join(path, child.Name())
		fileInfo, err := fs.Lstat(filename)
		if err != nil {
			return err
		}

		err = walk(fs, filename, fileInfo, walkFn)
		if err != nil {
			return err
		}
	}

	return nil
}

func WalkParallel(fs billy.Filesystem, root string, walkFn WalkFunc) error {
	info, err := fs.Lstat(root)
	if err != nil {
		klog.Errorf("walking path %q failed", root)
		return err
	}

	var errs []error
	var walkersWg sync.WaitGroup
	var syncerWg sync.WaitGroup
	errChan := make(chan error, 10)

	syncerWg.Add(1)
	go func() {
		defer syncerWg.Done()
		for e := range errChan {
			errs = append(errs, e)
		}
	}()

	err = walk(fs, root, info, func(path string, info os.FileInfo) error {
		walkersWg.Add(1)
		go func() {
			defer walkersWg.Done()
			errChan <- walkFn(path, info)
		}()

		return nil
	})

	walkersWg.Wait()

	close(errChan)

	syncerWg.Wait()

	errs = append(errs, err)

	return errors.NewAggregate(errs)
}
