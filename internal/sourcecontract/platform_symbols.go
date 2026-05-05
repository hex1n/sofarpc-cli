package sourcecontract

import "strings"

//go:generate go run generate_jdk8_symbols.go

type symbolTable map[string]map[string]string

func newProjectSymbolTable(index map[string]string) symbolTable {
	if len(index) == 0 {
		return nil
	}
	table := symbolTable{}
	for fqn := range index {
		pkg, simple := splitPackageAndSimple(fqn)
		if simple == "" {
			continue
		}
		if table[pkg] == nil {
			table[pkg] = map[string]string{}
		}
		if _, exists := table[pkg][simple]; !exists {
			table[pkg][simple] = fqn
		}
	}
	return table
}

func (t symbolTable) lookup(pkg, simple string) (string, bool) {
	if len(t) == 0 || simple == "" {
		return "", false
	}
	bySimple, ok := t[pkg]
	if !ok {
		return "", false
	}
	fqn, ok := bySimple[simple]
	return fqn, ok
}

func lookupJDK8PlatformSymbol(pkg, simple string) (string, bool) {
	if simple == "" {
		return "", false
	}
	bySimple, ok := jdk8PlatformSymbols[pkg]
	if !ok {
		return "", false
	}
	fqn, ok := bySimple[simple]
	return fqn, ok
}

func splitPackageAndSimple(fqn string) (string, string) {
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return "", ""
	}
	idx := strings.LastIndexByte(fqn, '.')
	if idx < 0 {
		return "", fqn
	}
	return fqn[:idx], fqn[idx+1:]
}
