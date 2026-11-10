package format

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/chimanjain/gomajor/pkg/model"
	"go.yaml.in/yaml/v4"
)

// YAMLOutput defines the structured schema for the saved YAML output.
type YAMLOutput struct {
	Results []model.SourceResult `yaml:"results" json:"results"`
}

// WriteReport serializes results to cleanPath as JSON or YAML and writes atomically to disk.
func WriteReport(w io.Writer, outputPath string, isJSON bool, results []model.SourceResult) error {
	cleanPath := filepath.Clean(outputPath)

	outputData := YAMLOutput{Results: results}
	formatName := "YAML"
	data, err := yaml.Marshal(outputData)
	if isJSON {
		formatName = "JSON"
		data, err = json.MarshalIndent(outputData, "", "  ")
	}

	if err == nil {
		err = safeWriteReportFile(cleanPath, data)
	}
	if err != nil {
		return fmt.Errorf("failed to write %s output: %w", formatName, err)
	}
	fmt.Fprintf(w, "Results written to %s file: %s\n", formatName, cleanPath)
	return nil
}

// PrintMultiJSONResults serializes results as indented JSON to the provided writer.
func PrintMultiJSONResults(w io.Writer, results []model.SourceResult) error {
	outputData := YAMLOutput{Results: results}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(outputData)
}

func safeWriteReportFile(cleanPath string, data []byte) error {
	if fi, err := os.Lstat(cleanPath); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("refusing to write report to symlink: %s", cleanPath)
	}

	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, ".gomajor-report-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}

	// Double check before atomic rename to ensure target wasn't replaced with symlink
	if fi, err := os.Lstat(cleanPath); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("refusing to write report to symlink: %s", cleanPath)
	}

	err = os.Rename(tmpName, cleanPath)
	if err != nil && runtime.GOOS == "windows" {
		_ = os.Remove(cleanPath)
		err = os.Rename(tmpName, cleanPath)
	}
	return err
}
