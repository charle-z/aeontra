//go:build !windows

package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const projectWorktreeContentDigestDomain = "mcp-devbox-managed-worktree-content-v1\x00"

type projectWorktreeContentPath struct {
	name    string
	tracked bool
}

func projectWorktreeContentDigest(ctx context.Context, manager *ProjectWorktreeManager, snapshot ProjectWorktreeSnapshot) (string, error) {
	return projectSourceContentDigest(ctx, snapshot.path, func(args []string) ([]string, error) {
		return readProjectWorktreeContentGitPaths(ctx, manager, snapshot, args)
	}, func() error { return manager.revalidate(ctx, snapshot) })
}

// RegisteredProjectContentDigest uses the same bounded descriptor-relative
// hashing as managed worktrees. The registry is revalidated around inventory
// and hashing; dirty files remain source, rather than a security violation.
func RegisteredProjectContentDigest(ctx context.Context, registry *ProjectRegistry, resolved ProjectResolution, runner DevGitCommandRunner) (string, error) {
	if registry == nil || runner == nil || !resolved.Project.ClaimGenerationValid {
		return "", ErrProjectWorktreeInvalid
	}
	revalidate := func() error {
		current, err := registry.Resolve(ctx, resolved.Project.Alias, resolved.TargetAlias)
		if err != nil {
			return err
		}
		if current.Workspace != resolved.Workspace || current.Project.ClaimGeneration != resolved.Project.ClaimGeneration ||
			current.Project.Owner != resolved.Project.Owner || current.Project.Repository != resolved.Project.Repository {
			return ErrProjectWorktreeUnsafe
		}
		return nil
	}
	return projectSourceContentDigest(ctx, resolved.Workspace.Path, func(args []string) ([]string, error) {
		output, err := runner.Run(ctx, resolved.Workspace.Path, args, GitHubCredential{})
		if err != nil {
			return nil, err
		}
		return parseProjectWorktreeContentPaths([]byte(output))
	}, revalidate)
}

// RegisteredProjectSourceEvidence binds the hash to one stable observed Git
// HEAD/index state. Paths from status never leave the Edge. This is source
// evidence for development routing, not an assertion that ignored build
// outputs or a developer's concurrent writes form an immutable snapshot.
func RegisteredProjectSourceEvidence(ctx context.Context, registry *ProjectRegistry, resolved ProjectResolution, runner DevGitCommandRunner) (digest, head string, clean bool, err error) {
	if registry == nil || runner == nil {
		return "", "", false, ErrProjectWorktreeInvalid
	}
	read := func() (string, string, error) {
		head, err := runner.Run(ctx, resolved.Workspace.Path, []string{"rev-parse", "--verify", "HEAD"}, GitHubCredential{})
		if err != nil || !devGitCommitPattern.MatchString(strings.TrimSpace(head)) {
			return "", "", ErrProjectWorktreeUnavailable
		}
		status, err := runner.Run(ctx, resolved.Workspace.Path, []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}, GitHubCredential{})
		return strings.TrimSpace(head), status, err
	}
	beforeHead, beforeStatus, err := read()
	if err != nil {
		return "", "", false, err
	}
	digest, err = RegisteredProjectContentDigest(ctx, registry, resolved, runner)
	if err != nil {
		return "", "", false, err
	}
	afterHead, afterStatus, err := read()
	if err != nil || beforeHead != afterHead || beforeStatus != afterStatus {
		return "", "", false, ErrProjectWorktreeUnavailable
	}
	return digest, afterHead, afterStatus == "", nil
}

func projectSourceContentDigest(ctx context.Context, sourcePath string, inventory func([]string) ([]string, error), revalidate func() error) (string, error) {
	root, rootInfo, err := openProjectWorktreeContentRoot(sourcePath)
	if err != nil {
		return "", err
	}
	defer root.Close()

	headPaths, err := inventory([]string{"ls-tree", "-r", "--full-tree", "-z", "--name-only", "HEAD"})
	if err != nil {
		return "", err
	}
	indexPaths, err := inventory([]string{"ls-files", "--cached", "-z"})
	if err != nil {
		return "", err
	}
	untrackedPaths, err := inventory([]string{"ls-files", "--others", "--exclude-standard", "-z"})
	if err != nil {
		return "", err
	}

	byName := make(map[string]bool, len(headPaths)+len(indexPaths)+len(untrackedPaths))
	for _, paths := range [][]string{headPaths, indexPaths} {
		for _, name := range paths {
			byName[name] = true
		}
	}
	for _, name := range untrackedPaths {
		if _, ok := byName[name]; !ok {
			byName[name] = false
		}
	}
	if len(byName) > maxProjectWorktreeContentFiles {
		return "", ErrProjectWorktreeUnsafe
	}
	paths := make([]projectWorktreeContentPath, 0, len(byName))
	for name, tracked := range byName {
		paths = append(paths, projectWorktreeContentPath{name: name, tracked: tracked})
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].name < paths[j].name })

	// Git ran through the managed path, while reads below use the pinned directory
	// descriptor. Revalidate both the registration and path identity after the Git
	// inventory so an exchanged worktree cannot silently supply the file list.
	if err := revalidate(); err != nil {
		return "", err
	}
	currentRootInfo, err := os.Lstat(sourcePath)
	if err != nil || !currentRootInfo.IsDir() || currentRootInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(rootInfo, currentRootInfo) {
		return "", ErrProjectWorktreeUnsafe
	}

	digest, err := hashProjectWorktreeContent(ctx, int(root.Fd()), paths)
	if err != nil {
		return "", err
	}
	// The descriptor above pins the directory being read, but the next
	// operation will resolve the path again. Refuse a digest if that path was
	// exchanged while hashing or its managed identity changed.
	if err := revalidate(); err != nil {
		return "", err
	}
	currentRootInfo, err = os.Lstat(sourcePath)
	if err != nil || !currentRootInfo.IsDir() || currentRootInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(rootInfo, currentRootInfo) {
		return "", ErrProjectWorktreeUnsafe
	}
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func readProjectWorktreeContentGitPaths(ctx context.Context, manager *ProjectWorktreeManager, snapshot ProjectWorktreeSnapshot, args []string) ([]string, error) {
	output, err := manager.runner.Run(ctx, snapshot.path, args, manager.credential)
	if err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	return parseProjectWorktreeContentPaths([]byte(output))
}

func openProjectWorktreeContentRoot(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, ErrProjectWorktreeUnavailable
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrProjectWorktreeUnsafe
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, nil, ErrProjectWorktreeUnsafe
		}
		return nil, nil, ErrProjectWorktreeUnavailable
	}
	root := os.NewFile(uintptr(fd), "managed-worktree")
	if root == nil {
		_ = unix.Close(fd)
		return nil, nil, ErrProjectWorktreeUnavailable
	}
	openedInfo, err := root.Stat()
	if err != nil || !openedInfo.IsDir() || !os.SameFile(info, openedInfo) {
		_ = root.Close()
		return nil, nil, ErrProjectWorktreeUnsafe
	}
	return root, openedInfo, nil
}

func hashProjectWorktreeContent(ctx context.Context, rootFD int, paths []projectWorktreeContentPath) ([sha256.Size]byte, error) {
	hash := sha256.New()
	_, _ = io.WriteString(hash, projectWorktreeContentDigestDomain)
	writeProjectWorktreeDigestUint64(hash, uint64(len(paths)))
	var totalBytes int64
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, ErrProjectWorktreeUnavailable
		}
		file, found, err := openProjectWorktreeContentFile(rootFD, path.name)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if !found {
			if !path.tracked {
				return [sha256.Size]byte{}, ErrProjectWorktreeUnavailable
			}
			_, _ = hash.Write([]byte{0x00})
			writeProjectWorktreeDigestString(hash, path.name)
			continue
		}
		if err := hashProjectWorktreeRegularFile(ctx, hash, file, path.name, &totalBytes); err != nil {
			_ = file.Close()
			return [sha256.Size]byte{}, err
		}
		if err := file.Close(); err != nil {
			return [sha256.Size]byte{}, ErrProjectWorktreeUnavailable
		}
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func hashProjectWorktreeRegularFile(ctx context.Context, hash io.Writer, file *os.File, name string, totalBytes *int64) error {
	before, err := file.Stat()
	if err != nil {
		return ErrProjectWorktreeUnavailable
	}
	if !before.Mode().IsRegular() || before.Size() < 0 {
		return ErrProjectWorktreeUnsafe
	}
	if before.Size() > maxProjectWorktreeContentBytes-*totalBytes {
		return ErrProjectWorktreeUnsafe
	}
	executableMode := byte(before.Mode().Perm() & 0o111)
	_, _ = hash.Write([]byte{0x01})
	writeProjectWorktreeDigestString(hash, name)
	_, _ = hash.Write([]byte{executableMode})
	writeProjectWorktreeDigestUint64(hash, uint64(before.Size()))

	reader := projectWorktreeContextReader{ctx: ctx, reader: file}
	read, err := io.CopyN(hash, reader, before.Size())
	if err != nil || read != before.Size() {
		return ErrProjectWorktreeUnavailable
	}
	*totalBytes += read
	var extra [1]byte
	n, err := file.Read(extra[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return ErrProjectWorktreeUnavailable
	}
	after, err := file.Stat()
	if err != nil {
		return ErrProjectWorktreeUnavailable
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() != before.Size() ||
		after.Mode().Perm()&0o111 != before.Mode().Perm()&0o111 || !after.ModTime().Equal(before.ModTime()) {
		return ErrProjectWorktreeUnsafe
	}
	return nil
}

type projectWorktreeContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader projectWorktreeContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func openProjectWorktreeContentFile(rootFD int, relative string) (*os.File, bool, error) {
	if len(relative) == 0 || len(relative) > maxProjectWorktreeContentPathBytes || strings.HasPrefix(relative, "/") {
		return nil, false, ErrProjectWorktreeUnsafe
	}
	components := strings.Split(relative, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, 0) {
			return nil, false, ErrProjectWorktreeUnsafe
		}
	}
	parentFD, err := unix.Dup(rootFD)
	if err != nil {
		return nil, false, ErrProjectWorktreeUnavailable
	}
	for _, component := range components[:len(components)-1] {
		nextFD, openErr := unix.Openat(parentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(parentFD)
		if errors.Is(openErr, unix.ENOENT) {
			return nil, false, nil
		}
		if errors.Is(openErr, unix.ELOOP) || errors.Is(openErr, unix.ENOTDIR) {
			return nil, false, ErrProjectWorktreeUnsafe
		}
		if openErr != nil {
			return nil, false, ErrProjectWorktreeUnavailable
		}
		parentFD = nextFD
	}
	defer unix.Close(parentFD)

	name := components[len(components)-1]
	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, ErrProjectWorktreeUnavailable
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, false, ErrProjectWorktreeUnsafe
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		if errors.Is(err, unix.ELOOP) {
			return nil, false, ErrProjectWorktreeUnsafe
		}
		return nil, false, ErrProjectWorktreeUnavailable
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		_ = unix.Close(fd)
		return nil, false, ErrProjectWorktreeUnavailable
	}
	if after.Mode&unix.S_IFMT != unix.S_IFREG || before.Dev != after.Dev || before.Ino != after.Ino {
		_ = unix.Close(fd)
		return nil, false, ErrProjectWorktreeUnsafe
	}
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, ErrProjectWorktreeUnavailable
	}
	return file, true, nil
}

func writeProjectWorktreeDigestString(writer io.Writer, value string) {
	writeProjectWorktreeDigestUint64(writer, uint64(len(value)))
	_, _ = io.WriteString(writer, value)
}

func writeProjectWorktreeDigestUint64(writer io.Writer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}
