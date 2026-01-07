package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const rekorHashedrekordKind = "hashedrekord"

type RekorHashedrekord struct {
	APIVersion *string         `json:"apiVersion"`
	Spec       json.RawMessage `json:"spec"`
}

func (m *RekorHashedrekord) Kind() string {
	return rekorHashedrekordKind
}

func (m *RekorHashedrekord) SetKind(val string) {
}

func (m *RekorHashedrekord) UnmarshalJSON(raw []byte) error {
	var base struct {
		Kind string `json:"kind"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&base); err != nil {
		return err
	}

	switch base.Kind {
	case rekorHashedrekordKind:
		var data struct { // We can’t use RekorHashedRekord directly, because that would be an infinite recursion.
			APIVersion *string         `json:"apiVersion"`
			Spec       json.RawMessage `json:"spec"`
		}
		dec = json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			return err
		}
		res := RekorHashedrekord{
			APIVersion: data.APIVersion,
			Spec:       data.Spec,
		}
		*m = res
		return nil

	default:
		return fmt.Errorf("invalid kind value: %q", base.Kind)
	}
}

func (m RekorHashedrekord) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind       string          `json:"kind"`
		APIVersion *string         `json:"apiVersion"`
		Spec       json.RawMessage `json:"spec"`
	}{
		Kind:       m.Kind(),
		APIVersion: m.APIVersion,
		Spec:       m.Spec,
	})
}

type RekorHashedrekordV001Schema struct {
	Data      *RekorHashedrekordV001SchemaData      `json:"data"`
	Signature *RekorHashedrekordV001SchemaSignature `json:"signature"`
}

type RekorHashedrekordV001SchemaData struct {
	Hash *RekorHashedrekordV001SchemaDataHash `json:"hash,omitempty"`
}

type RekorHashedrekordV001SchemaDataHash struct {
	Algorithm *string `json:"algorithm"`
	Value     *string `json:"value"`
}

const (
	RekorHashedrekordV001SchemaDataHashAlgorithmSha256 string = "sha256"
	RekorHashedrekordV001SchemaDataHashAlgorithmSha384 string = "sha384"
	RekorHashedrekordV001SchemaDataHashAlgorithmSha512 string = "sha512"
)

type RekorHashedrekordV001SchemaSignature struct {
	Content   []byte                                         `json:"content,omitempty"`
	PublicKey *RekorHashedrekordV001SchemaSignaturePublicKey `json:"publicKey,omitempty"`
}

type RekorHashedrekordV001SchemaSignaturePublicKey struct {
	Content []byte `json:"content,omitempty"`
}
