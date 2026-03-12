package ext

import (
	"fmt"

	"github.com/warpdl/warpdl/pkg/warpcli"
)

// resolveExtensionID accepts either an exact extension ID or an exact name.
// If multiple extensions share the same name, the caller must use the ID.
func resolveExtensionID(client *warpcli.Client, value string) (string, error) {
	exts, err := client.ListExtension(true)
	if err != nil {
		return "", err
	}

	var nameMatches []string
	for _, ext := range *exts {
		if ext.ExtensionId == value {
			return value, nil
		}
		if ext.Name == value {
			nameMatches = append(nameMatches, ext.ExtensionId)
		}
	}

	switch len(nameMatches) {
	case 0:
		return value, nil
	case 1:
		return nameMatches[0], nil
	default:
		return "", fmt.Errorf("multiple extensions named %q found; use the unique hash", value)
	}
}
