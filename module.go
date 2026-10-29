package template

import (
	"github.com/go-core-fx/logger"
	"go.uber.org/fx"
)

const ModuleName = "template"

func Module() fx.Option {
	return fx.Module(
		ModuleName,
		logger.WithNamedLogger(ModuleName),
		// fx.Provide(New),
	)
}
