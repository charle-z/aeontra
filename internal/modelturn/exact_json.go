package modelturn

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func exactJSON(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.Equal(raw, trimmed) {
		return nil, errors.New("canonical payload must not contain surrounding whitespace")
	}
	if len(trimmed) == 0 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, errors.New("canonical payload must be one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	canonical, err := marshalCanonical(value)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, trimmed) {
		return nil, errors.New("payload is valid JSON but is not canonical")
	}
	return append([]byte(nil), trimmed...), nil
}

func marshalCanonical(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buffer.String(), "\n")), nil
}
