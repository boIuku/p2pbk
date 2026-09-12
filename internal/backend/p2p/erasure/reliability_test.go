package erasure

import (
	"bytes"
	"errors"
	"testing"
)

// The advertised 6+3 guarantee is any SIX valid shards, not just one chosen
// missing parity shard. Exhaust every six-of-nine combination (84 cases).
func TestDefaultPolicyEveryRecoverableShardSubset(t *testing.T) {
	c, err := New(6, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("independent recovery oracle"), 101)
	m, shards, err := c.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for mask := 0; mask < 1<<9; mask++ {
		var selected []Shard
		for i, s := range shards {
			if mask&(1<<i) != 0 {
				selected = append(selected, s)
			}
		}
		if len(selected) != 6 {
			continue
		}
		count++
		got, err := c.Decode(m, selected)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("subset %09b failed: %v", mask, err)
		}
		selected = append([]Shard(nil), selected...)
		selected[0].Data = append([]byte(nil), selected[0].Data...)
		selected[0].Data[0] ^= 255
		if _, err = c.Decode(m, selected); !errors.Is(err, ErrNotEnoughShards) {
			t.Fatalf("subset %09b accepted only five valid shards: %v", mask, err)
		}
	}
	if count != 84 {
		t.Fatalf("tested %d combinations", count)
	}
}

// Fuzzing explores payload length, shard policy and loss independently of the
// encoder's implementation. Bounded inputs keep this suitable for scheduled CI.
func FuzzErasureRecovery(f *testing.F) {
	f.Add([]byte("backup"), uint8(6), uint8(3), uint16(7))
	f.Add([]byte{}, uint8(2), uint8(1), uint16(0))
	f.Fuzz(func(t *testing.T, data []byte, k, p uint8, mask uint16) {
		if len(data) > 1<<18 {
			return
		}
		dataShards := int(k%8) + 1
		parity := int(p%4) + 1
		c, err := New(dataShards, parity)
		if err != nil {
			t.Fatal(err)
		}
		m, shards, err := c.Encode(data)
		if err != nil {
			t.Fatal(err)
		}
		var available []Shard
		for i, s := range shards {
			if mask&(1<<i) == 0 {
				available = append(available, s)
			}
		}
		got, err := c.Decode(m, available)
		if len(data) == 0 || len(available) >= dataShards {
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("recoverable payload changed: %v", err)
			}
		} else if !errors.Is(err, ErrNotEnoughShards) {
			t.Fatalf("insufficient shards accepted: %v", err)
		}
	})
}
