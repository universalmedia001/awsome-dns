package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func LoadDatabase(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read domain database: %w", err)
	}

	var records []Record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode domain database: %w", err)
	}
	return records, nil
}

func SaveDatabase(path string, records []Record) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create domain database directory: %w", err)
	}

	file, err := os.CreateTemp(dir, ".domains-*.json")
	if err != nil {
		return fmt.Errorf("create temporary domain database: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(records); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode domain database: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync domain database: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close domain database: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace domain database: %w", err)
	}
	return nil
}
