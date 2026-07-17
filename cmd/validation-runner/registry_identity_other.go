//go:build !linux

package main

import (
	"errors"
	"os"
)

type fileIdentity struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime int64
}

func identityFromFileInfo(info os.FileInfo) (fileIdentity, error) {
	if info == nil {
		return fileIdentity{}, errors.New("filesystem identity is unavailable")
	}
	return fileIdentity{name: info.Name(), size: info.Size(), mode: info.Mode(), modTime: info.ModTime().UnixNano()}, nil
}

func verifyRepositoryDescriptors(root, repoID string, expectedRoot, expectedRepo fileIdentity) error {
	rootFile, err := os.Open(root)
	if err != nil {
		return errors.New("repository registry root cannot be opened safely")
	}
	defer rootFile.Close()
	rootInfo, err := rootFile.Stat()
	if err != nil {
		return errors.New("repository registry root identity is unavailable")
	}
	rootIdentity, err := identityFromFileInfo(rootInfo)
	if err != nil || rootIdentity != expectedRoot {
		return errors.New("repository registry root identity changed")
	}
	repoFile, err := os.Open(root + string(os.PathSeparator) + repoID)
	if err != nil {
		return errors.New("registered repository cannot be opened safely")
	}
	defer repoFile.Close()
	repoInfo, err := repoFile.Stat()
	if err != nil {
		return errors.New("registered repository identity is unavailable")
	}
	repoIdentity, err := identityFromFileInfo(repoInfo)
	if err != nil || repoIdentity != expectedRepo {
		return errors.New("registered repository identity changed")
	}
	return nil
}
