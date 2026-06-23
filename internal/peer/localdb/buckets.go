package localdb

type Bucket string

const (
	BucketSettings   Bucket = "settings"
	BucketPeers      Bucket = "peers"
	BucketOwnedBlobs Bucket = "owned_blobs"
)

var RequiredBuckets = []Bucket{
	BucketSettings,
	BucketPeers,
	BucketOwnedBlobs,
}
