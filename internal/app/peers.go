package app

import (
	"errors"
	"fmt"
	"time"

	"p2pbk/internal/config"
	"p2pbk/internal/peer/localdb"
)

type PeerAdmission struct {
	PeerID        string
	FailureDomain string
	Approved      bool
	ApprovedAt    time.Time
	RevokedAt     time.Time
	Note          string
}

func ApprovePeer(cfg config.Config, peerID, failureDomain, note string) error {
	db, err := openLocalDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.ApprovePeer(peerID, failureDomain, note, time.Now().UTC())
}

func RevokePeer(cfg config.Config, peerID, note string) error {
	db, err := openLocalDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.RevokePeer(peerID, note, time.Now().UTC())
}

func ListPeerAdmissions(cfg config.Config) ([]PeerAdmission, error) {
	db, err := openLocalDB(cfg)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	approvals, err := db.ListApprovals()
	if err != nil {
		return nil, err
	}
	out := make([]PeerAdmission, 0, len(approvals))
	for _, approval := range approvals {
		out = append(out, PeerAdmission{
			PeerID:        approval.PeerID,
			FailureDomain: approval.FailureDomain,
			Approved:      approval.RevokedAt.IsZero(),
			ApprovedAt:    approval.ApprovedAt,
			RevokedAt:     approval.RevokedAt,
			Note:          approval.Note,
		})
	}
	return out, nil
}

func openLocalDB(cfg config.Config) (*localdb.DB, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, err
	}
	if paths.LocalDB == "" {
		return nil, errors.New("local database path is required")
	}
	return localdb.Open(paths.LocalDB)
}

func FormatPeerAdmission(admission PeerAdmission) string {
	state := "revoked"
	if admission.Approved {
		state = "approved"
	}
	domain := admission.FailureDomain
	if domain == "" {
		domain = "unspecified-domain"
	}
	return fmt.Sprintf("%s %s domain=%s", admission.PeerID, state, domain)
}
