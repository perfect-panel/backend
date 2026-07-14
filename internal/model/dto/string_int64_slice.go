package dto

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

type StringInt64Slice []int64

func (s *StringInt64Slice) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*s = nil
		return nil
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	values := make([]int64, 0, len(raw))
	for _, item := range raw {
		var number int64
		if err := json.Unmarshal(item, &number); err == nil {
			values = append(values, number)
			continue
		}

		var text string
		if err := json.Unmarshal(item, &text); err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		values = append(values, parsed)
	}

	*s = values
	return nil
}

func (s StringInt64Slice) MarshalJSON() ([]byte, error) {
	values := make([]string, 0, len(s))
	for _, item := range s {
		values = append(values, strconv.FormatInt(item, 10))
	}
	return json.Marshal(values)
}

func (s StringInt64Slice) Int64s() []int64 {
	return []int64(s)
}
