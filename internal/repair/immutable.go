package repair

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// Standalone authoring publishes only complete, synced files. Runtime producers
// use preparedArtifacts instead, so publication participates in pending recovery.
func publishImmutableBytes(repository, relative string, data []byte) (ArtifactRef, error) {
	if relative != path.Clean(relative) || !strings.HasPrefix(relative, artifactRoot+"/") || strings.Contains(relative, "\\") {
		return ArtifactRef{}, fmt.Errorf("invalid repair artifact path %q", relative)
	}
	root, err := os.OpenRoot(repository)
	if err != nil {
		return ArtifactRef{}, err
	}
	defer root.Close()
	if err := checkImmutablePath(root, relative); err != nil {
		return ArtifactRef{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ArtifactRef{}, err
	}
	stage := ".claude/operations/staging/" + hex.EncodeToString(random[:])
	if err := checkImmutablePath(root, stage+"/0.data"); err != nil {
		return ArtifactRef{}, err
	}
	if err := root.MkdirAll(path.Dir(stage), 0700); err != nil {
		return ArtifactRef{}, err
	}
	if err := root.Mkdir(stage, 0700); err != nil {
		return ArtifactRef{}, err
	}
	defer root.RemoveAll(stage)
	file, err := root.OpenFile(stage+"/0.data", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return ArtifactRef{}, err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return ArtifactRef{}, err
	}
	if err := checkImmutablePath(root, relative); err != nil {
		return ArtifactRef{}, err
	}
	if err := root.MkdirAll(path.Dir(relative), 0755); err != nil {
		return ArtifactRef{}, err
	}
	// Link is the no-overwrite publication boundary. Even identical historical
	// content is a collision; authoring has no operation receipt to replay.
	if err := root.Link(stage+"/0.data", relative); err != nil {
		return ArtifactRef{}, fmt.Errorf("publish immutable artifact %s: %w", relative, err)
	}
	for dir := path.Dir(relative); ; dir = path.Dir(dir) {
		file, err := root.Open(dir)
		if err != nil {
			return ArtifactRef{}, err
		}
		err = errors.Join(file.Sync(), file.Close())
		if err != nil {
			// A published file is immutable history, even if fsync fails. Do
			// not remove it based on an ambiguous failure acknowledgement.
			return ArtifactRef{}, fmt.Errorf("sync published artifact %s: %w; retain for inspection", relative, err)
		}
		if dir == "." {
			break
		}
	}
	return fileRef(relative, data), nil
}

func checkImmutablePath(root *os.Root, relative string) error {
	parts := strings.Split(relative, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("repair artifact path contains a symlink: %s", relative)
		}
		if (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return fmt.Errorf("invalid repair artifact file type: %s", relative)
		}
	}
	return nil
}
