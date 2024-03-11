package filters

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"

	"golang.org/x/exp/slog"
)

type Runner interface {
	RemoveBackground(ctx context.Context, infile string, outfile string) (string, error)
}

func NewRunner(logger *slog.Logger, imfBinaryPath string) Runner {
	return &cliRunner{
		logger:        logger,
		imfBinaryPath: imfBinaryPath,
	}
}

var regexHexColor = regexp.MustCompile(`^(#[0-9a-f]{6})\b`)

type cliRunner struct {
	logger        *slog.Logger
	imfBinaryPath string
}

func (r *cliRunner) RemoveBackground(ctx context.Context, infile string, outfile string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, r.imfBinaryPath, "remove-background", "-i", infile, "-o", outfile)
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
	m := regexHexColor.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("not a hex color")
	}
	return m[1], nil
}
