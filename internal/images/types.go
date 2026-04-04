package images

import "time"

const (
	ArtifactKindBase    = "base"
	ArtifactKindPayload = "payload"
)

type LocalCatalog struct {
	Base     BaseArtifact      `json:"base"`
	Payloads []PayloadArtifact `json:"payloads"`
}

type BaseArtifact struct {
	Name               string      `json:"name"`
	Directory          string      `json:"directory"`
	Files              []LocalFile `json:"files"`
	Ready              bool        `json:"ready"`
	LastModified       *time.Time  `json:"lastModified,omitempty"`
	RemoteManifestPath string      `json:"remoteManifestPath,omitempty"`
}

type PayloadArtifact struct {
	ImageRef           string      `json:"imageRef"`
	Directory          string      `json:"directory"`
	Files              []LocalFile `json:"files"`
	Ready              bool        `json:"ready"`
	LastModified       *time.Time  `json:"lastModified,omitempty"`
	RemoteManifestPath string      `json:"remoteManifestPath,omitempty"`
}

type LocalFile struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	SizeBytes    int64     `json:"sizeBytes,omitempty"`
	LastModified time.Time `json:"lastModified"`
}

type Manifest struct {
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Username   string         `json:"username"`
	UploadedAt time.Time      `json:"uploadedAt"`
	Platform   string         `json:"platform,omitempty"`
	Files      []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Name      string `json:"name"`
	Key       string `json:"key"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256,omitempty"`
}

type SyncSummary struct {
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Bucket      string         `json:"bucket"`
	Prefix      string         `json:"prefix"`
	ManifestKey string         `json:"manifestKey"`
	LocalDir    string         `json:"localDir"`
	Transferred []ManifestFile `json:"transferred"`
	CompletedAt time.Time      `json:"completedAt"`
}
