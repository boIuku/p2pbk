package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kopia/kopia/repo/blob"

	"p2pbk/internal/config"
)

type backupIndex struct {
	Name      string       `json:"name"`
	Source    string       `json:"source"`
	CreatedAt time.Time    `json:"created_at"`
	Entries   []backupFile `json:"entries"`
}

type backupFile struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Mode uint32 `json:"mode"`
}

type localRepository struct {
	root    string
	storage blob.Storage
}

func openLocalRepository(cfg config.Config) (localRepository, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return localRepository{}, err
	}
	root := filepath.Join(paths.DataDir, "repository")
	if err := os.MkdirAll(filepath.Join(root, "objects"), 0o700); err != nil {
		return localRepository{}, err
	}
	if err := os.MkdirAll(filepath.Join(root, "backups"), 0o700); err != nil {
		return localRepository{}, err
	}
	return localRepository{root: root}, nil
}

func openStorageRepository(cfg config.Config, storage blob.Storage) (localRepository, error) {
	repo, err := openLocalRepository(cfg)
	if err != nil {
		return localRepository{}, err
	}
	repo.storage = storage
	return repo, nil
}

func (r localRepository) Backup(source, name string) (backupIndex, error) {
	info, err := os.Stat(source)
	if err != nil {
		return backupIndex{}, err
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return backupIndex{}, err
	}
	if name == "" {
		name = filepath.Base(sourceAbs)
	}
	idx := backupIndex{Name: name, Source: sourceAbs, CreatedAt: time.Now().UTC()}

	if info.IsDir() {
		err = filepath.WalkDir(sourceAbs, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(sourceAbs, path)
			if err != nil {
				return err
			}
			entry, err := r.storeFile(path, filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			idx.Entries = append(idx.Entries, entry)
			return nil
		})
	} else {
		idx.Entries = nil
		entry, err := r.storeFile(sourceAbs, filepath.Base(sourceAbs))
		if err != nil {
			return backupIndex{}, err
		}
		idx.Entries = append(idx.Entries, entry)
	}
	if err != nil {
		return backupIndex{}, err
	}
	return idx, r.writeIndex(idx)
}

func (r localRepository) Restore(name, target string) (backupIndex, error) {
	idx, err := r.readIndex(name)
	if err != nil {
		return backupIndex{}, err
	}
	for _, entry := range idx.Entries {
		dst := filepath.Join(target, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return backupIndex{}, err
		}
		src := r.objectPath(entry.Hash)
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(entry.Mode))
		if err != nil {
			return backupIndex{}, err
		}
		var restoreErr error
		if r.storage != nil {
			restoreErr = r.storage.GetBlob(context.Background(), blob.ID(entry.Hash), 0, -1, blobOutput{Writer: out})
		} else {
			in, err := os.Open(src)
			if err != nil {
				_ = out.Close()
				return backupIndex{}, err
			}
			_, restoreErr = io.Copy(out, in)
			restoreErr = errorsJoin(restoreErr, in.Close())
		}
		closeErr := out.Close()
		if restoreErr != nil {
			return backupIndex{}, restoreErr
		}
		if closeErr != nil {
			return backupIndex{}, closeErr
		}
	}
	return idx, nil
}

func (r localRepository) storeFile(path, rel string) (backupFile, error) {
	in, err := os.Open(path)
	if err != nil {
		return backupFile{}, err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return backupFile{}, err
	}
	h := sha256.New()
	tmp, err := os.CreateTemp(filepath.Join(r.root, "objects"), "tmp-*")
	if err != nil {
		return backupFile{}, err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return backupFile{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return backupFile{}, err
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if r.storage != nil {
		data, err := os.ReadFile(tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			return backupFile{}, err
		}
		if err := r.storage.PutBlob(context.Background(), blob.ID(hash), blobBytes(data), blob.PutOptions{}); err != nil {
			_ = os.Remove(tmpPath)
			return backupFile{}, err
		}
	}
	objectPath := r.objectPath(hash)
	if _, err := os.Stat(objectPath); os.IsNotExist(err) {
		if err := os.Rename(tmpPath, objectPath); err != nil {
			_ = os.Remove(tmpPath)
			return backupFile{}, err
		}
	} else {
		_ = os.Remove(tmpPath)
	}
	return backupFile{Path: rel, Hash: hash, Size: info.Size(), Mode: uint32(info.Mode().Perm())}, nil
}

func (r localRepository) writeIndex(idx backupIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.indexPath(idx.Name), data, 0o600)
}

func (r localRepository) readIndex(name string) (backupIndex, error) {
	var idx backupIndex
	data, err := os.ReadFile(r.indexPath(name))
	if err != nil {
		return idx, err
	}
	return idx, json.Unmarshal(data, &idx)
}

func (r localRepository) objectPath(hash string) string {
	return filepath.Join(r.root, "objects", hash)
}

func (r localRepository) indexPath(name string) string {
	return filepath.Join(r.root, "backups", safeName(name)+".json")
}

func safeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "backup"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(name)
}

func errorsJoin(errs ...error) error {
	var out error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if out == nil {
			out = err
		} else {
			out = fmt.Errorf("%v; %w", out, err)
		}
	}
	return out
}

type blobBytes []byte

func (b blobBytes) Length() int {
	return len(b)
}

func (b blobBytes) Reader() io.ReadSeekCloser {
	return nopReadSeekCloser{Reader: bytes.NewReader(b)}
}

func (b blobBytes) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(b)
	return int64(n), err
}

type nopReadSeekCloser struct {
	*bytes.Reader
}

func (nopReadSeekCloser) Close() error {
	return nil
}

type blobOutput struct {
	io.Writer
}

func (o blobOutput) Reset() {}

func (o blobOutput) Length() int {
	return 0
}
