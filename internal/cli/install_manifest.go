package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

const packageManifest = "release-manifest.json"

func installInventory(root, excluded string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("inventory refuses symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("inventory refuses special file %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == excluded {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files[rel] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	return files, err
}

func verifyInstallManifest(root string) error {
	data, err := os.ReadFile(filepath.Join(root, packageManifest))
	if err != nil {
		return fmt.Errorf("read package manifest: %w", err)
	}
	var manifest struct {
		SchemaVersion int               `json:"schema_version"`
		Files         map[string]string `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.SchemaVersion != 1 || len(manifest.Files) == 0 {
		return fmt.Errorf("invalid package manifest")
	}
	actual, err := installInventory(root, packageManifest)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, manifest.Files) {
		return fmt.Errorf("package bytes differ from release manifest; restore the complete verified release")
	}
	return nil
}

// Installation rewrites links and initializes Runtime. These derived bytes
// have their own receipt; they must never be compared to packaged hashes.
func recordInstalledManifest(source, stage string) error {
	const receipt = ".claude/framework-installation.json"
	files, err := installInventory(stage, receipt)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(source, packageManifest))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	encoded, err := json.MarshalIndent(map[string]any{
		"schema_version": 1, "package_manifest_sha256": hex.EncodeToString(digest[:]),
		"installed_files": files, "note": "Installation-time bytes; mutable Runtime is expected to advance after installation.",
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stage, receipt), append(encoded, '\n'), 0644)
}
