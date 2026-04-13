package runtime

import (
	"fmt"
	"os/exec"
	"strings"
)

func detectJavaMajor(javaBin string) (string, error) {
	output, err := exec.Command(javaBin, "-version").CombinedOutput()
	if err != nil {
		return "", err
	}
	text := string(output)
	start := strings.IndexByte(text, '"')
	if start < 0 {
		return "", fmt.Errorf("unable to parse java version output")
	}
	end := strings.IndexByte(text[start+1:], '"')
	if end < 0 {
		return "", fmt.Errorf("unable to parse java version output")
	}
	version := text[start+1 : start+1+end]
	if strings.HasPrefix(version, "1.") {
		parts := strings.Split(version, ".")
		if len(parts) > 1 {
			return parts[1], nil
		}
	}
	return strings.Split(version, ".")[0], nil
}
