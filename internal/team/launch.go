package team

type DocumentReference struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	ReadOrder int    `json:"read_order"`
}
