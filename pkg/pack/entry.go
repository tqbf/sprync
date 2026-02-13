package pack

type ManifestEntry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Mode int    `json:"mode"`
	Size int64  `json:"size"`
}

type Manifest map[string]ManifestEntry
