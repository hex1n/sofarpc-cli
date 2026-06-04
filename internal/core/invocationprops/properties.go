// Package invocationprops owns gateway-carried invocation property
// declarations, merge rules, redaction, and execution-time resolution.
package invocationprops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Declaration struct {
	Value    *string `json:"value,omitempty"`
	Env      string  `json:"env,omitempty"`
	Unset    bool    `json:"unset,omitempty"`
	Redacted bool    `json:"redacted,omitempty"`
}

type Declarations map[string]Declaration

type Source struct {
	Name         string
	Declarations Declarations
}

type EnvLookup func(name string) (string, bool)

type EnvStatus struct {
	Key     string `json:"key"`
	Env     string `json:"env"`
	Present bool   `json:"present"`
	Empty   bool   `json:"empty,omitempty"`
}

func (d *Declaration) UnmarshalJSON(body []byte) error {
	var raw struct {
		Value    *string `json:"value,omitempty"`
		Env      *string `json:"env,omitempty"`
		Unset    *bool   `json:"unset,omitempty"`
		Redacted *bool   `json:"redacted,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("contains multiple JSON values")
		}
		return err
	}
	d.Value = raw.Value
	if raw.Env != nil {
		d.Env = strings.TrimSpace(*raw.Env)
	}
	if raw.Unset != nil {
		d.Unset = *raw.Unset
	}
	if raw.Redacted != nil {
		d.Redacted = *raw.Redacted
	}
	return nil
}

func NormalizeInput(decls Declarations) (Declarations, error) {
	return normalize(decls, validationMode{
		allowRedacted: false,
		allowUnset:    true,
		redactEnv:     false,
	})
}

func NormalizePlan(decls Declarations) (Declarations, error) {
	return normalize(decls, validationMode{
		allowRedacted: true,
		allowUnset:    false,
		redactEnv:     true,
	})
}

func Merge(sources ...Source) (Declarations, error) {
	out := Declarations{}
	masked := map[string]struct{}{}
	for _, source := range sources {
		decls, err := NormalizeInput(source.Declarations)
		if err != nil {
			return nil, sourceError(source.Name, err)
		}
		for _, key := range SortedKeys(decls) {
			if _, ok := out[key]; ok {
				continue
			}
			if _, ok := masked[key]; ok {
				continue
			}
			decl := decls[key]
			if decl.Unset {
				masked[key] = struct{}{}
				continue
			}
			out[key] = decl
		}
	}
	return Redact(out), nil
}

func Redact(decls Declarations) Declarations {
	out := clone(decls)
	for key, decl := range out {
		if strings.TrimSpace(decl.Env) != "" {
			decl.Env = strings.TrimSpace(decl.Env)
			decl.Redacted = true
			out[key] = decl
		}
	}
	return out
}

func Resolve(decls Declarations, lookup EnvLookup) (map[string]string, error) {
	normalized, err := NormalizePlan(decls)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, key := range SortedKeys(normalized) {
		decl := normalized[key]
		switch {
		case decl.Value != nil:
			out[key] = *decl.Value
		case strings.TrimSpace(decl.Env) != "":
			if lookup == nil {
				return nil, fmt.Errorf("invocation property %q env %q cannot be resolved: no env lookup configured", key, decl.Env)
			}
			value, ok := lookup(decl.Env)
			if !ok || value == "" {
				return nil, fmt.Errorf("invocation property %q env %q is missing or empty", key, decl.Env)
			}
			out[key] = value
		}
	}
	return out, nil
}

func CheckEnv(decls Declarations, lookup EnvLookup) ([]EnvStatus, error) {
	normalized, err := NormalizePlan(decls)
	if err != nil {
		return nil, err
	}
	var out []EnvStatus
	for _, key := range SortedKeys(normalized) {
		decl := normalized[key]
		if strings.TrimSpace(decl.Env) == "" {
			continue
		}
		status := EnvStatus{Key: key, Env: decl.Env}
		if lookup != nil {
			value, ok := lookup(decl.Env)
			status.Present = ok
			status.Empty = ok && value == ""
		}
		out = append(out, status)
	}
	return out, nil
}

func SortedKeys(decls Declarations) []string {
	if len(decls) == 0 {
		return nil
	}
	keys := make([]string, 0, len(decls))
	for key := range decls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type validationMode struct {
	allowRedacted bool
	allowUnset    bool
	redactEnv     bool
}

func normalize(decls Declarations, mode validationMode) (Declarations, error) {
	if len(decls) == 0 {
		return nil, nil
	}
	out := Declarations{}
	for rawKey, decl := range decls {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return nil, fmt.Errorf("invocation property key must not be empty")
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("invocation property key %q is duplicated after trimming", key)
		}
		normalized, err := normalizeDeclaration(key, decl, mode)
		if err != nil {
			return nil, err
		}
		out[key] = normalized
	}
	return out, nil
}

func normalizeDeclaration(key string, decl Declaration, mode validationMode) (Declaration, error) {
	decl.Env = strings.TrimSpace(decl.Env)
	count := 0
	if decl.Value != nil {
		count++
	}
	if decl.Env != "" {
		count++
	}
	if decl.Unset {
		count++
	}
	if count != 1 {
		return Declaration{}, fmt.Errorf("invocation property %q must declare exactly one of value, env, or unset", key)
	}
	if decl.Env == "" && decl.Redacted {
		return Declaration{}, fmt.Errorf("invocation property %q redacted is only valid with env", key)
	}
	if decl.Env != "" && !mode.allowRedacted && decl.Redacted {
		return Declaration{}, fmt.Errorf("invocation property %q redacted is plan output only", key)
	}
	if decl.Env != "" && mode.redactEnv {
		decl.Redacted = true
	}
	if decl.Unset && !mode.allowUnset {
		return Declaration{}, fmt.Errorf("invocation property %q unset is merge-time only and is not replayable", key)
	}
	return decl, nil
}

func clone(decls Declarations) Declarations {
	if len(decls) == 0 {
		return nil
	}
	out := make(Declarations, len(decls))
	for key, decl := range decls {
		if decl.Value != nil {
			value := *decl.Value
			decl.Value = &value
		}
		out[key] = decl
	}
	return out
}

func sourceError(source string, err error) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return err
	}
	return fmt.Errorf("%s invocationProperties: %w", source, err)
}
