package version

var (
	// Version is set by release builds with -ldflags "-X p2pbk/internal/version.Version=vX.Y.Z".
	Version = "dev"
	// Commit is set by release builds with the current Git commit hash.
	Commit = "unknown"
	// Date is set by release builds with an RFC3339 build timestamp.
	Date = "unknown"
)

func String() string {
	if Commit == "unknown" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
