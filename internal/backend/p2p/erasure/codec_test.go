package erasure

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	codec := newTestCodec(t)
	input := []byte("small backups still need to round trip")

	manifest, shards, err := codec.Encode(input)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	output, err := codec.Decode(manifest, shards)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if !bytes.Equal(output, input) {
		t.Fatalf("decoded output = %q, want %q", output, input)
	}
}

func TestEncodeDecodeEmptyBlob(t *testing.T) {
	codec := newTestCodec(t)

	manifest, shards, err := codec.Encode(nil)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if manifest.OriginalSize != 0 {
		t.Fatalf("original size = %d, want 0", manifest.OriginalSize)
	}
	if manifest.ShardSize != 0 {
		t.Fatalf("shard size = %d, want 0", manifest.ShardSize)
	}
	if len(shards) != manifest.DataShards+manifest.ParityShards {
		t.Fatalf("shard count = %d", len(shards))
	}

	output, err := codec.Decode(manifest, nil)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(output) != 0 {
		t.Fatalf("decoded output length = %d, want 0", len(output))
	}
}

func TestDecodeRecoversMissingShards(t *testing.T) {
	codec := newTestCodec(t)
	input := bytes.Repeat([]byte("p2pbk"), 128)

	manifest, shards, err := codec.Encode(input)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	available := append([]Shard(nil), shards...)
	available[1] = available[len(available)-1]
	available = available[:len(available)-1]
	available[3] = available[len(available)-1]
	available = available[:len(available)-1]

	output, err := codec.Decode(manifest, available)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if !bytes.Equal(output, input) {
		t.Fatal("decoded output did not match input after missing shards")
	}
}

func TestDecodeTreatsCorruptShardAsMissing(t *testing.T) {
	codec := newTestCodec(t)
	input := bytes.Repeat([]byte("audit"), 256)

	manifest, shards, err := codec.Encode(input)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	shards[0].Data[0] ^= 0xff

	output, err := codec.Decode(manifest, shards)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if !bytes.Equal(output, input) {
		t.Fatal("decoded output did not match input after corrupt shard")
	}
}

func TestDecodeFailsWhenTooManyShardsAreMissing(t *testing.T) {
	codec := newTestCodec(t)
	input := bytes.Repeat([]byte("missing"), 64)

	manifest, shards, err := codec.Encode(input)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	available := shards[:manifest.DataShards-1]
	_, err = codec.Decode(manifest, available)
	if !errors.Is(err, ErrNotEnoughShards) {
		t.Fatalf("Decode error = %v, want ErrNotEnoughShards", err)
	}
}

func TestDecodeRejectsMismatchedManifest(t *testing.T) {
	codec := newTestCodec(t)
	manifest, shards, err := codec.Encode([]byte("hello"))
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	manifest.DataShards = 9
	if _, err := codec.Decode(manifest, shards); err == nil {
		t.Fatal("Decode returned nil error for mismatched manifest")
	}
}

func newTestCodec(t *testing.T) *Codec {
	t.Helper()

	codec, err := New(4, 2)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return codec
}
