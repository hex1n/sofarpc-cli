package cli

import (
	appdescribe "github.com/hex1n/sofarpc-cli/internal/app/describe"
	appfacade "github.com/hex1n/sofarpc-cli/internal/app/facade"
	appinvoke "github.com/hex1n/sofarpc-cli/internal/app/invoke"
	appsession "github.com/hex1n/sofarpc-cli/internal/app/session"
	apptarget "github.com/hex1n/sofarpc-cli/internal/app/target"
	"github.com/hex1n/sofarpc-cli/internal/config"
)

func (a *App) WorkingDir() string {
	return a.Cwd
}

func (a *App) ConfigPaths() config.Paths {
	return a.Paths
}

func (a *App) SessionService() appsession.Deps {
	return a.newSessionService()
}

func (a *App) TargetService() apptarget.Deps {
	return a.newTargetService()
}

func (a *App) DescribeService() appdescribe.Deps {
	return a.newDescribeResolver()
}

func (a *App) InvokeService() appinvoke.Deps {
	return a.newInvokeExecutor()
}

func (a *App) FacadeService() appfacade.Deps {
	return a.newFacadeService()
}
