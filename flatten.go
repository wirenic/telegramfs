package main

import (
	"encoding/json"
	"fmt"
)

// Document is a container for a generic JSON document.
type Document map[string]interface{}

// NewDocument returns a flattened view of the input JSON document. The document is
// flattened, meaning that if the JSON document is
//
// 	{ "user": { "name": "Frank", "age": 42 } }
//
// the map will have keys "user.name", with value "Frank", and "user.age", with
// value 42.
func NewDocument(jsonString string) (Document, error) {
	var nested map[string]interface{}
	err := json.Unmarshal([]byte(jsonString), &nested)
	if err != nil {
		return nil, err
	}
	flattened := make(Document)
	flattened.recursivelyFlatten(nested, "")
	return flattened, nil
}

func (doc Document) recursivelyFlatten(nested map[string]interface{}, prefix string) {
	var longKey string
	for key, value := range nested {
		if prefix != "" {
			longKey = fmt.Sprintf("%s.%s", prefix, key)
		} else {
			longKey = key
		}
		if inner, ok := value.(map[string]interface{}); ok {
			doc.recursivelyFlatten(inner, longKey)
		} else {
			doc[longKey] = value
		}
	}
}

func (doc Document) GetBool(path string) (bool, bool) {
	iv, present := doc[path]
	if !present {
		return false, false
	}
	v, typeMatches := iv.(bool)
	return v, typeMatches
}

func (doc Document) GetFloat64(path string) (float64, bool) {
	iv, present := doc[path]
	if !present {
		return 0, false
	}
	v, typeMatches := iv.(float64)
	return v, typeMatches
}

func (doc Document) GetString(path string) (string, bool) {
	iv, present := doc[path]
	if !present {
		return "", false
	}
	v, present := iv.(string)
	return v, present
}

func (doc Document) GetInt64(path string) (int64, bool) {
	f, typeMatches := doc.GetFloat64(path)
	return int64(f), typeMatches
}
