package parsercorephase0

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
)

// ScannerFingerprint identifies a scanner value without formatting its token
// tables on every owned lexer operation. Pointer scanners are identified by
// their address: their mutable payload belongs in scanner checkpoints. Value
// scanners include array and slice contents, so replacing or mutating a binding
// still invalidates an existing contract. The caller also compares reflect.Type.
func ScannerFingerprint(scanner any) [32]byte {
	if scanner == nil {
		return [32]byte{}
	}
	value := reflect.ValueOf(scanner)
	if value.Kind() == reflect.Pointer {
		// Type is compared separately. The address is already an exact identity;
		// hashing it adds work without distinguishing any additional instance.
		var identity [32]byte
		identity[0] = byte(reflect.Pointer)
		binary.LittleEndian.PutUint64(identity[1:9], uint64(value.Pointer()))
		return identity
	}
	var buffer [4096]byte
	encoded, ok := appendScannerIdentity(buffer[:0], value)
	if !ok {
		// Preserve the existing identity rule for unusual scanner values such as
		// maps. In particular, fmt sorts map keys rather than using range order.
		return sha256.Sum256([]byte(fmt.Sprintf("%T:%#v", scanner, scanner)))
	}
	return sha256.Sum256(encoded)
}

func appendScannerIdentity(dst []byte, value reflect.Value) ([]byte, bool) {
	if !value.IsValid() {
		return append(dst, byte(reflect.Invalid)), true
	}
	dst = append(dst, byte(value.Kind()))
	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			return append(dst, 1), true
		}
		return append(dst, 0), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return binary.LittleEndian.AppendUint64(dst, uint64(value.Int())), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return binary.LittleEndian.AppendUint64(dst, value.Uint()), true
	case reflect.Float32, reflect.Float64:
		return binary.LittleEndian.AppendUint64(dst, math.Float64bits(value.Float())), true
	case reflect.Complex64, reflect.Complex128:
		v := value.Complex()
		dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(real(v)))
		return binary.LittleEndian.AppendUint64(dst, math.Float64bits(imag(v))), true
	case reflect.String:
		v := value.String()
		dst = binary.LittleEndian.AppendUint64(dst, uint64(len(v)))
		return append(dst, v...), true
	case reflect.Pointer, reflect.UnsafePointer, reflect.Chan:
		return binary.LittleEndian.AppendUint64(dst, uint64(value.Pointer())), true
	case reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice {
			if value.IsNil() {
				return append(dst, 0), true
			}
			dst = append(dst, 1)
		}
		dst = binary.LittleEndian.AppendUint64(dst, uint64(value.Len()))
		for i := 0; i < value.Len(); i++ {
			var ok bool
			dst, ok = appendScannerIdentity(dst, value.Index(i))
			if !ok {
				return nil, false
			}
		}
		return dst, true
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			var ok bool
			dst, ok = appendScannerIdentity(dst, value.Field(i))
			if !ok {
				return nil, false
			}
		}
		return dst, true
	default:
		return nil, false
	}
}
