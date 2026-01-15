package runner

import (
	"bufio"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

var paramRegex = regexp.MustCompile(`\{\{(!)?(\w+)\}\}`)

// ParamInfo holds param name and whether it's sensitive
type ParamInfo struct {
	Name      string
	Sensitive bool
}

// ExtractParams returns all {{param}} and {{!param}} from a command string
func ExtractParams(cmd string) []ParamInfo {
	matches := paramRegex.FindAllStringSubmatch(cmd, -1)
	seen := make(map[string]bool)
	var params []ParamInfo
	for _, m := range matches {
		sensitive := m[1] == "!"
		name := m[2]
		if !seen[name] {
			seen[name] = true
			params = append(params, ParamInfo{Name: name, Sensitive: sensitive})
		}
	}
	return params
}

// SubstituteParams replaces {{param}} and {{!param}} with provided values
func SubstituteParams(cmd string, values map[string]string) string {
	result := cmd
	for name, value := range values {
		result = strings.ReplaceAll(result, "{{"+name+"}}", value)
		result = strings.ReplaceAll(result, "{{!"+name+"}}", value)
	}
	return result
}

// OutputMsg is sent through the channel for each line of output
type OutputMsg struct {
	Line   string
	IsErr  bool
	Done   bool
	ErrMsg string
}

// Run executes a command and streams output through a channel
func Run(cmd string, output chan<- OutputMsg) {
	defer close(output)

	c := exec.Command("sh", "-c", cmd)

	stdout, err := c.StdoutPipe()
	if err != nil {
		output <- OutputMsg{Done: true, ErrMsg: err.Error()}
		return
	}

	stderr, err := c.StderrPipe()
	if err != nil {
		output <- OutputMsg{Done: true, ErrMsg: err.Error()}
		return
	}

	if err := c.Start(); err != nil {
		output <- OutputMsg{Done: true, ErrMsg: err.Error()}
		return
	}

	// Stream stdout and stderr concurrently
	done := make(chan struct{}, 2)

	streamReader := func(r io.Reader, isErr bool) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			output <- OutputMsg{Line: scanner.Text(), IsErr: isErr}
		}
		done <- struct{}{}
	}

	go streamReader(stdout, false)
	go streamReader(stderr, true)

	// Wait for both streams
	<-done
	<-done

	err = c.Wait()
	if err != nil {
		output <- OutputMsg{Done: true, ErrMsg: err.Error()}
	} else {
		output <- OutputMsg{Done: true}
	}
}
