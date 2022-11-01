package templating

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/flosch/pongo2/v4"
	"github.com/pkg/errors"
	"github.com/wrouesnel/p2cli/pkg/fileconsts"
	"go.uber.org/zap"
)

type TemplateEngine struct {
	StdOut io.Writer
}

func (te *TemplateEngine) ExecuteTemplate(filterSet *FilterSet, tmpl *LoadedTemplate,
	inputData pongo2.Context, outputPath string) error {
	logger := zap.L()
	cwd, err := os.Getwd()
	if err != nil {
		logger.Error("Could not get the current working directory", zap.Error(err))
	}

	var outputWriter io.Writer
	if outputPath != "" {
		fileOut, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(fileconsts.OS_ALL_RWX))
		if err != nil {
			return errors.Wrap(err, "ExecuteTemplate: error opening output file for writing")
		}
		defer func() { _ = fileOut.Close() }()
		outputWriter = io.Writer(fileOut)

		if err := os.Chdir(filepath.Dir(outputPath)); err != nil {
			return fmt.Errorf("could not change to template output path directory: %w", err)
		}
	} else {
		outputWriter = te.StdOut
	}

	ctx := make(pongo2.Context)
	ctx.Update(inputData)

	// Parallelism risk! This is a really hacky way to implement this functionality,
	// and creates all sorts of problems if this is ever used concurrently. We need
	// changes to Pongo2 (ideally per templateset filters) in order to avoid this.
	filterSet.OutputFileName = outputPath
	terr := tmpl.Template.ExecuteWriter(ctx, outputWriter)

	if err := os.Chdir(cwd); err != nil {
		return fmt.Errorf("could not change back to original working directory: %w", err)
	}

	return errors.Wrap(terr, "ExecuteTemplate")
}
