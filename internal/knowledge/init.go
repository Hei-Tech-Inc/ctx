package knowledge

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
)

//go:embed templates/*.yaml
var templates embed.FS

// Init scaffolds the knowledge directory: three commented YAML templates and
// the recipes/ and scripts/ directories. Existing files are never touched.
// It returns the paths it created.
func Init(root string) ([]string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	var created []string
	for _, name := range []string{"clusters.yaml", "environments.yaml", "services.yaml"} {
		data, err := templates.ReadFile("templates/" + name)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(root, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return nil, werr
		}
		created = append(created, path)
	}
	for _, d := range []string{"recipes", "scripts"} {
		path := filepath.Join(root, d)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
		created = append(created, path)
	}
	return created, nil
}
