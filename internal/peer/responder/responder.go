package responder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"p2pbk/internal/network/wire"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/challenges"
	"p2pbk/internal/trust/reputation"
)

const (
	MethodChallenge = "responder.challenge"
)

type Responder struct {
	blobserver    *blobserver.Server
	localdb       *localdb.DB
	reputation    *reputation.Tracker
	peerID        peer.ID
	reputationKey string
}

type ResponderOption func(*Responder)

func WithPeerID(p peer.ID) ResponderOption {
	return func(r *Responder) {
		r.peerID = p
	}
}

func WithReputationKey(key string) ResponderOption {
	return func(r *Responder) {
		r.reputationKey = key
	}
}

func New(bs *blobserver.Server, db *localdb.DB, rep *reputation.Tracker, opts ...ResponderOption) *Responder {
	r := &Responder{
		blobserver:    bs,
		localdb:       db,
		reputation:    rep,
		peerID:        peer.ID(""),
		reputationKey: "",
	}
	for _, opt := range opts {
		opt(r)
	}
	if len(r.reputationKey) == 0 {
		r.reputationKey = fmt.Sprintf("%s", r.peerID)
	}
	return r
}

func (r *Responder) Answer(ctx context.Context, ch challenges.Challenge) (challenges.Response, error) {
	got, err := r.blobserver.GetShard(ctx, blobserver.GetShardRequest{Ref: ch.Ref})
	if err != nil {
		return challenges.Response{}, err
	}

	// Record success in reputation
	r.reputation.RecordSuccess(r.peerID.String(), ch.Ref.Owner, ch.Ref.BlobID, uint32(ch.Ref.Index), ch.Nonce, time.Now())

	return challenges.Answer(ch, got.Data)
}

func (r *Responder) Register(router *wire.Router) {
	router.Handle(MethodChallenge, r.handleChallenge)
}

func (r *Responder) handleChallenge(ctx context.Context, req wire.Request) (wire.Response, error) {
	var ch challenges.Challenge
	if err := json.Unmarshal(req.Payload, &ch); err != nil {
		return wire.Response{}, err
	}
	if p := wire.RemotePeer(ctx); p == "" || ch.Ref.Owner != p.String() {
		return wire.Response{}, blobserver.ErrUnauthorized
	}

	// Log incoming challenge
	fmt.Printf("received challenge from %s: ref=%+v nonce=%s\n", req.Method, ch.Ref, ch.Nonce)

	resp, err := r.Answer(ctx, ch)
	if err != nil {
		return wire.Response{}, err
	}

	return wire.OK(resp)
}

// VerifyAndRecord processes a challenge response received from another peer.
// Returns success/failure and updated reputation score.
func (r *Responder) VerifyAndRecord(ctx context.Context, owner, blobID string, index uint32,
	nonce string, proof string, peerID peer.ID, shard []byte) (bool, error) {

	// Create a synthetic challenge for verification
	ref := blobserver.ShardRef{Owner: owner, BlobID: blobID, Index: int(index)}
	ch := challenges.Challenge{
		Ref:    ref,
		Nonce:  nonce,
		Offset: 0,
		Length: int64(len(shard)),
	}

	// Verify the proof
	expectedProof, err := challenges.Proof(ch, bytes.NewReader(shard))
	if err != nil {
		return false, fmt.Errorf("generate proof: %w", err)
	}

	success := proof == expectedProof

	// Record in reputation
	if success {
		r.reputation.RecordSuccess(peerID.String(), owner, blobID, index, nonce, time.Now())
		fmt.Printf("challenge success for peer %s: ref=%+v\n", peerID, ch.Ref)
	} else {
		r.reputation.RecordFailure(peerID.String(), owner, blobID, index, nonce, time.Now())
		fmt.Printf("challenge failure for peer %s: ref=%+v\n", peerID, ch.Ref)
	}

	return success, nil
}
