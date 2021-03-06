package ravendb

// ClientConfiguration represents client configuration
type ClientConfiguration struct {
	Etag       int64 `json:"Etag"`
	IsDisabled bool  `json:"Disabled"`
	// TODO: should this be *int ?
	MaxNumberOfRequestsPerSession int                 `json:"MaxNumberOfRequestsPerSession"`
	ReadBalanceBehavior           ReadBalanceBehavior `json:"ReadBalanceBehavior"`
}
