package main

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"reflect"
	"sort"
	"sra/vat"
)

type ChunkedHash struct {
	data     []byte
	dataHash []byte
}

type StreamingHasher struct {
	hasher hash.Hash
	Chunks []*ChunkedHash
	Final  []byte
}

func NewStreamingHasher(hasher hash.Hash) *StreamingHasher {
	s := &StreamingHasher{
		hasher: hasher,
		Chunks: make([]*ChunkedHash, 0, 10),
	}

	return s
}

func (sh *StreamingHasher) Write(p []byte) (int, error) {
	c := &ChunkedHash{
		data: make([]byte, len(p)),
	}
	copy(c.data, p)
	n, err := sh.hasher.Write(p)
	if err != nil {
		return n, err
	}
	c.dataHash = sh.hasher.Sum(nil)
	sh.Chunks = append(sh.Chunks, c)
	return n, nil

}

func (sh *StreamingHasher) Sum(p []byte) []byte {
	sh.Final = sh.hasher.Sum(p)
	r := make([]byte, len(sh.Final))
	copy(r, sh.Final)
	return r
}

// HashType generates a hash for the given type using reflection.
// This version includes handling for structs, slices, arrays, maps,
// pointers, interfaces, funcs, and channels.  It sorts struct fields
// alphabetically to avoid sensitivity to field order.
func HashType(t reflect.Type) (*StreamingHasher, error) {
	hasher := sha256.New()
	sh := NewStreamingHasher(hasher)
	err := hashTypeRecursive(t, sh)
	if err != nil {
		return nil, err
	}
	sh.Sum(nil)
	return sh, nil
}

func hashTypeRecursive(t reflect.Type, hasher interface{ Write([]byte) (int, error) }) error {
	_, err := hasher.Write([]byte(t.String())) // Include the type name
	if err != nil {
		return err
	}

	switch t.Kind() {
	case reflect.Struct:
		// Sort fields alphabetically to avoid sensitivity to field order
		fields := make([]string, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			fields[i] = t.Field(i).Name
		}
		sort.Strings(fields)

		for _, fieldName := range fields {
			_, err := hasher.Write([]byte(fieldName))
			if err != nil {
				return err
			}
			field, _ := t.FieldByName(fieldName) // err is always nil when the name is from t.
			err = hashTypeRecursive(field.Type, hasher)
			if err != nil {
				return err
			}
		}

	case reflect.Slice, reflect.Array:
		if err := hashTypeRecursive(t.Elem(), hasher); err != nil {
			return err
		}

	case reflect.Ptr:
		if err := hashTypeRecursive(t.Elem(), hasher); err != nil {
			return err
		}

	case reflect.Map:
		if err := hashTypeRecursive(t.Key(), hasher); err != nil {
			return err
		}
		if err := hashTypeRecursive(t.Elem(), hasher); err != nil {
			return err
		}

	case reflect.Interface:
		//todo: revisit implementation.
		_, err = hasher.Write([]byte(t.String()))
		if err != nil {
			return err
		}

	default:
		// Basic types (int, string, bool, etc.) are handled by writing the type name.
		// You might consider adding more specific handling if needed.
	}
	return nil
}

func main() {

	t := reflect.TypeOf(vat.AssessmentData{})
	sh, err := HashType(t)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	for _, c := range sh.Chunks {
		fmt.Printf("%s\n", c.data)
	}
	fmt.Printf("finalized: %x\n", sh.Final)
}
