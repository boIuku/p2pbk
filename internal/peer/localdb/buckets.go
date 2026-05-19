package localdb

type Bucket string

const (
	BucketSettings         Bucket = "settings"
	BucketPeers            Bucket = "peers"
	BucketOwnedBlobs       Bucket = "owned_blobs"
	BucketReceipts         Bucket = "receipts"
	BucketChallengeHistory Bucket = "challenge_history"
)

var RequiredBuckets = []Bucket{
	BucketSettings,
	BucketPeers,
	BucketOwnedBlobs,
	BucketReceipts,
	BucketChallengeHistory,
}
