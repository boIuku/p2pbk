package challenges

import (
	"time"

	"p2pbk/internal/peer/blobserver"
)

type Issuer struct {
	MaxLength int64
	Now       func() time.Time
}

func (i Issuer) Issue(ref blobserver.ShardRef, shardSize int64) (Challenge, error) {
	maxLength := i.MaxLength
	if maxLength == 0 {
		maxLength = 64 * 1024
	}
	now := time.Now
	if i.Now != nil {
		now = i.Now
	}
	return New(ref, shardSize, maxLength, now())
}
