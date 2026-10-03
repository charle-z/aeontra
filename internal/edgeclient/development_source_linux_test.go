//go:build linux

package edgeclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func registeredSourceDigestForTest(t *testing.T, root string, tracked, untracked []string) (string, error) {
	t.Helper()
	return projectSourceContentDigestWithPolicy(t.Context(), root, func(args []string) ([]string, error) {
		if args[0] == "ls-tree" {
			return tracked, nil
		}
		if args[1] == "--others" {
			return untracked, nil
		}
		return nil, nil
	}, func() error { return nil }, registeredProjectSourceContentPolicy())
}

func TestRegisteredSourceLeafLinksHashTextWithoutReadingTargets(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside contents"), 0o000); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"missing", "../outside", outside} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			link := filepath.Join(root, "link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			initial, err := registeredSourceDigestForTest(t, root, []string{"link"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			untracked, err := registeredSourceDigestForTest(t, root, nil, []string{"link"})
			if err != nil || untracked != initial {
				t.Fatalf("tracked/untracked link differs: %q %v", untracked, err)
			}
			if target == outside {
				if err := os.Chmod(outside, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(outside, []byte("changed outside contents"), 0o600); err != nil {
					t.Fatal(err)
				}
				repeated, err := registeredSourceDigestForTest(t, root, []string{"link"}, nil)
				if err != nil || repeated != initial {
					t.Fatal("outside target contents influenced source digest")
				}
			}
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target+"-changed", link); err != nil {
				t.Fatal(err)
			}
			changed, err := registeredSourceDigestForTest(t, root, []string{"link"}, nil)
			if err != nil || changed == initial {
				t.Fatal("link text was omitted from source digest")
			}
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(link, []byte(target), 0o600); err != nil {
				t.Fatal(err)
			}
			regular, err := registeredSourceDigestForTest(t, root, []string{"link"}, nil)
			if err != nil || regular == initial {
				t.Fatal("file type was omitted from source digest")
			}
		})
	}
}

func TestRegisteredSourcePinnedLinksRejectExchangedEntries(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink("original", link); err != nil {
		t.Fatal(err)
	}
	directory, _, err := openProjectWorktreeContentRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	pinned, found, err := openProjectSourceContentFile(int(directory.Fd()), "link", true)
	if err != nil || !found {
		t.Fatalf("pin link: %v", err)
	}
	defer pinned.Close()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("replacement", link); err != nil {
		t.Fatal(err)
	}
	text, err := readPinnedProjectSourceSymlink(pinned)
	if err != nil || string(text) != "original" {
		t.Fatalf("pinned link observed replacement: %q %v", text, err)
	}
	if err := revalidateProjectSourceContentFile(int(directory.Fd()), "link", pinned); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("exchanged entry accepted: %v", err)
	}
}

func TestRegisteredSourceRejectsUnsafeParentsAndSpecialEntries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "parent")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(socket)
	if err := unix.Bind(socket, &unix.SockaddrUnix{Name: filepath.Join(root, "socket")}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"parent/file", "directory", "fifo", "socket", "../outside", "/outside", "./file", "parent//file"} {
		if _, err := registeredSourceDigestForTest(t, root, []string{path}, nil); !errors.Is(err, ErrProjectWorktreeUnsafe) {
			t.Errorf("unsafe path %q accepted: %v", path, err)
		}
	}
	if _, err := registeredSourceDigestForTest(t, root, nil, []string{"vanished"}); !errors.Is(err, ErrProjectWorktreeUnavailable) {
		t.Fatalf("missing untracked entry accepted: %v", err)
	}
	rootLink := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := registeredSourceDigestForTest(t, rootLink, nil, nil); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("root symlink accepted: %v", err)
	}
}

func TestRegisteredSourceFiniteInventoryAndLinkLimits(t *testing.T) {
	var paths bytes.Buffer
	for i := 0; i < maxRegisteredProjectSourceFiles; i++ {
		fmt.Fprintf(&paths, "file-%05d\x00", i)
	}
	if _, err := parseProjectSourceContentPaths(paths.Bytes(), maxRegisteredProjectSourceFiles, maxRegisteredProjectSourcePathBytes); err != nil {
		t.Fatal(err)
	}
	paths.WriteString("one-too-many\x00")
	if _, err := parseProjectSourceContentPaths(paths.Bytes(), maxRegisteredProjectSourceFiles, maxRegisteredProjectSourcePathBytes); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("excess path count accepted: %v", err)
	}
	paths.Reset()
	// 512 distinct maximum-sized paths occupy exactly 2 MiB with their NULs.
	for i := 0; i < 512; i++ {
		fmt.Fprintf(&paths, "%04d%s\x00", i, strings.Repeat("x", 4091))
	}
	if _, err := parseProjectSourceContentPaths(paths.Bytes(), maxRegisteredProjectSourceFiles, maxRegisteredProjectSourcePathBytes); err != nil {
		t.Fatal(err)
	}
	paths.WriteByte(0)
	if _, err := parseProjectSourceContentPaths(paths.Bytes(), maxRegisteredProjectSourceFiles, maxRegisteredProjectSourcePathBytes); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("excess path bytes accepted: %v", err)
	}
	for _, size := range []int{4096, 4097} {
		_, err := parseProjectSourceContentPaths(append(bytes.Repeat([]byte{'x'}, size), 0), maxRegisteredProjectSourceFiles, maxRegisteredProjectSourcePathBytes)
		if (size == 4096 && err != nil) || (size == 4097 && !errors.Is(err, ErrProjectWorktreeUnsafe)) {
			t.Fatalf("path size %d: %v", size, err)
		}
		total := int64(0)
		err = writeRegisteredProjectSourceSymlink(io.Discard, "link", bytes.Repeat([]byte{'x'}, size), &total)
		if (size == 4096 && err != nil) || (size == 4097 && !errors.Is(err, ErrProjectWorktreeUnsafe)) {
			t.Fatalf("link size %d: %v", size, err)
		}
	}
	total := maxProjectWorktreeContentBytes - 1
	if err := writeRegisteredProjectSourceSymlink(io.Discard, "link", []byte("x"), &total); err != nil {
		t.Fatal(err)
	}
	if err := writeRegisteredProjectSourceSymlink(io.Discard, "link", []byte("x"), &total); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("excess cumulative bytes accepted: %v", err)
	}
}

func TestRegisteredSourceDomainAndContentByteLimit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered, err := registeredSourceDigestForTest(t, root, []string{"file"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := projectSourceContentDigest(t.Context(), root, func([]string) ([]string, error) { return []string{"file"}, nil }, func() error { return nil })
	if err != nil || registered == managed {
		t.Fatal("registered and managed source domains are not separated")
	}
	for _, size := range []int64{maxProjectWorktreeContentBytes, maxProjectWorktreeContentBytes + 1} {
		if err := os.Truncate(path, size); err != nil {
			t.Fatal(err)
		}
		_, err := registeredSourceDigestForTest(t, root, []string{"file"}, nil)
		if (size == maxProjectWorktreeContentBytes && err != nil) || (size > maxProjectWorktreeContentBytes && !errors.Is(err, ErrProjectWorktreeUnsafe)) {
			t.Fatalf("content size %d: %v", size, err)
		}
	}
}

func TestRegisteredSourceRejectsUntrackedEmbeddedGitRepository(t *testing.T) {
	fixture := newProjectWorktreeFixture(t)
	developmentSourceFixtureGit(t, fixture.canonical.Path, "remote", "add", "origin", "https://github.com/charle-z/project.git")
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: fixture.stateRoot, AllowedOwner: "charle-z", Workspaces: fixture.workspaces})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, _, err := registry.Register(ProjectRegistration{Alias: "project", Owner: "charle-z", Repository: "project", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: fixture.canonical.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell}}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(fixture.canonical.Path, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	developmentSourceFixtureGit(t, nested, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(nested, "source"), []byte("embedded source"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(t.Context(), "project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRegisteredProjectSourceGitRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin")
	if _, _, _, err := RegisteredProjectSourceEvidence(t.Context(), registry, resolved, runner); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("embedded source was omitted or accepted: %v", err)
	}
}

func TestRegisteredSourceGitOutputIsCompleteAndSeparated(t *testing.T) {
	for _, size := range []int{maxRegisteredProjectSourcePathBytes, maxRegisteredProjectSourcePathBytes + 1, maxRegisteredProjectSourcePathBytes + 2} {
		stdout := &boundedHTBLabCapture{limit: maxRegisteredProjectSourcePathBytes + 1}
		stderr := &boundedHTBLabCapture{limit: 64 << 10}
		value := append(bytes.Repeat([]byte{'x'}, size-1), 0)
		if size > maxRegisteredProjectSourcePathBytes+1 {
			value[maxRegisteredProjectSourcePathBytes] = 0 // Captured prefix ends at a complete entry.
		}
		_, _ = stdout.Write(value)
		_, _ = stderr.Write([]byte("benign Git warning"))
		output, err := registeredProjectSourceGitOutput(stdout, stderr, nil)
		if size == maxRegisteredProjectSourcePathBytes {
			if err != nil || len(output) != size || strings.Contains(output, "warning") {
				t.Fatalf("complete stdout contaminated: %v", err)
			}
		} else if err == nil || output != "" {
			t.Fatalf("NUL-boundary truncation accepted at %d bytes", size)
		}
	}
	stdout := &boundedHTBLabCapture{limit: maxRegisteredProjectSourcePathBytes + 1}
	stderr := &boundedHTBLabCapture{limit: 64 << 10}
	_, _ = stderr.Write(bytes.Repeat([]byte{'x'}, (64<<10)+1))
	if _, err := registeredProjectSourceGitOutput(stdout, stderr, nil); err == nil {
		t.Fatal("truncated stderr accepted")
	}
	if _, err := registeredProjectSourceGitOutput(stdout, &boundedHTBLabCapture{}, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatal("Git failure was hidden")
	}
}

func TestRegisteredSourceGitRunnerRestrictsArgumentsAndCredentials(t *testing.T) {
	runner := NewRegisteredProjectSourceGitRunner(t.TempDir(), "/usr/local/bin:/usr/bin:/bin")
	for _, args := range [][]string{{"fetch", "origin"}, {"status"}, {"ls-files", "--cached", "-z", "--", "file"}, {"-c", "core.hooksPath=hook", "status"}} {
		if _, err := runner.Run(t.Context(), t.TempDir(), args, GitHubCredential{}); err == nil {
			t.Fatalf("non-source argv accepted: %q", args)
		}
	}
	if _, err := runner.Run(t.Context(), t.TempDir(), []string{"rev-parse", "--verify", "HEAD"}, GitHubCredential{Owner: "charle-z"}); err == nil {
		t.Fatal("source runner accepted credential")
	}
	fixture := newProjectWorktreeFixture(t)
	developmentSourceFixtureGit(t, fixture.canonical.Path, "update-ref", "refs/tags/HEAD", fixture.head)
	output, err := runner.Run(t.Context(), fixture.canonical.Path, []string{"rev-parse", "--verify", "HEAD"}, GitHubCredential{})
	if err != nil || strings.TrimSpace(output) != fixture.head {
		t.Fatalf("real Git warning contaminated HEAD: %q %v", output, err)
	}
}
