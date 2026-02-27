package xdscenter

import "time"

type RouteRecord struct {
	Host             string    `json:"host"`
	Label            string    `json:"label"`
	VMID             string    `json:"vmID"`
	GuestIP          string    `json:"guestIP"`
	GuestPort        int       `json:"guestPort"`
	NetNS            string    `json:"netns"`
	NetNSPath        string    `json:"netnsPath"`
	TargetAddr       string    `json:"targetAddr"`
	ClusterName      string    `json:"clusterName"`
	CreatedAt        time.Time `json:"createdAt,omitempty"`
	Source           string    `json:"source"`
	ResolvedAt       time.Time `json:"resolvedAt"`
	ResolverStrategy string    `json:"resolverStrategy"`
}

type RoutesResponse struct {
	Domain string        `json:"domain"`
	Count  int           `json:"count"`
	Routes []RouteRecord `json:"routes"`
}

type ConsulSyncResult struct {
	Enabled bool   `json:"enabled"`
	Count   int    `json:"count"`
	Prefix  string `json:"prefix"`
}
