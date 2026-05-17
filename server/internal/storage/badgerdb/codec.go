package badgerdb

import "encoding/json"

// EncodeJSON marshals v with stable semantics for KV storage.
func EncodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// DecodeJSON unmarshals JSON into dest.
func DecodeJSON(data []byte, dest any) error {
	return json.Unmarshal(data, dest)
}
