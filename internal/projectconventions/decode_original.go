package projectconventions

import "encoding/json"

// DecodeProjections reads original convention references and professional text.
func DecodeProjections(data []byte, out *[]Projection) error { return json.Unmarshal(data, out) }

func decodeOriginalRegistry(data []byte, out *Registry) error { return json.Unmarshal(data, out) }
