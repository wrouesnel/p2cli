package templating

import (
	"os"

	"github.com/flosch/pongo2/v6"
	"go.uber.org/zap"
)

type LoadedTemplate struct {
	Template    *pongo2.Template
	TemplateSet *pongo2.TemplateSet
}

func LoadTemplate(templatePath string) *LoadedTemplate {
	logger := zap.L()
	// Load template
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		logger.Error("Could not read template file", zap.Error(err))
		return nil
	}

	templateString := string(templateBytes)

	templateSet := pongo2.NewSet(templatePath, pongo2.DefaultLoader)

	// Load the template to parse it and get it into the cache.
	tmpl, err := templateSet.FromString(templateString)
	if err != nil {
		logger.Error("Could not template file", zap.Error(err), zap.String("template", templatePath))
		return nil
	}

	return &LoadedTemplate{
		Template:    tmpl,
		TemplateSet: templateSet,
	}
}
