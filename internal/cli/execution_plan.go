package cli

func workerClasspathMode(stubPaths []string) string {
	if len(stubPaths) == 0 {
		return "runtime-only"
	}
	return "runtime+stubs"
}

func contractSourceLabel(source string) string {
	return source
}
