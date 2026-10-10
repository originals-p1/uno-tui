package client

import (
	"encoding/json"
	"strconv"
)

func jsonMarshal(v interface{}) ([]byte, error) { return json.Marshal(v) }

func jsonUnmarshal(b []byte, v interface{}) error { return json.Unmarshal(b, v) }

func intToStr(n int64) string { return strconv.FormatInt(n, 10) }

// jsonUnmarshalPayload is reserved placeholder for future use.
var _ = json.Marshal
