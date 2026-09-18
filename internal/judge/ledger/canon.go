package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

func Canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := canonicalWrite(&buf, tree); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Hash(value any) (string, error) {
	canon, err := Canonical(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalWrite(buf *bytes.Buffer, node any) error {
	switch value := node.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if value {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case string:
		text, err := json.Marshal(value)
		if err != nil {
			return err
		}
		buf.Write(text)
		return nil
	case json.Number:
		return canonicalNumber(buf, value)
	case []any:
		buf.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := canonicalWrite(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case map[string]any:
		keysInByteOrder := make([]string, 0, len(value))
		for key := range value {
			keysInByteOrder = append(keysInByteOrder, key)
		}
		sort.Strings(keysInByteOrder)
		buf.WriteByte('{')
		for i, key := range keysInByteOrder {
			if i > 0 {
				buf.WriteByte(',')
			}
			text, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(text)
			buf.WriteByte(':')
			if err := canonicalWrite(buf, value[key]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("ledger: canonical json met %T", node)
	}
}

func canonicalNumber(buf *bytes.Buffer, number json.Number) error {
	if whole, err := number.Int64(); err == nil {
		buf.WriteString(strconv.FormatInt(whole, 10))
		return nil
	}
	shortestRoundTrip, err := number.Float64()
	if err != nil {
		return fmt.Errorf("ledger: canonical json number %q: %w", number.String(), err)
	}
	buf.WriteString(strconv.FormatFloat(shortestRoundTrip, 'g', -1, 64))
	return nil
}
