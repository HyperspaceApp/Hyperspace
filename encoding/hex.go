package encoding

import (
	"encoding/hex"
	"errors"
)

//HexStringToBytes converts a hex encoded string (but as go type interface{}) to a byteslice
// If v is no valid string or the string contains invalid characters, an error is returned
func HexStringToBytes(v interface{}) (result []byte, err error) {
	var ok bool
	var stringValue string
	if stringValue, ok = v.(string); !ok {
		return nil, errors.New("Not a valid string")
	}
	if result, err = hex.DecodeString(stringValue); err != nil {
		return nil, errors.New("Not a valid hexadecimal value")
	}
	return
}

// BytesToHexString converts a byte slice to a hex encoded string
func BytesToHexString(bytes []byte) (result string) {
	return hex.EncodeToString(bytes)
}
