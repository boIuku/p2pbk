package p2p

type Options struct {
	Owner        string
	DataShards   int
	ParityShards int
	DisplayName  string
	ManifestPath string
}

func (o Options) withDefaults() Options {
	if o.Owner == "" {
		o.Owner = "local"
	}
	if o.DataShards == 0 {
		o.DataShards = 6
	}
	if o.ParityShards == 0 {
		o.ParityShards = 3
	}
	if o.DisplayName == "" {
		o.DisplayName = "p2pbk"
	}
	return o
}
