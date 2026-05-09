package parse

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/gmmakoele-ship-it/rafikiclaw/internal/claw/schema/v1"
	"gopkg.in/yaml.v3"
)

func File(path string) (v1.Clawfile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return v1.Clawfile{}, fmt.Errorf("read clawfile: %w", err)
	}
	var cfg v1.Clawfile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return v1.Clawfile{}, fmt.Errorf("parse yaml (%s): %w", filepath.Base(path), err)
	}
	return cfg, nil
}
