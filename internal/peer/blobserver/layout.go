package blobserver

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type ShardRef struct {
	Owner  string `json:"owner"`
	BlobID string `json:"blob_id"`
	Index  int    `json:"index"`
}

func (r ShardRef) Validate() error {
	if err := validateSegment("owner", r.Owner); err != nil {
		return err
	}
	if err := validateSegment("blob_id", r.BlobID); err != nil {
		return err
	}
	if r.Index < 0 {
		return errors.New("index must be non-negative")
	}
	return nil
}

func ShardPath(root string, ref ShardRef) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return filepath.Join(root, ref.Owner, ref.BlobID, fmt.Sprintf("%d.shard", ref.Index)), nil
}

func validateSegment(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s cannot be %q", name, value)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s cannot contain path separators", name)
	}
	return nil
}
