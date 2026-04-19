package cli

import (
	appshared "github.com/hex1n/sofarpc-cli/internal/app/shared"
	apptarget "github.com/hex1n/sofarpc-cli/internal/app/target"
)

type invocationInputs = appshared.InvocationInputs
type resolvedInvocation = appshared.ResolvedInvocation
type localSchemaResolution = appshared.LocalSchemaResolution
type serviceSchemaResolution = appshared.ServiceSchemaResolution

type targetReport = apptarget.Report
type targetCandidate = apptarget.Candidate
type targetLayer = apptarget.Layer
