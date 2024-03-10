package filters

import (
	"bytes"
	"context"
	"io"
	"os/exec"

	"golang.org/x/exp/slog"
)

type Runner interface {
	RemoveBackground(ctx context.Context, infile string, outfile string) (string, error)
}

func NewRunner(logger *slog.Logger) Runner {
	return &cliRunner{
		logger: logger,
	}
}

type cliRunner struct {
	logger *slog.Logger
}

func (r *cliRunner) RemoveBackground(ctx context.Context, infile string, outfile string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "imf", "remove-background", "-i", infile, "-o", outfile)
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()

	stdoutStr := ""
	if stdoutBytes, err := io.ReadAll(&stdout); err == nil {
		stdoutStr = string(stdoutBytes)
	}
	stderrStr := ""
	if stderrBytes, err := io.ReadAll(&stderr); err == nil {
		stderrStr = string(stderrBytes)
	}

	if err != nil {
		r.logger.Error("remove-background command failed", "error", err, "stdout", stdoutStr, "stderr", stderrStr)
		return "", err
	}
	return parseColor(stderrStr)
}

func parseColor(s string) (string, error) {
	return "#cccccc", nil
}
