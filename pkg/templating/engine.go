package templating

import (
	"io"

	"github.com/flosch/pongo2/v4"
	"github.com/pkg/errors"
)

type TemplateEngine struct {
	// PrepareOutput is invoked before the engine writes a file and must return a writer
	// which directs the byte output to correct location, and a finalizer function which
	// can be called to finish the write operation.
	PrepareOutput func(inputData pongo2.Context, outputPath string) (io.Writer, func() error, error)
}

func (te *TemplateEngine) ExecuteTemplate(filterSet *FilterSet, tmpl *LoadedTemplate,
	inputData pongo2.Context, outputPath string) error {
	outputWriter, finalizer, err := te.PrepareOutput(inputData, outputPath)
	if err != nil {
		return errors.Wrap(err, "ExecuteTemplate")
	}

	ctx := make(pongo2.Context)
	ctx.Update(inputData)

	// Parallelism risk! This is a really hacky way to implement this functionality,
	// and creates all sorts of problems if this is ever used concurrently. We need
	// changes to Pongo2 (ideally per templateset filters) in order to avoid this.
	filterSet.OutputFileName = outputPath
	if err := tmpl.Template.ExecuteWriter(ctx, outputWriter); err != nil {
		return errors.Wrap(err, "ExecuteTemplate template error")
	}

	if finalizer != nil {
		if err := finalizer(); err != nil {
			return errors.Wrap(err, "ExecuteTemplate finalizer error")
		}
	}

	return nil
}
