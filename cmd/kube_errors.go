package cmd

import (
	"errors"
	"strings"

	"github.com/sakvarelidze/kradar/internal/kube"
)

func friendlyKubeError(err error) error {
	f := kube.ClassifyKubeError(err)
	parts := []string{f.Short}
	if strings.TrimSpace(f.Hint) != "" {
		parts = append(parts, "hint: "+f.Hint)
	}
	return errors.New(strings.Join(parts, "\n"))
}
