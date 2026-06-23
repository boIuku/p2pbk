package localdb

type Bucket string

const (
	BucketSettings      Bucket = "settings"
	BucketPeers         Bucket = "peers"
	BucketOwnedBlobs    Bucket = "owned_blobs"
	BucketShardReceipts Bucket = "shard_receipts"
	BucketAuditHistory  Bucket = "audit_history"
)

var RequiredBuckets = []Bucket{
	BucketSettings,
	BucketPeers,
	BucketOwnedBlobs,
	BucketShardReceipts,
	BucketAuditHistory,
}
