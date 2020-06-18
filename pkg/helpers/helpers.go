package helpers

import (
	"os"

	"github.com/go-git/go-billy/v5"
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

func Walk(fs billy.Filesystem, root string, walkFn WalkFunc) error {
	klog.Infof("walking path %q", root)
	info, err := fs.Lstat(root)
	if err != nil {
		klog.Errorf("walking path %q failed", root)
		return err
	}

	klog.Infof("walking path %q recursion", root)

	return walk(fs, root, info, walkFn)
}
