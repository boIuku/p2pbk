package erasure

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/reedsolomon"
)

var (
	ErrNotEnoughShards = errors.New("not enough valid shards")
	ErrShardHash       = errors.New("shard hash mismatch")
)

type Codec struct {
	dataShards   int
	parityShards int
	encoder      reedsolomon.Encoder
}

type Shard struct {
	Index int
	Data  []byte
	Hash  [32]byte
}

type Manifest struct {
	DataShards   int
	ParityShards int
	OriginalSize int
	ShardSize    int
	Shards       []ShardInfo
}

type ShardInfo struct {
	Index int
	Hash  [32]byte
}

func New(dataShards, parityShards int) (*Codec, error) {
	if dataShards <= 0 {
		return nil, errors.New("dataShards must be positive")
	}
	if parityShards <= 0 {
		return nil, errors.New("parityShards must be positive")
	}

	encoder, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return nil, err
	}
	return &Codec{
		dataShards:   dataShards,
		parityShards: parityShards,
		encoder:      encoder,
	}, nil
}

func (c *Codec) Encode(data []byte) (Manifest, []Shard, error) {
	if len(data) == 0 {
		return c.encodeEmpty()
	}

	raw, err := c.encoder.Split(data)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("split shards: %w", err)
	}
	if err := c.encoder.Encode(raw); err != nil {
		return Manifest{}, nil, fmt.Errorf("encode parity shards: %w", err)
	}

	shards := make([]Shard, len(raw))
	infos := make([]ShardInfo, len(raw))
	for i, shard := range raw {
		copyData := append([]byte(nil), shard...)
		hash := sha256.Sum256(copyData)
		shards[i] = Shard{Index: i, Data: copyData, Hash: hash}
		infos[i] = ShardInfo{Index: i, Hash: hash}
	}

	return Manifest{
		DataShards:   c.dataShards,
		ParityShards: c.parityShards,
		OriginalSize: len(data),
		ShardSize:    len(raw[0]),
		Shards:       infos,
	}, shards, nil
}

func (c *Codec) Decode(manifest Manifest, shards []Shard) ([]byte, error) {
	if err := c.validateManifest(manifest); err != nil {
		return nil, err
	}
	if manifest.OriginalSize == 0 {
		return []byte{}, nil
	}

	totalShards := c.dataShards + c.parityShards
	raw := make([][]byte, totalShards)
	valid := 0

	for _, shard := range shards {
		if shard.Index < 0 || shard.Index >= totalShards {
			continue
		}
		if raw[shard.Index] != nil {
			continue
		}
		if len(shard.Data) != manifest.ShardSize {
			continue
		}
		if sha256.Sum256(shard.Data) != manifest.Shards[shard.Index].Hash {
			continue
		}
		raw[shard.Index] = append([]byte(nil), shard.Data...)
		valid++
	}

	if valid < c.dataShards {
		return nil, ErrNotEnoughShards
	}
	if err := c.encoder.Reconstruct(raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotEnoughShards, err)
	}

	if ok, err := c.encoder.Verify(raw); err != nil {
		return nil, fmt.Errorf("verify reconstructed shards: %w", err)
	} else if !ok {
		return nil, ErrShardHash
	}

	out := make([]byte, 0, manifest.OriginalSize)
	writer := appendWriter{dst: &out}
	if err := c.encoder.Join(writer, raw, manifest.OriginalSize); err != nil {
		return nil, fmt.Errorf("join shards: %w", err)
	}
	return out, nil
}

func (c *Codec) validateManifest(manifest Manifest) error {
	if manifest.DataShards != c.dataShards {
		return fmt.Errorf("manifest data shards = %d, codec uses %d", manifest.DataShards, c.dataShards)
	}
	if manifest.ParityShards != c.parityShards {
		return fmt.Errorf("manifest parity shards = %d, codec uses %d", manifest.ParityShards, c.parityShards)
	}
	if manifest.OriginalSize < 0 {
		return errors.New("manifest original size must be non-negative")
	}
	if manifest.OriginalSize == 0 && manifest.ShardSize != 0 {
		return errors.New("manifest shard size must be zero for empty data")
	}
	if manifest.OriginalSize > 0 && manifest.ShardSize <= 0 {
		return errors.New("manifest shard size must be positive")
	}
	totalShards := c.dataShards + c.parityShards
	if manifest.OriginalSize > c.dataShards*manifest.ShardSize {
		return errors.New("manifest original size exceeds data shard capacity")
	}
	if len(manifest.Shards) != totalShards {
		return fmt.Errorf("manifest has %d shards, want %d", len(manifest.Shards), totalShards)
	}
	for i, shard := range manifest.Shards {
		if shard.Index != i {
			return fmt.Errorf("manifest shard %d has index %d", i, shard.Index)
		}
	}
	return nil
}

func (c *Codec) encodeEmpty() (Manifest, []Shard, error) {
	totalShards := c.dataShards + c.parityShards
	shards := make([]Shard, totalShards)
	infos := make([]ShardInfo, totalShards)
	hash := sha256.Sum256(nil)

	for i := range shards {
		shards[i] = Shard{Index: i, Data: []byte{}, Hash: hash}
		infos[i] = ShardInfo{Index: i, Hash: hash}
	}

	return Manifest{
		DataShards:   c.dataShards,
		ParityShards: c.parityShards,
		OriginalSize: 0,
		ShardSize:    0,
		Shards:       infos,
	}, shards, nil
}

type appendWriter struct {
	dst *[]byte
}

func (w appendWriter) Write(p []byte) (int, error) {
	*w.dst = append(*w.dst, p...)
	return len(p), nil
}

var _ io.Writer = appendWriter{}
